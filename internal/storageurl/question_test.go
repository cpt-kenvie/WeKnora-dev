package storageurl

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestQuestionSnapshotURLRewriteDoesNotChangeHistory(t *testing.T) {
	original := &types.SearchResult{Question: &types.QuestionSnapshot{ID: "q", ImageRef: "resource://xifDo7NTSL300Lp1goVutw"}}
	rewritten := publicRewriter("https://cdn.example.com/question.jpg").CopyReferences(context.Background(), []*types.SearchResult{original})
	require.Equal(t, "https://cdn.example.com/question.jpg", rewritten[0].Question.ImageRef)
	require.Equal(t, "resource://xifDo7NTSL300Lp1goVutw", original.Question.ImageRef)
}
