package types

import (
	"fmt"
	"strings"
)

// SelectQuestionResults 仅把完整题干匹配视为精确命中；语义相近题始终以候选呈现。
func SelectQuestionResults(query string, results []*SearchResult) ([]*SearchResult, bool) {
	questions, exact := []*SearchResult{}, []*SearchResult{}
	seen := map[string]bool{}
	normalized := NormalizeQuestionStem(query)
	for _, result := range results {
		if result == nil || result.Question == nil || seen[result.Question.ID] {
			continue
		}
		seen[result.Question.ID] = true
		questions = append(questions, result)
		stem := NormalizeQuestionStem(result.Question.Stem)
		if stem != "" && normalized == stem {
			exact = append(exact, result)
		}
	}
	if len(exact) == 1 {
		return exact, true
	}
	if len(exact) > 1 {
		questions = exact
	}
	if len(questions) > 5 {
		questions = questions[:5]
	}
	return questions, false
}

// QuestionAnswerMarkdown 完全使用入库快照，模型不参与改写题干、选项和答案。
func QuestionAnswerMarkdown(query string, results []*SearchResult) string {
	questions, exact := SelectQuestionResults(query, results)
	if len(questions) == 0 {
		return "题库中没有找到可靠匹配的原题，请补充题干或更具体的关键词。"
	}
	var b strings.Builder
	if exact {
		b.WriteString("找到以下原题：\n\n")
	} else {
		b.WriteString("找到以下候选原题，请核对题干和选项；各答案仅对应所在题目。\n\n")
	}
	labels := map[string]string{QuestionSingle: "单选题", QuestionMultiple: "多选题", QuestionJudgment: "判断题", QuestionFill: "填空题"}
	escape := strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;", "\\", "\\\\", "*", "\\*", "_", "\\_", "`", "\\`", "[", "\\[", "]", "\\]", "#", "\\#")
	for _, result := range questions {
		q := result.Question
		fmt.Fprintf(&b, "**%s**\n\n%s\n\n", labels[q.QuestionType], escape.Replace(q.Stem))
		for _, o := range q.Options {
			fmt.Fprintf(&b, "%s. %s  \n", escape.Replace(o.Key), escape.Replace(o.Text))
		}
		fmt.Fprintf(&b, "\n**正确答案：%s**\n\n", escape.Replace(q.AnswerText()))
		if q.ImageRef != "" {
			fmt.Fprintf(&b, "![题目原图](%s)\n\n", q.ImageRef)
		}
	}
	return b.String()
}
