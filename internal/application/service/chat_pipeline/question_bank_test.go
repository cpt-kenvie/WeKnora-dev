package chatpipeline

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type questionCompletionChat struct {
	openStreamChat
	messages []chat.Message
	answer   string
	err      error
}

func (m *questionCompletionChat) Chat(_ context.Context, messages []chat.Message, _ *chat.ChatOptions) (*types.ChatResponse, error) {
	m.messages = messages
	if m.err != nil {
		return nil, m.err
	}
	return &types.ChatResponse{Content: m.answer, FinishReason: "stop"}, nil
}

func (m *questionCompletionChat) ChatStream(ctx context.Context, messages []chat.Message, opt *chat.ChatOptions) (<-chan types.StreamResponse, error) {
	m.messages = messages
	if m.err != nil {
		return nil, m.err
	}
	return m.openStreamChat.ChatStream(ctx, messages, opt)
}

func questionCompletionFixture() *types.ChatManage {
	q := &types.QuestionSnapshot{ID: "question", Revision: 1, KnowledgeBaseID: "bank", KnowledgeID: "image", ImageRef: "resource://original",
		QuestionContent: types.QuestionContent{QuestionType: types.QuestionFill, Stem: "旅客持有____。", BlankCount: 1,
			Options: []types.QuestionOption{}, Answer: types.QuestionAnswer{Blanks: []string{"铁路有效乘车凭证"}}}}
	other := *q
	other.ID, other.Stem, other.Answer = "other", "无票乘车拒绝补票应追补____。", types.QuestionAnswer{Blanks: []string{"应收和加收票款"}}
	return &types.ChatManage{QuestionBankOnly: true,
		PipelineRequest: types.PipelineRequest{Query: "乘车人持有什么才能被称为旅客？"},
		PipelineState: types.PipelineState{UserContent: "乘车人持有什么才能被称为旅客？", MergeResult: []*types.SearchResult{
			{ID: "original-chunk", KnowledgeID: "image", KnowledgeBaseID: "bank", ChunkType: types.ChunkTypeQuestion, Content: q.SearchText(), Question: q},
			{ID: "other-chunk", KnowledgeID: "image", KnowledgeBaseID: "bank", ChunkType: types.ChunkTypeQuestion, Content: other.SearchText(), Question: &other},
		}}}
}

func TestQuestionBankCompletionUsesModelAndStoredAnswers(t *testing.T) {
	for _, stream := range []bool{false, true} {
		name := "completion"
		if stream {
			name = "stream"
		}
		t.Run(name, func(t *testing.T) {
			cm := questionCompletionFixture()
			model := &questionCompletionChat{answer: `必须持有铁路有效乘车凭证。<ref id="c1"/>`}
			service := &stubModelService{model: model}
			next := func() *PluginError { return nil }
			var answer string
			if stream {
				model.chunks = []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: model.answer, Done: true, FinishReason: "stop"}}
				model.closeStream = true
				bus := &syncEventBus{}
				cm.EventBus = bus
				require.Nil(t, (&PluginChatCompletionStream{modelService: service}).OnEvent(context.Background(), types.CHAT_COMPLETION_STREAM, cm, next))
				require.Eventually(t, func() bool {
					for _, evt := range bus.finalAnswerEvents() {
						if evt.Done {
							return true
						}
					}
					return false
				}, time.Second, time.Millisecond)
				answer = strings.Join(bus.finalAnswerContents(), "")
			} else {
				require.Nil(t, (&PluginChatCompletion{modelService: service}).OnEvent(context.Background(), types.CHAT_COMPLETION, cm, next))
				answer = cm.ChatResponse.Content
			}
			require.NotEmpty(t, model.messages)
			require.Contains(t, model.messages[len(model.messages)-1].Content, "<answer>铁路有效乘车凭证</answer>")
			require.Contains(t, model.messages[len(model.messages)-1].Content, "<answer>应收和加收票款</answer>")
			require.Contains(t, answer, "必须持有铁路有效乘车凭证。")
			require.Contains(t, answer, `chunk_id="original-chunk"`)
			require.NotContains(t, answer, "other-chunk")
			require.NotContains(t, answer, "以下候选原题")
		})
	}
}

func TestQuestionBankModelFailureDoesNotDumpCandidates(t *testing.T) {
	for _, stream := range []bool{false, true} {
		cm := questionCompletionFixture()
		bus := &syncEventBus{}
		cm.EventBus = bus
		model := &questionCompletionChat{err: errors.New("model unavailable")}
		service := &stubModelService{model: model}
		var err *PluginError
		if stream {
			err = (&PluginChatCompletionStream{modelService: service}).OnEvent(context.Background(), types.CHAT_COMPLETION_STREAM, cm, func() *PluginError { return nil })
		} else {
			err = (&PluginChatCompletion{modelService: service}).OnEvent(context.Background(), types.CHAT_COMPLETION, cm, func() *PluginError { return nil })
		}
		require.NotNil(t, err)
		require.Nil(t, cm.ChatResponse)
		require.Empty(t, bus.finalAnswerContents())
	}
}

func TestQuestionBankMergePreservesMixedDocumentsAndSeparateQuestions(t *testing.T) {
	cm := questionCompletionFixture()
	questions := cm.MergeResult
	stale := *questions[0]
	stale.ID = "previous-revision-chunk"
	cm.History = []*types.History{{KnowledgeReferences: []*types.SearchResult{&stale}}}
	cm.Query = stale.Content
	document := &types.SearchResult{ID: "document-chunk", KnowledgeID: "document", KnowledgeBaseID: "docs",
		ChunkType: "text", Content: strings.Repeat("旅客运输相关规定。", 100), Score: 0.9}
	cm.RerankResult = append(append([]*types.SearchResult{document}, questions...), questions[0])
	cm.MergeResult = nil
	require.Nil(t, (&PluginMerge{}).OnEvent(context.Background(), types.CHUNK_MERGE, cm, func() *PluginError { return nil }))
	require.Len(t, cm.MergeResult, 3)
	require.Same(t, document, cm.MergeResult[0])
	for _, question := range questions {
		require.Contains(t, cm.MergeResult, question)
		require.Empty(t, question.SubChunkID)
	}
}
