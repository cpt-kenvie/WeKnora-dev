package repository

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func seedPaperQuestion(t *testing.T, db *gorm.DB, source *types.Knowledge, kind, status string) *types.Question {
	t.Helper()
	question := exampleQuestion()
	var existing int64
	require.NoError(t, db.Unscoped().Model(&types.Question{}).Count(&existing).Error)
	question.SourceIndex = int(existing)
	question.ID, question.ChunkID = uuid.NewString(), uuid.NewString()
	question.TenantID, question.KnowledgeBaseID, question.KnowledgeID = source.TenantID, source.KnowledgeBaseID, source.ID
	question.QuestionType, question.ReviewStatus = kind, status
	question.Stem = "题目 " + question.ID
	question.Fingerprint = types.QuestionFingerprint(question.Stem)
	question.IndexStatus = "failed"
	question.IsEnabled = false
	require.NoError(t, db.Create(question).Error)
	return question
}

func TestQuestionPaperScopeReviewAndDeletedParents(t *testing.T) {
	repo, db, source := setupQuestionTest(t)
	ctx := context.Background()
	ready := seedPaperQuestion(t, db, source, types.QuestionFill, types.QuestionReady)
	seedPaperQuestion(t, db, source, types.QuestionFill, types.QuestionNeedsReview)
	deleted := seedPaperQuestion(t, db, source, types.QuestionFill, types.QuestionReady)
	require.NoError(t, db.Delete(deleted).Error)
	otherSource := *source
	otherSource.ID = uuid.NewString()
	require.NoError(t, db.Create(&otherSource).Error)
	seedPaperQuestion(t, db, &otherSource, types.QuestionFill, types.QuestionReady)
	require.NoError(t, db.Delete(&otherSource).Error)
	foreign := *source
	foreign.TenantID = 2
	seedPaperQuestion(t, db, &foreign, types.QuestionFill, types.QuestionReady)
	foreign.TenantID, foreign.KnowledgeBaseID = 1, "other-bank"
	seedPaperQuestion(t, db, &foreign, types.QuestionFill, types.QuestionReady)
	counts, err := repo.PaperCounts(ctx, 1, "bank")
	require.NoError(t, err)
	require.EqualValues(t, 1, counts[types.QuestionFill])
	require.EqualValues(t, 0, counts[types.QuestionSingle])
	sampled, err := repo.SamplePaper(ctx, 1, "bank", map[string]int{types.QuestionFill: 10})
	require.NoError(t, err)
	require.Len(t, sampled[types.QuestionFill], 1)
	require.Equal(t, ready.ID, sampled[types.QuestionFill][0].ID)
	counts, err = repo.PaperCounts(ctx, 2, "bank")
	require.NoError(t, err)
	require.Zero(t, counts[types.QuestionFill])
	require.NoError(t, db.Exec("UPDATE knowledge_bases SET deleted_at = CURRENT_TIMESTAMP WHERE id = ?", "bank").Error)
	sampled, err = repo.SamplePaper(ctx, 1, "bank", map[string]int{types.QuestionFill: 10})
	require.NoError(t, err)
	require.Empty(t, sampled[types.QuestionFill])
}

func TestQuestionPaperSamplesWholeBankWithoutDuplicates(t *testing.T) {
	repo, db, source := setupQuestionTest(t)
	for i := 0; i < 125; i++ {
		seedPaperQuestion(t, db, source, types.QuestionFill, types.QuestionReady)
	}
	for i := 0; i < 4; i++ {
		seedPaperQuestion(t, db, source, types.QuestionSingle, types.QuestionReady)
	}
	sampled, err := repo.SamplePaper(context.Background(), 1, "bank", map[string]int{types.QuestionFill: 120, types.QuestionSingle: 3})
	require.NoError(t, err)
	require.Len(t, sampled[types.QuestionFill], 120)
	require.Len(t, sampled[types.QuestionSingle], 3)
	seen := make(map[string]bool)
	for kind, questions := range sampled {
		for _, question := range questions {
			require.Equal(t, kind, question.QuestionType)
			require.False(t, seen[question.ID], fmt.Sprintf("重复题目 %s", question.ID))
			seen[question.ID] = true
		}
	}
}
