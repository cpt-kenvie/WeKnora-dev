package repository

import (
	"context"
	"errors"
	"os"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func setupQuestionTest(t *testing.T) (*QuestionRepository, *gorm.DB, *types.Knowledge) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, sqlDB.Close()) })
	require.NoError(t, db.Exec(knowledgeBasesTestDDL).Error)
	require.NoError(t, db.AutoMigrate(&types.Knowledge{}, &types.Chunk{}, &types.KnowledgeProcessingSpan{}))
	ddl, err := os.ReadFile("../../../migrations/sqlite/000034_question_bank.up.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(ddl)).Error)
	require.NoError(t, db.Exec("INSERT INTO knowledge_bases (id,tenant_id,name,type,embedding_model_id,summary_model_id) VALUES ('bank',1,'题库','question_bank','embed','')").Error)
	source := &types.Knowledge{ID: uuid.NewString(), TenantID: 1, KnowledgeBaseID: "bank", FilePath: "resource://original", ParseStatus: types.ParseStatusProcessing, EnableStatus: "enabled"}
	require.NoError(t, db.Create(source).Error)
	return NewQuestionRepository(db), db, source
}

func exampleQuestion() *types.Question {
	return &types.Question{QuestionContent: types.QuestionContent{QuestionType: types.QuestionFill, Stem: "旅客是指持有____的乘车人。", Options: []types.QuestionOption{}, Answer: types.QuestionAnswer{Blanks: []string{"铁路有效乘车凭证"}}, AnswerRaw: "铁路有效乘车凭证", BlankCount: 1}, Issues: []string{}, ReviewStatus: types.QuestionReady, IsEnabled: true}
}

func TestQuestionRepositoryVersionsReviewAndRetry(t *testing.T) {
	repo, db, source := setupQuestionTest(t)
	ctx := context.Background()
	saved, err := repo.SaveExtracted(ctx, source, []*types.Question{exampleQuestion()}, 0)
	require.NoError(t, err)
	q := saved[0]
	require.NoError(t, repo.FinishIndex(ctx, q, errors.New("embedding unavailable")))
	require.Equal(t, "failed", q.IndexStatus)
	require.NoError(t, db.Model(source).Update("parse_status", types.ParseStatusCompleted).Error)
	kb := &types.KnowledgeBase{ID: "bank", TenantID: 1}
	hits, err := repo.ExactCandidates(ctx, kb, types.SearchParams{QueryText: q.Stem})
	require.NoError(t, err)
	require.Empty(t, hits)
	old := *q
	require.NoError(t, repo.RetryIndex(ctx, q))
	require.NotEqual(t, old.ChunkID, q.ChunkID)
	require.ErrorIs(t, repo.FinishIndex(ctx, &old, nil), ErrQuestionConflict)
	require.NoError(t, repo.FinishIndex(ctx, q, nil))
	hits, err = repo.ExactCandidates(ctx, kb, types.SearchParams{QueryText: q.Stem})
	require.NoError(t, err)
	require.Len(t, hits, 1)
	_, err = repo.Get(ctx, 2, "bank", q.ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
	q.Answer.Blanks = []string{"人工核对答案"}
	expected := q.Revision
	require.NoError(t, repo.Update(ctx, q, expected))
	require.NoError(t, repo.FinishIndex(ctx, q, nil))
	stale := *q
	require.ErrorIs(t, repo.Update(ctx, &stale, expected), ErrQuestionConflict)
	// 模拟通用重新解析清理分块，然后确保人工修正仍有新的可检索投影。
	require.NoError(t, db.Where("knowledge_id = ?", source.ID).Delete(&types.Chunk{}).Error)
	require.NoError(t, db.Model(source).Update("parse_status", types.ParseStatusProcessing).Error)
	fresh, err := repo.SaveExtracted(ctx, source, []*types.Question{exampleQuestion()}, 0)
	require.NoError(t, err)
	require.Len(t, fresh, 1)
	require.Equal(t, q.ID, fresh[0].ID)
	require.Equal(t, "人工核对答案", fresh[0].Answer.Blanks[0])
	require.NotEqual(t, q.ChunkID, fresh[0].ChunkID)
	require.NoError(t, repo.FinishIndex(ctx, fresh[0], nil))
	require.NoError(t, repo.Delete(ctx, fresh[0], fresh[0].Revision))
	again, err := repo.SaveExtracted(ctx, source, []*types.Question{exampleQuestion()}, 0)
	require.NoError(t, err)
	require.Empty(t, again)
}

func TestQuestionRepositorySourceScopeAndDuplicates(t *testing.T) {
	repo, db, source := setupQuestionTest(t)
	ctx := context.Background()
	second := exampleQuestion()
	second.SourceIndex = 1
	saved, err := repo.SaveExtracted(ctx, source, []*types.Question{exampleQuestion(), second}, 0)
	require.NoError(t, err)
	require.Equal(t, types.QuestionNeedsReview, saved[1].ReviewStatus)
	require.False(t, saved[1].IsEnabled)
	list, err := repo.List(ctx, 1, "bank", types.QuestionListFilter{})
	require.NoError(t, err)
	require.EqualValues(t, 1, list.ProcessingSources)
	require.EqualValues(t, 0, list.FailedSources)
	require.EqualValues(t, 2, list.Total)
	require.NoError(t, db.Model(source).Update("parse_status", types.ParseStatusCancelled).Error)
	_, err = repo.SaveExtracted(ctx, source, []*types.Question{exampleQuestion()}, 0)
	require.ErrorIs(t, err, ErrQuestionConflict)
	require.NoError(t, db.Delete(source).Error)
	list, err = repo.List(ctx, 1, "bank", types.QuestionListFilter{})
	require.NoError(t, err)
	require.Empty(t, list.Items)
	_, err = repo.Get(ctx, 1, "bank", saved[0].ID)
	require.ErrorIs(t, err, gorm.ErrRecordNotFound)
}

func TestQuestionRepositoryMigrationRollback(t *testing.T) {
	_, db, _ := setupQuestionTest(t)
	ddl, err := os.ReadFile("../../../migrations/sqlite/000034_question_bank.down.sql")
	require.NoError(t, err)
	require.NoError(t, db.Exec(string(ddl)).Error)
	require.False(t, db.Migrator().HasTable(&types.Question{}))
}
