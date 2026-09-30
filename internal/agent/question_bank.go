package agent

import (
	"context"
	"encoding/json"

	agenttools "github.com/Tencent/WeKnora/internal/agent/tools"
	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

// collectQuestionToolReferences 只保存内置检索工具的原题快照；是否采用由后续模型回答中的引用决定。
func (e *AgentEngine) collectQuestionToolReferences(ctx context.Context, state *types.AgentState, step types.AgentStep, sessionID string) {
	refs := []*types.SearchResult{}
	seen := make(map[string]bool, len(state.KnowledgeRefs))
	for _, ref := range state.KnowledgeRefs {
		seen[ref.ID] = true
	}
	for _, call := range step.ToolCalls {
		if call.Name != agenttools.ToolSearchKnowledge || call.Result == nil || !call.Result.Success {
			continue
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
			if chunkID == "" || seen[chunkID] {
				continue
			}
			seen[chunkID] = true
			title, _ := row["knowledge_title"].(string)
			images, _ := json.Marshal([]types.ImageInfo{{URL: q.ImageRef, OriginalURL: q.ImageRef}})
			refs = append(refs, &types.SearchResult{ID: chunkID, KnowledgeID: q.KnowledgeID, KnowledgeBaseID: q.KnowledgeBaseID,
				KnowledgeTitle: title, ChunkType: types.ChunkTypeQuestion, Content: q.SearchText(), ImageInfo: string(images), Question: q})
		}
	}
	if len(refs) == 0 {
		return
	}
	state.KnowledgeRefs = append(state.KnowledgeRefs, refs...)
	e.eventBus.Emit(ctx, event.Event{ID: generateEventID("references"), Type: event.EventAgentReferences, SessionID: sessionID,
		Data: event.AgentReferencesData{References: refs}})
}
