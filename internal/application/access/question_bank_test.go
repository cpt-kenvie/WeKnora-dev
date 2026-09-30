package access

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestQuestionBankTransferRejectsNewCloneAndMove(t *testing.T) {
	source := &types.KnowledgeBase{ID: "source", TenantID: 1, Type: types.KnowledgeBaseTypeQuestionBank}
	target := &types.KnowledgeBase{ID: "target", TenantID: 1, Type: types.KnowledgeBaseTypeQuestionBank}
	for _, create := range []bool{false, true} {
		require.ErrorContains(t, validateTransferPair(source, target, 1, KBTransferClone, create), "题库")
	}
	require.ErrorContains(t, validateTransferPair(source, target, 1, KBTransferMove, false), "题库")
}
