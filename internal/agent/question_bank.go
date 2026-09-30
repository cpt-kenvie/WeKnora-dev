package agent

import (
	"context"
	"encoding/json"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

// answerQuestionToolResults 只读取内置检索工具的题目快照，外部工具不能伪造原题卡片。
func (e *AgentEngine) answerQuestionToolResults(ctx context.Context, query string, state *types.AgentState, step types.AgentStep, sessionID string) bool {
	refs := []*types.SearchResult{}
	questionBankOnly := false
	for _, call := range step.ToolCalls {
		if call.Name != agenttools.ToolSearchKnowledge || call.Result == nil || !call.Result.Success {
			continue
		}
		if only, ok := call.Result.Data["question_bank_only"].(bool); ok && only {
			questionBankOnly = true
		}
		rows, ok := call.Result.Data["results"].([]map[string]interface{})
		if !ok {
			continue
		}
		for _, row := range rows {
			q, ok := row["question"].(*types.QuestionSnapshot)
			if !ok || q == nil {
				continue
			}
			chunkID, _ := row["chunk_id"].(string)
			images, _ := json.Marshal([]types.ImageInfo{{URL: q.ImageRef, OriginalURL: q.ImageRef}})
			refs = append(refs, &types.SearchResult{ID: chunkID, KnowledgeID: q.KnowledgeID, KnowledgeBaseID: q.KnowledgeBaseID,
				ChunkType: types.ChunkTypeQuestion, Content: q.SearchText(), ImageInfo: string(images), Question: q})
		}
	}
	refs, _ = types.SelectQuestionResults(query, refs)
	if len(refs) == 0 && !questionBankOnly {
		return false
	}
	state.KnowledgeRefs = refs
	state.FinalAnswer = types.QuestionAnswerMarkdown(query, refs)
	state.IsComplete = true
	e.eventBus.Emit(ctx, event.Event{ID: generateEventID("references"), Type: event.EventAgentReferences, SessionID: sessionID,
		Data: event.AgentReferencesData{References: refs}})
	e.eventBus.Emit(ctx, event.Event{ID: generateEventID("answer"), Type: event.EventAgentFinalAnswer, SessionID: sessionID,
		Data: event.AgentFinalAnswerData{Content: state.FinalAnswer, Done: true}})
	return true
}
