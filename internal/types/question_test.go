package types

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestQuestionValidation(t *testing.T) {
	truth := true
	cases := []struct {
		name    string
		content QuestionContent
		valid   bool
	}{
		{"单选", QuestionContent{QuestionType: QuestionSingle, Stem: "应追补（）", Options: []QuestionOption{{"A", "应收票款"}, {"C", "应收和加收票款"}}, Answer: QuestionAnswer{OptionKeys: []string{"C"}}}, true},
		{"反向判断选项", QuestionContent{QuestionType: QuestionJudgment, Stem: "班前安全教育", Options: []QuestionOption{{"A", "错"}, {"B", "对"}}, Answer: QuestionAnswer{OptionKeys: []string{"B"}, Truth: &truth}}, true},
		{"禁止固定A映射", QuestionContent{QuestionType: QuestionJudgment, Stem: "班前安全教育", Options: []QuestionOption{{"A", "错"}, {"B", "对"}}, Answer: QuestionAnswer{OptionKeys: []string{"A"}, Truth: &truth}}, false},
		{"多空顺序", QuestionContent{QuestionType: QuestionFill, Stem: "____与____", BlankCount: 2, Answer: QuestionAnswer{Blanks: []string{"张三、李四", "王五"}}}, true},
		{"缺失空答案", QuestionContent{QuestionType: QuestionFill, Stem: "____与____", BlankCount: 2, Answer: QuestionAnswer{Blanks: []string{"张三"}}}, false},
		{"非法选项引用", QuestionContent{QuestionType: QuestionMultiple, Stem: "应急疏散", Options: []QuestionOption{{"A", "电梯"}, {"B", "楼梯"}}, Answer: QuestionAnswer{OptionKeys: []string{"Z"}}}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.content.Normalize()
			require.Equal(t, tc.valid, len(tc.content.ValidationIssues()) == 0)
		})
	}
}

func TestQuestionMatchingPreservesMeaning(t *testing.T) {
	for _, pair := range [][2]string{{"可以", "不可以"}, {"1.5", "15"}, {"-1", "1"}, {"a_b", "ab"}} {
		require.NotEqual(t, QuestionFingerprint(pair[0]), QuestionFingerprint(pair[1]))
	}
	require.Equal(t, QuestionFingerprint("车站（ ）"), QuestionFingerprint("车站()"))
	q := &QuestionSnapshot{ID: "q1", Revision: 1, QuestionContent: QuestionContent{QuestionType: QuestionFill, Stem: "旅客是指持有____的乘车人。", BlankCount: 1, Answer: QuestionAnswer{Blanks: []string{"铁路有效乘车凭证"}}}}
	refs, exact := SelectQuestionResults(q.Stem, []*SearchResult{{Question: q}, {Question: q}})
	require.True(t, exact)
	require.Len(t, refs, 1)
	other := *q
	other.ID = "q2"
	_, exact = SelectQuestionResults(q.Stem, []*SearchResult{{Question: q}, {Question: &other}})
	require.False(t, exact)
	require.Contains(t, QuestionAnswerMarkdown("旅客", refs), "候选原题")
	require.Contains(t, QuestionAnswerMarkdown(q.Stem, refs), "铁路有效乘车凭证")
}

func TestQuestionSnapshotRoundTripAndEscaping(t *testing.T) {
	q := Question{ID: "q", Revision: 2, ImageRef: "resource://original", QuestionContent: QuestionContent{QuestionType: QuestionFill, Stem: "<script>alert(1)</script>____", Answer: QuestionAnswer{Blanks: []string{"答案"}}, BlankCount: 1}}
	encoded, err := json.Marshal(q.Snapshot())
	require.NoError(t, err)
	snapshot := QuestionFromChunk(ChunkTypeQuestion, JSON(encoded))
	require.NotNil(t, snapshot)
	q.Stem = "人工修正后的新题干"
	require.NotEqual(t, q.Stem, snapshot.Stem)
	require.Nil(t, QuestionFromChunk(ChunkTypeText, JSON(encoded)))
	text := QuestionAnswerMarkdown(snapshot.Stem, []*SearchResult{{Question: snapshot}})
	require.NotContains(t, text, "<script>")
	require.Contains(t, text, "![题目原图](resource://original)")
}
