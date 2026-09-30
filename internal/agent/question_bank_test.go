package agent

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/models/chat"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

type questionSearchTool struct {
	agenttools.BaseTool
	result *types.ToolResult
}

func (t *questionSearchTool) Execute(context.Context, json.RawMessage) (*types.ToolResult, error) {
	return t.result, nil
}

func questionSearchFixture() *types.ToolResult {
	rows := []map[string]interface{}{}
	for i, item := range []struct{ stem, answer string }{
		{"旅客是指持有____的乘车人。", "铁路有效乘车凭证"},
		{"无票乘车拒绝补票，应追补____。", "应收和加收票款"},
		{"乘车时禁止携带____。", "危险品"},
	} {
		id := []string{"passenger", "fare", "luggage"}[i]
		q := &types.QuestionSnapshot{ID: id, Revision: 2, KnowledgeID: "image-" + id,
			KnowledgeBaseID: "bank", ImageRef: "resource://original-" + id,
			QuestionContent: types.QuestionContent{QuestionType: types.QuestionFill, Stem: item.stem,
				BlankCount: 1, Options: []types.QuestionOption{}, Answer: types.QuestionAnswer{Blanks: []string{item.answer}}}}
		rows = append(rows, map[string]interface{}{"chunk_id": "chunk-" + id, "knowledge_id": q.KnowledgeID,
			"knowledge_base_id": q.KnowledgeBaseID, "knowledge_title": "原图", "content": q.SearchText(), "question": q})
	}
	return &types.ToolResult{Success: true, Data: map[string]interface{}{
		"display_type": "search_results", "question_bank_only": true, "results": rows}}
}

func TestQuestionBankAgentReturnsToModelAfterSearch(t *testing.T) {
	for _, tc := range []struct {
		name, query, answer string
		empty               bool
		cited, excluded     []string
	}{
		{name: "one relevant among three", query: "乘车人持有什么才能被称为旅客？",
			answer: `乘车人须持有铁路有效乘车凭证。<ref id="c1"/>`, cited: []string{"passenger"}, excluded: []string{"fare", "luggage"}},
		{name: "multiple requested questions", query: "解释旅客定义和无票补票两道题",
			answer: `旅客须持有铁路有效乘车凭证。<ref id="c1"/> 无票拒补应追补应收和加收票款。<ref id="c2"/>`,
			cited:  []string{"passenger", "fare"}, excluded: []string{"luggage"}},
		{name: "irrelevant candidates", query: "退票期限是多少？", answer: "题库中没有找到匹配的原题，请补充题干。",
			excluded: []string{"passenger", "fare", "luggage"}},
		{name: "empty search", query: "退票期限是多少？", empty: true, answer: "题库中没有找到匹配的原题，请补充题干。"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			model := &mockChat{responses: []mockResponse{
				{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Done: true, FinishReason: "tool_calls",
					ToolCalls: []types.LLMToolCall{{ID: "search-call", Type: "function", Function: types.FunctionCall{
						Name: agenttools.ToolSearchKnowledge, Arguments: `{"query":"乘车人持有什么才能被称为旅客？"}`}}}}}},
				{chunks: []types.StreamResponse{{ResponseType: types.ResponseTypeAnswer, Content: tc.answer, Done: true, FinishReason: "stop"}}},
			}}
			engine := newTestEngine(t, model)
			result := questionSearchFixture()
			if tc.empty {
				result.Data["results"] = []map[string]interface{}{}
				result.Output = "No matching questions."
			}
			registry := agenttools.NewToolRegistry()
			registry.RegisterTool(&questionSearchTool{BaseTool: agenttools.NewBaseTool(agenttools.ToolSearchKnowledge, "test",
				json.RawMessage(`{"type":"object","properties":{"query":{"type":"string"}}}`)), result: result})
			engine.toolRegistry = registry
			var answers strings.Builder
			engine.eventBus.On(event.EventAgentFinalAnswer, func(_ context.Context, evt event.Event) error {
				answers.WriteString(evt.Data.(event.AgentFinalAnswerData).Content)
				return nil
			})
			state := &types.AgentState{}
			_, err := engine.executeLoop(context.Background(), state, tc.query,
				[]chat.Message{{Role: "system", Content: engine.buildSystemPrompt(context.Background())}, {Role: "user", Content: tc.query}},
				nil, "session", "message")
			require.NoError(t, err)
			require.Equal(t, 2, model.callCount, "检索后必须再次调用模型")
			require.True(t, state.IsComplete)
			require.Equal(t, state.FinalAnswer, answers.String())
			require.NotContains(t, state.FinalAnswer, "以下候选原题")
			for _, id := range tc.cited {
				require.Contains(t, state.FinalAnswer, `chunk_id="chunk-`+id+`"`)
			}
			for _, id := range tc.excluded {
				require.NotContains(t, state.FinalAnswer, `chunk_id="chunk-`+id+`"`)
			}
			observation := model.calls[1][len(model.calls[1])-1].Content
			if tc.empty {
				require.Empty(t, state.KnowledgeRefs)
				require.Contains(t, observation, "do not guess a stored answer")
			} else {
				require.Contains(t, observation, "<answer>铁路有效乘车凭证</answer>")
				require.Contains(t, observation, "<answer>应收和加收票款</answer>")
				require.Len(t, state.KnowledgeRefs, 3)
				require.Equal(t, 2, state.KnowledgeRefs[0].Question.Revision)
				require.Contains(t, state.KnowledgeRefs[0].ImageInfo, "resource://original-passenger")
			}
		})
	}
}

func TestQuestionBankReferencesDoNotFinishOrAcceptExternalTools(t *testing.T) {
	engine := newTestEngine(t, &mockChat{})
	state := &types.AgentState{}
	step := types.AgentStep{ToolCalls: []types.ToolCall{{Name: "external_tool", Result: questionSearchFixture()}}}
	engine.collectQuestionToolReferences(context.Background(), state, step, "session")
	require.Empty(t, state.KnowledgeRefs)
	step.ToolCalls[0].Name = agenttools.ToolSearchKnowledge
	engine.collectQuestionToolReferences(context.Background(), state, step, "session")
	engine.collectQuestionToolReferences(context.Background(), state, step, "session")
	require.Len(t, state.KnowledgeRefs, 3, "重复检索不重复保存快照")
	require.False(t, state.IsComplete)
	require.Empty(t, state.FinalAnswer)
}
