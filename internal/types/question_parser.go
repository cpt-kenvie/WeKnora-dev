package types

import (
	"fmt"
	"strings"
)

// 题库允许上传的原图格式；解析规则按实际扩展名选择。
var questionImageTypes = [...]string{"jpg", "jpeg", "png", "bmp", "webp"}

func IsQuestionImageType(fileType string) bool {
	fileType = normalizeParserFileType(fileType)
	for _, supported := range questionImageTypes {
		if fileType == supported {
			return true
		}
	}
	return false
}

// QuestionImageParserEngine 保留旧题库的视觉识别路径，外部引擎只在明确选择时调用。
// 内置图片解析器只生成图片引用，不能作为 OCR 文本来源。
func QuestionImageParserEngine(config ChunkingConfig, fileType string) string {
	rule := config.ResolveParserEngineRule(fileType)
	if rule == nil {
		return ""
	}
	switch engine := strings.TrimSpace(rule.Engine); engine {
	case "", "simple", "builtin":
		return ""
	default:
		return engine
	}
}

// ValidateQuestionBankConfig 允许视觉识别或解析引擎识别；具体格式在上传时再次检查。
func ValidateQuestionBankConfig(kb *KnowledgeBase) error {
	if kb.Type != KnowledgeBaseTypeQuestionBank {
		return nil
	}
	if !kb.NeedsEmbeddingModel() || kb.EmbeddingModelID == "" {
		return fmt.Errorf("题库需要启用检索并配置向量模型")
	}
	hasParser := false
	for _, fileType := range questionImageTypes {
		if QuestionImageParserEngine(kb.ChunkingConfig, fileType) != "" {
			hasParser = true
		}
	}
	if hasParser && strings.TrimSpace(kb.SummaryModelID) == "" {
		return fmt.Errorf("题库使用解析引擎识图时需要配置语言模型，用于整理题干和答案")
	}
	if !hasParser && (!kb.VLMConfig.IsEnabled() || kb.VLMConfig.ModelID == "") {
		return fmt.Errorf("题库需要配置视觉识别模型，或在索引与解析中选择图片解析引擎")
	}
	return nil
}
