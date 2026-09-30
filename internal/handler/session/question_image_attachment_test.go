package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type questionImageDocuments struct {
	interfaces.TemporaryDocumentService
	status string
}

func (s *questionImageDocuments) Get(context.Context, uint64, string, string) (*types.TemporaryDocument, error) {
	return &types.TemporaryDocument{ID: "image", Status: s.status}, nil
}

func (s *questionImageDocuments) ResolveForPrompt(context.Context, uint64, string, []string, string) (*types.TemporaryDocumentPromptResult, error) {
	return &types.TemporaryDocumentPromptResult{
		Attachments: types.MessageAttachments{{ID: "image", URL: "resource://original", FileType: ".png", Content: "![图](resource://original)"}},
		ImageURLs:   []string{"resource://original"},
	}, nil
}

func TestQuestionImageAttachmentReachesNormalKnowledgeQA(t *testing.T) {
	h := &Handler{temporaryDocuments: &questionImageDocuments{status: types.TemporaryDocumentStatusReady}}
	req := &qaRequestContext{
		session: &types.Session{TenantID: 42}, assistantMessage: &types.Message{ID: "answer"}, query: "回答", attachmentIDs: []string{"image"},
		attachmentMetas: types.MessageAttachments{{ID: "image", FileType: ".png", FileName: "question.png"}},
	}
	stream := &sseStreamContext{asyncCtx: context.Background(), eventBus: event.NewEventBus()}
	require.NoError(t, h.resolveTemporaryAttachments(stream, req))
	qa := req.buildQARequest()
	require.Equal(t, []string{"resource://original"}, qa.ImageURLs)
	require.Len(t, qa.Attachments, 1)
}

func TestQuestionImageAttachmentFailureStopsQA(t *testing.T) {
	h := &Handler{temporaryDocuments: &questionImageDocuments{status: types.TemporaryDocumentStatusFailed}}
	req := &qaRequestContext{
		session: &types.Session{TenantID: 42}, assistantMessage: &types.Message{ID: "answer"}, query: "回答", attachmentIDs: []string{"image"},
		attachmentMetas: types.MessageAttachments{{ID: "image", FileType: ".png", FileName: "question.png"}},
	}
	stream := &sseStreamContext{asyncCtx: context.Background(), eventBus: event.NewEventBus()}
	err := h.resolveTemporaryAttachments(stream, req)
	require.ErrorContains(t, err, "图片附件")
	require.Empty(t, req.images)
}
