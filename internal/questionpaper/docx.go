// Package questionpaper 生成仅包含题干、原始选项和可选答案的 Word 试卷。
package questionpaper

import (
	"archive/zip"
	"bytes"
	"encoding/xml"
	"fmt"
	"strings"

	"github.com/Tencent/WeKnora/internal/types"
)

const ContentType = "application/vnd.openxmlformats-officedocument.wordprocessingml.document"

// Build 使用标准 OOXML 封装生成可编辑的 .docx，不依赖 Office 或外部转换服务。
func Build(title string, sections []types.QuestionPaperSection, includeAnswers bool) ([]byte, error) {
	var body strings.Builder
	paragraph(&body, title, "Title", false)
	number := 0
	for _, section := range sections {
		paragraph(&body, fmt.Sprintf("%s（%d 道）", types.QuestionTypeLabel(section.QuestionType), len(section.Questions)), "Heading1", false)
		for _, question := range section.Questions {
			number++
			paragraph(&body, fmt.Sprintf("%d. %s", number, question.Stem), "Question", len(question.Options) > 0)
			for i, option := range question.Options {
				paragraph(&body, option.Key+". "+option.Text, "Option", i < len(question.Options)-1)
			}
		}
	}
	if includeAnswers {
		// 答案只遍历同一组抽题快照；关闭开关时不把答案或原图信息写入任何部件。
		body.WriteString(`<w:p><w:r><w:br w:type="page"/></w:r></w:p>`)
		paragraph(&body, "参考答案", "Heading1", false)
		number = 0
		for _, section := range sections {
			for _, question := range section.Questions {
				number++
				paragraph(&body, fmt.Sprintf("%d. %s", number, answerText(question)), "Question", false)
			}
		}
	}
	// 统一正文与中文字体，并通过悬挂缩进对齐换行后的题干。
	document := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?><w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body>` + body.String() + `<w:sectPr><w:pgSz w:w="12240" w:h="15840"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr></w:body></w:document>`
	parts := []struct{ name, content string }{
		{"[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8"?><Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types"><Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/><Default Extension="xml" ContentType="application/xml"/><Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/><Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/></Types>`},
		{"_rels/.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/></Relationships>`},
		{"word/_rels/document.xml.rels", `<?xml version="1.0" encoding="UTF-8"?><Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships"><Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/></Relationships>`},
		{"word/styles.xml", stylesXML},
		{"word/document.xml", document},
	}
	var output bytes.Buffer
	archive := zip.NewWriter(&output)
	for _, part := range parts {
		entry, err := archive.Create(part.name)
		if err != nil {
			return nil, err
		}
		if _, err := entry.Write([]byte(part.content)); err != nil {
			return nil, err
		}
	}
	if err := archive.Close(); err != nil {
		return nil, err
	}
	return output.Bytes(), nil
}

func answerText(question *types.Question) string {
	if question.QuestionType != types.QuestionFill || len(question.Answer.Blanks) < 2 {
		return question.AnswerText()
	}
	parts := make([]string, len(question.Answer.Blanks))
	for i, answer := range question.Answer.Blanks {
		parts[i] = fmt.Sprintf("（%d）%s", i+1, answer)
	}
	return strings.Join(parts, "；")
}

func paragraph(body *strings.Builder, text, style string, keepNext bool) {
	fmt.Fprintf(body, `<w:p><w:pPr><w:pStyle w:val="%s"/>`, style)
	if keepNext {
		body.WriteString(`<w:keepNext/>`)
	}
	body.WriteString(`</w:pPr><w:r>`)
	// 题目来自外部识别与人工输入：保留换行、制表符，去除 XML 1.0 不允许的控制字符。
	text = strings.Map(func(r rune) rune {
		if r == '\t' || r == '\n' || r == '\r' || r >= 0x20 && r <= 0xd7ff || r >= 0xe000 && r <= 0xfffd || r >= 0x10000 && r <= 0x10ffff {
			return r
		}
		return -1
	}, text)
	text = strings.ReplaceAll(strings.ReplaceAll(text, "\r\n", "\n"), "\r", "\n")
	for i, line := range strings.Split(text, "\n") {
		if i > 0 {
			body.WriteString(`<w:br/>`)
		}
		for j, part := range strings.Split(line, "\t") {
			if j > 0 {
				body.WriteString(`<w:tab/>`)
			}
			body.WriteString(`<w:t xml:space="preserve">`)
			var escaped bytes.Buffer
			_ = xml.EscapeText(&escaped, []byte(part))
			body.WriteString(escaped.String())
			body.WriteString(`</w:t>`)
		}
	}
	body.WriteString(`</w:r></w:p>`)
}

const stylesXML = `<?xml version="1.0" encoding="UTF-8"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
  <w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Times New Roman" w:hAnsi="Times New Roman" w:eastAsia="宋体"/><w:color w:val="000000"/><w:sz w:val="24"/><w:lang w:val="zh-CN" w:eastAsia="zh-CN"/></w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="360" w:lineRule="auto"/><w:widowControl/></w:pPr></w:pPrDefault></w:docDefaults>
  <w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
  <w:style w:type="paragraph" w:styleId="Title"><w:name w:val="Title"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:after="320"/><w:jc w:val="center"/></w:pPr><w:rPr><w:b/><w:sz w:val="36"/></w:rPr></w:style>
  <w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="160"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="28"/></w:rPr></w:style>
  <w:style w:type="paragraph" w:styleId="Question"><w:name w:val="Question"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:before="160" w:after="120"/><w:ind w:left="600" w:hanging="600"/></w:pPr></w:style>
  <w:style w:type="paragraph" w:styleId="Option"><w:name w:val="Option"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="60"/><w:ind w:left="960" w:hanging="360"/></w:pPr></w:style>
</w:styles>`
