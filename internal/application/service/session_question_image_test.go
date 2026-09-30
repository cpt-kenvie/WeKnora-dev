package service

import (
	"context"
	"fmt"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type questionImageKBs struct {
	interfaces.KnowledgeBaseService
	kb     *types.KnowledgeBase
	tenant uint64
}

func (s *questionImageKBs) GetKnowledgeBaseByID(ctx context.Context, _ string) (*types.KnowledgeBase, error) {
	s.tenant = types.MustTenantIDFromContext(ctx)
	return s.kb, nil
}

func TestQuestionImageModelUsesAuthorizedBankConfiguration(t *testing.T) {
	kbs := &questionImageKBs{kb: &types.KnowledgeBase{Type: types.KnowledgeBaseTypeQuestionBank, VLMConfig: types.VLMConfig{Enabled: true, ModelID: "bank-vision"}}}
	s := &sessionService{knowledgeBaseService: kbs}
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{Images: []string{"image"}, SearchTargets: types.SearchTargets{{TenantID: 42, KnowledgeBaseID: "bank"}}}}
	s.resolveQuestionImageModel(context.Background(), cm)
	require.Equal(t, "bank-vision", cm.VLMModelID)
	require.Equal(t, uint64(42), cm.VLMModelTenantID)
	require.Equal(t, uint64(42), kbs.tenant)
	cm.VLMModelID = "agent-vision"
	s.resolveQuestionImageModel(context.Background(), cm)
	require.Equal(t, "agent-vision", cm.VLMModelID)
}

func TestQuestionImageAttachmentsKeepOriginalsWithoutParserImageRefs(t *testing.T) {
	documents := make([]*types.TemporaryDocument, 0, 5)
	ids := make([]string, 0, 5)
	for i := 0; i < 5; i++ {
		doc := parentOwnedBrief(t)
		doc.ID, doc.FileType, doc.ResourceRef = fmt.Sprintf("img%d", i), ".png", fmt.Sprintf("resource://original%d", i)
		doc.Content = "图片解析引擎已提取题干和选项"
		documents = append(documents, doc)
		ids = append(ids, doc.ID)
	}
	s := &temporaryDocumentService{repo: &forkAccessDocRepo{docs: documents}}
	result, err := s.ResolveForPrompt(context.Background(), 7, "parent", ids, "回答")
	require.NoError(t, err)
	require.Len(t, result.Attachments, 5)
	require.Len(t, result.ImageURLs, 5)
	for i, attachment := range result.Attachments {
		require.Equal(t, attachment.URL, result.ImageURLs[i])
		require.Contains(t, attachment.Content, "题干和选项")
	}
}
