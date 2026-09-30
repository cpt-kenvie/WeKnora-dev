package service

import (
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func TestQuestionExtractionFourTypes(t *testing.T) {
	raw := "```json\n" + `{"questions":[
  {"question_type":"fill_blank","stem":"旅客是指持有____的乘车人。","options":[],"answer":{"blanks":["铁路有效乘车凭证"]},"answer_raw":"铁路有效乘车凭证","blank_count":1},
  {"question_type":"true_false","stem":"班前安全教育成为安全生产第一道防线。","options":[{"key":"A","text":"对"},{"key":"B","text":"错"}],"answer":{"option_keys":["A"],"truth":true},"answer_raw":"A"},
  {"question_type":"multiple_choice","stem":"应急疏散应遵循哪些要求？","options":[{"key":"A","text":"要求一"},{"key":"B","text":"要求二"},{"key":"C","text":"要求三"},{"key":"D","text":"要求四"}],"answer":{"option_keys":["A","B","C","D"]},"answer_raw":"ABCD"},
  {"question_type":"single_choice","stem":"车站对列车移交或本站发现的无票乘车而又拒绝补票的人应追补（）。","options":[{"key":"A","text":"应收票款"},{"key":"B","text":"加收票款"},{"key":"C","text":"应收和加收票款"},{"key":"D","text":"应收票款和罚款"}],"answer":{"option_keys":["C"]},"answer_raw":"C"}
 ]}` + "\n```"
	questions, err := decodeQuestionExtraction(raw)
	require.NoError(t, err)
	require.Len(t, questions, 4)
	for i, q := range questions {
		require.Equal(t, i, q.SourceIndex)
		require.Equal(t, types.QuestionReady, q.ReviewStatus)
		require.True(t, q.IsEnabled)
		require.Empty(t, q.Issues)
	}
	require.Equal(t, "铁路有效乘车凭证", questions[0].Answer.Blanks[0])
	require.Equal(t, []string{"C"}, questions[3].Answer.OptionKeys)
}

func TestQuestionExtractionRequiresReviewInsteadOfGuessing(t *testing.T) {
	for _, raw := range []string{
		`{"questions":[{"question_type":"single_choice","stem":"模糊题目","options":[{"key":"A","text":"甲"},{"key":"B","text":"乙"}],"answer":{"option_keys":["A"]}}]}`,
		`{"questions":[{"question_type":"fill_blank","stem":"____和____","blank_count":2,"answer":{"blanks":["甲"]},"answer_raw":"甲"}]}`,
		`{"questions":[{"question_type":"fill_blank","stem":"____","blank_count":1,"answer":{"blanks":["甲"]},"answer_raw":"甲","issues":["水印遮挡题干"]}]}`,
	} {
		questions, err := decodeQuestionExtraction(raw)
		require.NoError(t, err)
		require.Equal(t, types.QuestionNeedsReview, questions[0].ReviewStatus)
		require.False(t, questions[0].IsEnabled)
	}
	for _, raw := range []string{`{"questions":[]}`, `not json`, `{"questions":"题目"}`} {
		_, err := decodeQuestionExtraction(raw)
		require.Error(t, err)
	}
}

func TestQuestionExtractionModelUsesStringIssues(t *testing.T) {
	raw := `{"questions":[{"question_type":"fill_blank","stem":"____","blank_count":1,"answer":{"blanks":["甲"]},"answer_raw":"甲","issues":""}]}`
	questions, err := decodeQuestionExtraction(raw)
	require.NoError(t, err)
	require.Equal(t, types.QuestionReady, questions[0].ReviewStatus)
	raw = `{"questions":[{"question_type":"fill_blank","stem":"____","blank_count":1,"answer":{"blanks":["甲"]},"answer_raw":"甲","issues":"部分文字模糊"}]}`
	questions, err = decodeQuestionExtraction(raw)
	require.NoError(t, err)
	require.Equal(t, types.QuestionNeedsReview, questions[0].ReviewStatus)
}

func TestQuestionExtractionNormalizesModelScalars(t *testing.T) {
	raw := `{"questions":[{"question_type":"true_false","stem":"班前教育。","options":[{"key":"A","text":"错"},{"key":"B","text":"对"}],"answer":{"option_keys":"B","truth":"true","blanks":""},"answer_raw":"B","blank_count":"","issues":""}]}`
	questions, err := decodeQuestionExtraction(raw)
	require.NoError(t, err)
	require.Equal(t, types.QuestionReady, questions[0].ReviewStatus)
	require.True(t, *questions[0].Answer.Truth)
}
