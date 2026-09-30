package questionpaper

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"io"
	"strings"
	"testing"

	"github.com/Tencent/WeKnora/internal/types"
	"github.com/stretchr/testify/require"
)

func sampleSections() []types.QuestionPaperSection {
	truth := false
	return []types.QuestionPaperSection{
		{QuestionType: types.QuestionSingle, Questions: []*types.Question{{QuestionContent: types.QuestionContent{
			QuestionType: types.QuestionSingle, Stem: "哪项满足 a < b & c > d？\n请保留中文与换行。", Options: []types.QuestionOption{{Key: "B", Text: "先列 B"}, {Key: "A", Text: "后列 A"}}, Answer: types.QuestionAnswer{OptionKeys: []string{"A"}}, AnswerRaw: "不得导出的原始答案"}, ImageRef: "resource://private-original"}}},
		{QuestionType: types.QuestionMultiple, Questions: []*types.Question{{QuestionContent: types.QuestionContent{
			QuestionType: types.QuestionMultiple, Stem: "选择全部正确项。", Options: []types.QuestionOption{{Key: "A", Text: "甲"}, {Key: "B", Text: "乙"}, {Key: "C", Text: "丙"}}, Answer: types.QuestionAnswer{OptionKeys: []string{"A", "C"}}}}}},
		{QuestionType: types.QuestionJudgment, Questions: []*types.Question{{QuestionContent: types.QuestionContent{
			QuestionType: types.QuestionJudgment, Stem: "判断这句话。", Answer: types.QuestionAnswer{Truth: &truth}}}}},
		{QuestionType: types.QuestionFill, Questions: []*types.Question{{QuestionContent: types.QuestionContent{
			QuestionType: types.QuestionFill, Stem: "填写____和____。", BlankCount: 2, Answer: types.QuestionAnswer{Blanks: []string{"专属答案甲", "专属答案乙"}}}}}},
	}
}

func readPaper(t *testing.T, data []byte) map[string]string {
	t.Helper()
	archive, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	require.NoError(t, err)
	parts := make(map[string]string)
	for _, entry := range archive.File {
		reader, err := entry.Open()
		require.NoError(t, err)
		content, err := io.ReadAll(reader)
		require.NoError(t, err)
		require.NoError(t, reader.Close())
		decoder := xml.NewDecoder(bytes.NewReader(content))
		for {
			_, err := decoder.Token()
			if err == io.EOF {
				break
			}
			require.NoError(t, err, entry.Name)
		}
		parts[entry.Name] = string(content)
	}
	return parts
}

func paperParagraphs(t *testing.T, document string) []string {
	t.Helper()
	decoder := xml.NewDecoder(strings.NewReader(document))
	var paragraphs []string
	var paragraph strings.Builder
	inText := false
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			break
		}
		require.NoError(t, err)
		switch value := token.(type) {
		case xml.StartElement:
			if value.Name.Local == "t" {
				inText = true
			}
			if value.Name.Local == "br" {
				paragraph.WriteByte('\n')
			}
		case xml.CharData:
			if inText {
				paragraph.Write(value)
			}
		case xml.EndElement:
			if value.Name.Local == "t" {
				inText = false
			}
			if value.Name.Local == "p" {
				paragraphs = append(paragraphs, paragraph.String())
				paragraph.Reset()
			}
		}
	}
	return paragraphs
}

func TestBuildPaperNumbersOptionsAndMatchingAnswers(t *testing.T) {
	data, err := Build("安全知识试卷", sampleSections(), true)
	require.NoError(t, err)
	parts := readPaper(t, data)
	require.Len(t, parts, 5)
	paragraphs := paperParagraphs(t, parts["word/document.xml"])
	require.Equal(t, []string{
		"安全知识试卷", "单选题（1 道）", "1. 哪项满足 a < b & c > d？\n请保留中文与换行。", "B. 先列 B", "A. 后列 A",
		"多选题（1 道）", "2. 选择全部正确项。", "A. 甲", "B. 乙", "C. 丙",
		"判断题（1 道）", "3. 判断这句话。", "填空题（1 道）", "4. 填写____和____。",
		"\n", "参考答案", "1. A：后列 A", "2. A：甲；C：丙", "3. 错误", "4. （1）专属答案甲；（2）专属答案乙",
	}, paragraphs)
	require.Contains(t, parts["word/document.xml"], `<w:br w:type="page"/>`)
	require.Contains(t, parts["word/styles.xml"], `w:eastAsia="宋体"`)
	for _, part := range parts {
		require.NotContains(t, part, "private-original")
		require.NotContains(t, part, "不得导出的原始答案")
	}
}

func TestBuildPaperWithoutAnswersDoesNotEmbedAnswerData(t *testing.T) {
	data, err := Build("试题", sampleSections(), false)
	require.NoError(t, err)
	for _, part := range readPaper(t, data) {
		for _, hidden := range []string{"参考答案", "专属答案甲", "专属答案乙", "不得导出的原始答案", "private-original"} {
			require.NotContains(t, part, hidden)
		}
	}
}

func TestBuildPaperSanitizesXMLControlsAndPreservesUnicode(t *testing.T) {
	sections := sampleSections()
	sections[0].Questions[0].Stem = "中文\x00\v\t字符\r\n<标签> 🚆"
	data, err := Build("题库 & 试卷", sections, false)
	require.NoError(t, err)
	document := readPaper(t, data)["word/document.xml"]
	require.Contains(t, document, "中文")
	require.Contains(t, document, "<w:tab/>")
	require.Contains(t, document, "&lt;标签&gt; 🚆")
	require.NotContains(t, document, "�")
}
