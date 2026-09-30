package chatpipeline

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestQuestionBankCompletionSkipsModelAndKeepsOriginal(t *testing.T) {
	cm := &types.ChatManage{PipelineRequest: types.PipelineRequest{Query: "旅客持有____。"}, PipelineState: types.PipelineState{MergeResult: []*types.SearchResult{{Question: &types.QuestionSnapshot{ID: "q", Revision: 1, ImageRef: "resource://original", QuestionContent: types.QuestionContent{QuestionType: types.QuestionFill, Stem: "旅客持有____。", BlankCount: 1, Answer: types.QuestionAnswer{Blanks: []string{"铁路有效乘车凭证"}}}}}}}}
	next := func() *PluginError { return nil }
	// 不提供模型依赖；任何意外模型调用都会使测试失败。
	require.Nil(t, (&PluginChatCompletion{}).OnEvent(context.Background(), types.CHAT_COMPLETION, cm, next))
	require.Contains(t, cm.ChatResponse.Content, "铁路有效乘车凭证")
	require.Contains(t, cm.ChatResponse.Content, "resource://original")
	bus := event.NewEventBus()
	cm.EventBus = bus.AsEventBusInterface()
	emitted := []event.AgentFinalAnswerData{}
	bus.On(event.EventAgentFinalAnswer, func(_ context.Context, e event.Event) error {
		emitted = append(emitted, e.Data.(event.AgentFinalAnswerData))
		return nil
	})
	require.Nil(t, (&PluginChatCompletionStream{}).OnEvent(context.Background(), types.CHAT_COMPLETION_STREAM, cm, next))
	require.Len(t, emitted, 1)
	require.True(t, emitted[0].Done)
	require.Equal(t, cm.ChatResponse.Content, emitted[0].Content)
	cm.MergeResult = nil
	cm.QuestionBankOnly = true
	require.Nil(t, (&PluginChatCompletion{}).OnEvent(context.Background(), types.CHAT_COMPLETION, cm, next))
	require.Contains(t, cm.ChatResponse.Content, "没有找到可靠匹配")
}
