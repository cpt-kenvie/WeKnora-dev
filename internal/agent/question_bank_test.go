package agent

import (
	"context"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestQuestionBankAgentUsesSnapshot(t *testing.T) {
	bus := event.NewEventBus()
	engine := &AgentEngine{eventBus: bus}
	snapshot := &types.QuestionSnapshot{ID: "question", Revision: 2, ImageRef: "resource://original", QuestionContent: types.QuestionContent{QuestionType: types.QuestionFill, Stem: "持有____", Answer: types.QuestionAnswer{Blanks: []string{"凭证"}}, BlankCount: 1}}
	step := types.AgentStep{ToolCalls: []types.ToolCall{{Name: agenttools.ToolSearchKnowledge, Result: &types.ToolResult{Success: true, Data: map[string]interface{}{"results": []map[string]interface{}{{"chunk_id": "version-chunk", "question": snapshot}}}}}}}
	var got string
	bus.On(event.EventAgentFinalAnswer, func(_ context.Context, e event.Event) error {
		got = e.Data.(event.AgentFinalAnswerData).Content
		return nil
	})
	state := &types.AgentState{}
	require.True(t, engine.answerQuestionToolResults(context.Background(), snapshot.Stem, state, step, "session"))
	require.True(t, state.IsComplete)
	require.Equal(t, state.FinalAnswer, got)
	require.Contains(t, got, "凭证")
	require.Equal(t, "version-chunk", state.KnowledgeRefs[0].ID)
	require.Contains(t, state.KnowledgeRefs[0].ImageInfo, "resource://original")
	step.ToolCalls[0].Name = "external_tool"
	require.False(t, engine.answerQuestionToolResults(context.Background(), snapshot.Stem, &types.AgentState{}, step, "session"))
}

func TestQuestionBankAgentEmptySearchDoesNotGuess(t *testing.T) {
	engine := &AgentEngine{eventBus: event.NewEventBus()}
	state := &types.AgentState{}
	step := types.AgentStep{ToolCalls: []types.ToolCall{{Name: agenttools.ToolSearchKnowledge, Result: &types.ToolResult{Success: true, Data: map[string]interface{}{"question_bank_only": true, "results": []interface{}{}}}}}}
	require.True(t, engine.answerQuestionToolResults(context.Background(), "问题", state, step, "session"))
	require.Contains(t, state.FinalAnswer, "没有找到可靠匹配")
}
