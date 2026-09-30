package repository

import (
	"context"
	"errors"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestQuestionStorageQuotaRetryAndDelete(t *testing.T) {
	repo, db, source := setupQuestionTest(t)
	ctx := context.Background()
	require.NoError(t, db.Exec("CREATE TABLE tenants (id INTEGER PRIMARY KEY, storage_used BIGINT NOT NULL, storage_quota BIGINT NOT NULL, deleted_at DATETIME)").Error)
	require.NoError(t, db.Exec("INSERT INTO tenants (id, storage_used, storage_quota) VALUES (1, 0, 100)").Error)
	saved, err := repo.SaveExtracted(ctx, source, []*types.Question{exampleQuestion()}, 0)
	require.NoError(t, err)
	q := saved[0]
	assertUsage := func(want int64) {
		t.Helper()
		var tenant types.Tenant
		var current types.Knowledge
		require.NoError(t, db.First(&tenant, 1).Error)
		require.NoError(t, db.First(&current, "id = ?", source.ID).Error)
		require.Equal(t, want, tenant.StorageUsed)
		require.Equal(t, want, current.StorageSize)
	}
	require.NoError(t, repo.ReserveIndexStorage(ctx, q, 80))
	require.NoError(t, repo.ReserveIndexStorage(ctx, q, 80))
	assertUsage(80)
	require.ErrorContains(t, repo.ReserveIndexStorage(ctx, q, 101), "存储空间不足")
	assertUsage(80)
	require.NoError(t, repo.FinishIndex(ctx, q, errors.New("partial index failure")))
	source.ParseStatus = types.ParseStatusFailed
	require.NoError(t, repo.UpdateSourceState(ctx, source, 0))
	assertUsage(80) // 旧任务对象的 StorageSize 为零，不能覆盖已预留用量。
	require.NoError(t, repo.RetryIndex(ctx, q))
	require.NoError(t, repo.ReserveIndexStorage(ctx, q, 70))
	assertUsage(70)
	require.NoError(t, repo.FinishIndex(ctx, q, nil))
	var recovered types.Knowledge
	require.NoError(t, db.First(&recovered, "id = ?", source.ID).Error)
	require.Equal(t, types.ParseStatusCompleted, recovered.ParseStatus)
	require.NoError(t, repo.Delete(ctx, q, q.Revision))
	assertUsage(0)
}

func TestQuestionSourceStateCannotReviveCancelledAttempt(t *testing.T) {
	repo, db, source := setupQuestionTest(t)
	saved, err := repo.SaveExtracted(context.Background(), source, []*types.Question{exampleQuestion()}, 0)
	require.NoError(t, err)
	require.NoError(t, db.Model(source).Update("parse_status", types.ParseStatusCancelled).Error)
	source.ParseStatus = types.ParseStatusCompleted
	require.ErrorIs(t, repo.UpdateSourceState(context.Background(), source, 0), ErrQuestionConflict)
	require.Error(t, repo.FinishIndex(context.Background(), saved[0], nil))
}
