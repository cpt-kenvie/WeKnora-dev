package service

import (
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Tencent/WeKnora/internal/types"
)

const (
	// 只匹配页面标记中的数字和空白，不对题目正文做全角转换。
	questionMetaSpace     = `[\s\x{3000}]*`
	questionMetaNumber    = `[0-9０-９零〇一二三四五六七八九十百两]+`
	questionMetaScore     = questionMetaNumber + `(?:[.．][0-9０-９]+)?`
	questionScorePart     = `(?:(?:本题|该题|此题|共|满分|分值|得分|每题|每小题|每空)` + questionMetaSpace + `[:：]?` + questionMetaSpace + `)*` + questionMetaScore + questionMetaSpace + `分`
	questionScoreBody     = questionScorePart + `(?:` + questionMetaSpace + `[,，;；、]` + questionMetaSpace + questionScorePart + `)*`
	questionScoreBrackets = `(?:\(` + questionMetaSpace + questionScoreBody + questionMetaSpace + `\)|` +
		`（` + questionMetaSpace + questionScoreBody + questionMetaSpace + `）|` +
		`\[` + questionMetaSpace + questionScoreBody + questionMetaSpace + `\]|` +
		`【` + questionMetaSpace + questionScoreBody + questionMetaSpace + `】)`
	questionTypeLabel   = `(?:【(?:单选题|多选题|判断题|填空题)】|\[(?:单选题|多选题|判断题|填空题)\])`
	questionScoreLabel  = `(?:分值|得分|满分|本题(?:共)?|每题|每小题|每空)` + questionMetaSpace + `[:：]?` + questionMetaSpace + questionMetaScore + questionMetaSpace + `分`
	questionNumberLabel = `(?:第` + questionMetaSpace + questionMetaNumber + questionMetaSpace + `题|题号` + questionMetaSpace + `[:：]` + questionMetaSpace + questionMetaNumber + `)`
)

var (
	// 裸题号需要分隔符；点号后的数字和运算符由清理函数进一步保护。
	questionOrdinalPrefix = regexp.MustCompile(`^(?:` + questionMetaNumber + `[.．、:：)）]|` +
		`[（(]` + questionMetaSpace + questionMetaNumber + questionMetaSpace + `[)）]|[①-⑳⑴-⒇]|` +
		questionNumberLabel + `(?:[.．、:：]|[\s\x{3000}]+|$)|` +
		`[【\[]` + questionMetaSpace + questionNumberLabel + questionMetaSpace + `[】\]])`)
	questionScoreOnly   = regexp.MustCompile(`^` + questionScoreBrackets + `$`)
	questionScorePrefix = regexp.MustCompile(`^(` + questionTypeLabel + questionMetaSpace + `)?` + questionScoreBrackets)
	questionScoreSuffix = regexp.MustCompile(questionScoreBrackets + `$`)
	// 显式评分标签可独占一行或位于题目前后，不匹配“比赛得了2分”等正文。
	questionScoreLine = regexp.MustCompile(`(?m)^[\t \x{3000}]*` + questionScoreLabel + `[\t \x{3000}]*\r?$`)
	questionScoreHead = regexp.MustCompile(`^` + questionScoreLabel + `(?:[\s\x{3000}]+|$)`)
	questionScoreTail = regexp.MustCompile(`[\s\x{3000}]+` + questionScoreLabel + `$`)
	// 成组的小问属于完整题干，模型去掉外层题号后也不能再删掉第一个小问。
	questionFirstSubpart = regexp.MustCompile(`^(?:[（(]` + questionMetaSpace + `[1１一]` + questionMetaSpace + `[)）]|①|⑴|[1１一][.．、])$`)
	questionNextSubpart  = regexp.MustCompile(`[（(]` + questionMetaSpace + `[2２二]` + questionMetaSpace + `[)）]|②|⑵|\n[\t \x{3000}]*[2２二][.．、]`)
	questionNumberList   = regexp.MustCompile(`^` + questionMetaNumber + `[、,，]`)
)

// cleanExtractedQuestion 仅用于上传抽取边界，清理后再校验、生成指纹和检索分块。
func cleanExtractedQuestion(q *types.QuestionContent) {
	stem := strings.TrimSpace(q.Stem)
	ordinalRemoved := false
	for {
		previous := stem
		if questionScoreOnly.MatchString(stem) {
			stem = ""
		}
		stem = cleanQuestionScore(stem)
		if prefix := questionOrdinalPrefix.FindString(stem); !ordinalRemoved && prefix != "" {
			rest := stem[len(prefix):]
			adjacent, _ := utf8.DecodeRuneInString(rest)
			rest = strings.TrimSpace(rest)
			first, _ := utf8.DecodeRuneInString(rest)
			separator, _ := utf8.DecodeLastRuneInString(prefix)
			// 3.14、1:2 和（1）+（2）属于题意；不能把前半部分当成题号。
			numericValue := strings.ContainsRune(".．", separator) && unicode.IsDigit(adjacent) ||
				strings.ContainsRune(":：", separator) && unicode.IsDigit(first) ||
				separator == '、' && questionNumberList.MatchString(rest)
			subpart := questionFirstSubpart.MatchString(prefix) && questionNextSubpart.MatchString(rest)
			if strings.Contains(prefix, "题") || (!strings.ContainsRune("+-*/=<>×÷^＋－＝", first) && !numericValue && !subpart) {
				stem = rest
				ordinalRemoved = true
			}
		}
		if stem == previous {
			break
		}
	}
	q.Stem = stem
	q.AnswerRaw = cleanQuestionScore(q.AnswerRaw)
	for i := range q.Options {
		q.Options[i].Text = cleanQuestionScore(q.Options[i].Text)
	}
	for i := range q.Answer.Blanks {
		q.Answer.Blanks[i] = cleanQuestionScore(q.Answer.Blanks[i])
	}
}

func cleanQuestionScore(text string) string {
	text = strings.TrimSpace(text)
	// 单独的“（2分）”也可能是选项或答案的实际内容，交由模型按语义判断。
	if questionScoreOnly.MatchString(text) {
		return text
	}
	text = strings.TrimSpace(questionScoreLine.ReplaceAllString(text, ""))
	for {
		previous := text
		text = strings.TrimSpace(questionScorePrefix.ReplaceAllString(text, "$1"))
		text = strings.TrimSpace(questionScoreSuffix.ReplaceAllString(text, ""))
		text = strings.TrimSpace(questionScoreHead.ReplaceAllString(text, ""))
		text = strings.TrimSpace(questionScoreTail.ReplaceAllString(text, ""))
		if text == previous {
			return text
		}
	}
}
