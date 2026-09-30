package service

import (
	"context"
	"errors"
	"testing"
	"time"

	chatpipeline "github.com/Tencent/WeKnora/internal/application/service/chat_pipeline"
	"github.com/Tencent/WeKnora/internal/config"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type questionBankStreamKBService struct {
	interfaces.KnowledgeBaseService
}

func (*questionBankStreamKBService) GetKnowledgeBaseByID(_ context.Context, id string) (*types.KnowledgeBase, error) {
	return &types.KnowledgeBase{ID: id, TenantID: 1, Type: types.KnowledgeBaseTypeQuestionBank}, nil
}

func (s *questionBankStreamKBService) GetKnowledgeBasesByIDsOnly(ctx context.Context, ids []string) ([]*types.KnowledgeBase, error) {
	kbs := make([]*types.KnowledgeBase, 0, len(ids))
	for _, id := range ids {
		kb, err := s.GetKnowledgeBaseByID(ctx, id)
		if err != nil {
			return nil, err
		}
		kbs = append(kbs, kb)
	}
	return kbs, nil
}

// 让真实 AgentQA 进入题库分支，但由测试控制异步回答到达的时机。
type questionBankStreamPlugin struct {
	ready chan struct{}
	err   *chatpipeline.PluginError
}

func (*questionBankStreamPlugin) ActivationEvents() []types.EventType {
	return []types.EventType{types.CHAT_COMPLETION_STREAM}
}

func (p *questionBankStreamPlugin) OnEvent(context.Context, types.EventType, *types.ChatManage, func() *chatpipeline.PluginError) *chatpipeline.PluginError {
	close(p.ready)
	return p.err
}

func startQuestionBankAgentTest(t *testing.T, pipelineErr *chatpipeline.PluginError) (
	context.CancelFunc, *event.EventBus, <-chan error, <-chan event.AgentCompleteData,
) {
	t.Helper()
	ctx, cancel := context.WithCancel(searchKnowledgeCtx())
	plugin := &questionBankStreamPlugin{ready: make(chan struct{}), err: pipelineErr}
	manager := chatpipeline.NewEventManager()
	manager.Register(plugin)
	svc := &sessionService{
		cfg:                  &config.Config{Conversation: &config.ConversationConfig{Summary: &config.SummaryConfig{}}},
		knowledgeBaseService: &questionBankStreamKBService{},
		modelService: &stubModelService{modelsByID: map[string]*types.Model{
			"model": {ID: "model", TenantID: 1, Type: types.ModelTypeKnowledgeQA, Status: types.ModelStatusActive},
		}},
		eventManager: manager,
	}
	req := &types.QARequest{
		Session:            &types.Session{ID: "session", TenantID: 1},
		Query:              "题库问题",
		AssistantMessageID: "assistant",
		SummaryModelID:     "model",
		KnowledgeBaseIDs:   []string{"bank"},
		TurnLeaseHeld:      true,
		CustomAgent: &types.CustomAgent{ID: "agent", TenantID: 1, Config: types.CustomAgentConfig{
			AgentMode: types.AgentModeSmartReasoning, ModelID: "model", SkillsSelectionMode: "none", WebSearchProviderID: "unused",
		}},
	}
	bus := event.NewEventBus()
	completed := make(chan event.AgentCompleteData, 4)
	bus.On(event.EventAgentComplete, func(_ context.Context, evt event.Event) error {
		completed <- evt.Data.(event.AgentCompleteData)
		return nil
	})
	returned := make(chan error, 1)
	stopped := make(chan struct{})
	go func() {
		defer close(stopped)
		returned <- svc.AgentQA(ctx, req, bus)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-stopped:
		case <-time.After(time.Second):
			t.Error("题库请求取消后未退出")
		}
	})
	select {
	case <-plugin.ready:
	case <-time.After(time.Second):
		select {
		case err := <-returned:
			t.Fatalf("未进入题库流式管线: %v", err)
		default:
			t.Fatal("题库管线未启动")
		}
	}
	return cancel, bus, returned, completed
}

func requireQuestionBankAgentWaiting(t *testing.T, returned <-chan error) {
	t.Helper()
	select {
	case err := <-returned:
		t.Fatalf("回答尚未结束，AgentQA 提前返回: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}

func questionBankAgentResult(t *testing.T, returned <-chan error, completed <-chan event.AgentCompleteData) (error, event.AgentCompleteData) {
	t.Helper()
	var err error
	select {
	case err = <-returned:
	case <-time.After(time.Second):
		t.Fatal("终止事件后 AgentQA 未返回")
	}
	select {
	case result := <-completed:
		select {
		case <-completed:
			t.Fatal("重复发送完成事件")
		default:
		}
		return err, result
	default:
		t.Fatal("AgentQA 返回前未发送完成事件")
		return err, event.AgentCompleteData{}
	}
}

func TestQuestionBankAgentWaitsForStreamAndCompletesWithAnswer(t *testing.T) {
	_, bus, returned, completed := startQuestionBankAgentTest(t, nil)
	requireQuestionBankAgentWaiting(t, returned)
	ctx := context.Background()
	ref := &types.SearchResult{ID: "question", KnowledgeBaseID: "bank"}
	require.NoError(t, bus.Emit(ctx, event.Event{Type: event.EventAgentReferences, Data: event.AgentReferencesData{References: []*types.SearchResult{ref}}}))
	require.NoError(t, bus.Emit(ctx, event.Event{Type: event.EventAgentToolCall, Data: event.AgentToolCallData{ToolCallID: "search", ToolName: "knowledge_search", Arguments: map[string]interface{}{"query": "题库问题"}}}))
	require.NoError(t, bus.Emit(ctx, event.Event{Type: event.EventAgentToolResult, Data: event.AgentToolResultData{ToolCallID: "search", ToolName: "knowledge_search", Success: true, Output: "找到原题", Duration: 12}}))
	require.NoError(t, bus.Emit(ctx, event.Event{Type: event.EventAgentThought, Data: event.AgentThoughtData{Content: "核对原题"}}))
	require.NoError(t, bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "答案是"}}))
	requireQuestionBankAgentWaiting(t, returned)
	require.NoError(t, bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "B。", Done: true, Truncated: true}}))
	err, result := questionBankAgentResult(t, returned, completed)
	require.NoError(t, err)
	require.Equal(t, "答案是B。", result.FinalAnswer)
	require.Equal(t, "assistant", result.MessageID)
	require.Equal(t, "session", result.SessionID)
	require.Equal(t, []interface{}{ref}, result.KnowledgeRefs)
	steps, ok := result.AgentSteps.([]types.AgentStep)
	require.True(t, ok)
	require.Len(t, steps, 1)
	require.Equal(t, "核对原题", steps[0].ReasoningContent)
	require.True(t, steps[0].Truncated)
	require.NotEmpty(t, steps[0].ToolCalls)
	search := steps[0].ToolCalls[len(steps[0].ToolCalls)-1]
	require.Equal(t, types.PipelineToolCallIDPrefix+"search", search.ID)
	require.Equal(t, "题库问题", search.Args["query"])
	require.Equal(t, "找到原题", search.Result.Output)
	// 上游重复发送结束标记时，不得再次完成或重复保存正文。
	require.NoError(t, bus.Emit(ctx, event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "重复", Done: true}}))
	require.Empty(t, completed)
}

func TestQuestionBankAgentStreamErrorKeepsPartialAnswer(t *testing.T) {
	_, bus, returned, completed := startQuestionBankAgentTest(t, nil)
	require.NoError(t, bus.Emit(context.Background(), event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "已生成部分"}}))
	require.NoError(t, bus.Emit(context.Background(), event.Event{Type: event.EventError, Data: event.ErrorData{Error: "模型连接中断"}}))
	err, result := questionBankAgentResult(t, returned, completed)
	require.NoError(t, err, "流式错误已经通过错误事件通知，不重复上报")
	require.Equal(t, "已生成部分", result.FinalAnswer)
}

func TestQuestionBankAgentCancellationKeepsPartialAnswer(t *testing.T) {
	cancel, bus, returned, completed := startQuestionBankAgentTest(t, nil)
	require.NoError(t, bus.Emit(context.Background(), event.Event{Type: event.EventAgentFinalAnswer, Data: event.AgentFinalAnswerData{Content: "停止前的回答"}}))
	cancel()
	err, result := questionBankAgentResult(t, returned, completed)
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, "停止前的回答", result.FinalAnswer)
}

func TestQuestionBankAgentSetupErrorDoesNotWaitForStream(t *testing.T) {
	_, _, returned, completed := startQuestionBankAgentTest(t, chatpipeline.ErrModelCall.WithError(errors.New("模型不可用")))
	err, result := questionBankAgentResult(t, returned, completed)
	require.Error(t, err)
	require.Empty(t, result.FinalAnswer)
}
