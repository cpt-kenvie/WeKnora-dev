package modelcontext

import (
	"encoding/json"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestQuestionBankModelContextUsesSnapshotAfterJSONRoundTrip(t *testing.T) {
	truth := false
	for _, content := range []types.QuestionContent{
		{QuestionType: types.QuestionFill, Stem: "旅客持有____。", BlankCount: 1, Answer: types.QuestionAnswer{Blanks: []string{"铁路有效乘车凭证"}}},
		{QuestionType: types.QuestionSingle, Stem: "单选题", Options: []types.QuestionOption{{Key: "A", Text: "选项甲"}, {Key: "B", Text: "选项乙"}}, Answer: types.QuestionAnswer{OptionKeys: []string{"B"}}},
		{QuestionType: types.QuestionMultiple, Stem: "多选题", Options: []types.QuestionOption{{Key: "A", Text: "选项甲"}, {Key: "B", Text: "选项乙"}}, Answer: types.QuestionAnswer{OptionKeys: []string{"A", "B"}}},
		{QuestionType: types.QuestionJudgment, Stem: "判断题", Answer: types.QuestionAnswer{Truth: &truth}},
	} {
		t.Run(content.QuestionType, func(t *testing.T) {
			q := &types.QuestionSnapshot{ID: "question", Revision: 3, KnowledgeID: "image", KnowledgeBaseID: "bank",
				ImageRef: "resource://AbCdEfGhIjKlMnOpQrStUv", QuestionContent: content}
			result := &types.ToolResult{Success: true, Data: map[string]interface{}{
				"display_type": "search_results", "results": []map[string]interface{}{{
					"chunk_id": "version-chunk", "knowledge_id": "image", "knowledge_base_id": "bank",
					"content": "过期索引内容", "question": q,
				}},
			}}
			encoded, err := json.Marshal(result)
			require.NoError(t, err)
			var restored types.ToolResult
			require.NoError(t, json.Unmarshal(encoded, &restored))
			for _, input := range []*types.ToolResult{result, &restored} {
				registry := NewRegistry(true)
				output := registry.ModelToolResultForTool("search_knowledge", input)
				require.Contains(t, output, `type="question"`)
				require.Contains(t, output, content.Stem)
				require.Contains(t, output, "<answer>"+content.AnswerText()+"</answer>")
				require.Contains(t, output, "![题目原图](res://")
				require.NotContains(t, output, "过期索引内容")
				require.Contains(t, registry.DecodeOutputText(`<ref id="c1"/>`), `chunk_id="version-chunk"`)
			}
		})
	}
}

func TestQuestionBankHonorsDisabledCitations(t *testing.T) {
	registry := NewRegistry(false)
	prompt := registry.ProtocolPrompt()
	require.Contains(t, prompt, "Source citations are disabled")
	require.Contains(t, prompt, "include its original question, options, stored answer and supplied original image")
	registry.RegisterChunk(ChunkReference{ChunkID: "question-chunk", ChunkType: types.ChunkTypeQuestion})
	require.Empty(t, registry.DecodeOutputText(`<ref id="c1"/>`))
}
