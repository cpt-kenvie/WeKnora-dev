package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// processQuestionImage 使用原文件读取通道，完整原图始终由知识文件持有。
func (s *knowledgeService) processQuestionImage(ctx context.Context, kb *types.KnowledgeBase, knowledge *types.Knowledge, eff types.EffectiveProcessConfig, lastRetry bool) error {
	attempt := attemptFromCtx(ctx)
	span := s.tracker().BeginStage(ctx, knowledge.ID, attempt, "题目识别", types.JSONMap{"file_name": knowledge.FileName})
	err := func() error {
		if !types.IsQuestionImageType(knowledge.FileType) {
			return fmt.Errorf("题库仅支持上传题目图片")
		}
		reader, err := s.resolveFileServiceForPath(ctx, kb, knowledge.FilePath).GetFile(ctx, knowledge.FilePath)
		if err != nil {
			return err
		}
		defer reader.Close()
		// 与上传限额之外再设置读取上限，避免异常文件耗尽工作进程内存。
		const maxQuestionImageBytes = 30 << 20
		image, err := io.ReadAll(io.LimitReader(reader, maxQuestionImageBytes+1))
		if err != nil {
			return err
		}
		if len(image) > maxQuestionImageBytes {
			return fmt.Errorf("题目图片不能超过 30 MB")
		}
		switch http.DetectContentType(image) {
		case "image/jpeg", "image/png", "image/webp", "image/bmp":
		default:
			return fmt.Errorf("文件内容不是支持的题目图片（JPG、PNG、WebP、BMP）")
		}
		if engine := types.QuestionImageParserEngine(eff.ChunkingConfig, knowledge.FileType); engine != "" {
			text, err := s.parseQuestionImage(ctx, knowledge, eff, engine, image)
			if err != nil {
				return err
			}
			return s.questionBank.ExtractParsed(ctx, kb, knowledge, text)
		}
		// 上传或重新识别的模型覆盖仅对本次任务有效，不修改知识库配置。
		resolvedKB := *kb
		resolvedKB.VLMConfig = eff.VLMConfig
		return s.questionBank.Extract(ctx, &resolvedKB, knowledge, image)
	}()
	if aborted, status := s.isKnowledgeAborted(ctx, knowledge.TenantID, knowledge.ID); aborted {
		return abortRetryErr(ctx, knowledge.ID, status)
	}
	if s.isKnowledgeSourceReplaced(ctx, knowledge) || attemptSuperseded(ctx, s.tracker(), knowledge.ID, attempt) {
		return nil
	}
	if err != nil {
		knowledge.ErrorMessage = err.Error()
		if lastRetry {
			knowledge.ParseStatus = types.ParseStatusFailed
			s.tracker().FailSpan(ctx, span, "QUESTION_EXTRACTION_FAILED", err.Error(), err)
			s.tracker().FinalizeAttempt(ctx, knowledge.ID, attempt, types.SpanStatusFailed, nil, "QUESTION_EXTRACTION_FAILED", err.Error())
		}
		if !lastRetry {
			s.tracker().FailSpan(ctx, span, "QUESTION_EXTRACTION_RETRY", err.Error(), err)
		}
		if saveErr := s.questionBank.repo.UpdateSourceState(ctx, knowledge, attempt); saveErr != nil {
			return saveErr
		}
		return err
	}
	now := time.Now()
	knowledge.ParseStatus, knowledge.EnableStatus, knowledge.ErrorMessage = types.ParseStatusCompleted, "enabled", ""
	knowledge.ProcessedAt, knowledge.PendingSubtasksCount = &now, 0
	if err := s.questionBank.repo.UpdateSourceState(ctx, knowledge, attempt); err != nil {
		return err
	}
	s.tracker().EndSpan(ctx, span, nil)
	s.tracker().FinalizeAttempt(ctx, knowledge.ID, attempt, types.SpanStatusDone, nil, "", "")
	return nil
}

// parseQuestionImage 复用已有解析器和超时设置，题库任务状态仍由独立更新通道维护。
func (s *knowledgeService) parseQuestionImage(ctx context.Context, source *types.Knowledge, eff types.EffectiveProcessConfig, engine string, image []byte) (string, error) {
	processOverrides, err := source.ProcessOverrides()
	if err != nil {
		return "", err
	}
	var uploadOverrides map[string]string
	if processOverrides != nil {
		uploadOverrides = processOverrides.ParserEngineOverrides
	}
	overrides := MergeParserEngineOverrides(s.getParserEngineOverridesFromContext(ctx), uploadOverrides)
	applyParserRuleOverrides(overrides, eff.ChunkingConfig, source.FileType)
	if err := validateParserEngineOverrideURLs(overrides); err != nil {
		return "", fmt.Errorf("图片解析引擎地址不可用：%w", err)
	}
	reader := s.resolveDocReader(ctx, engine, source.FileType, false, overrides)
	if reader == nil {
		return "", fmt.Errorf("图片解析引擎 %s 不可用，请检查解析引擎配置", engine)
	}
	result, err := s.callDocReaderWithTimeout(ctx, reader, &types.ReadRequest{
		FileContent: image, FileName: source.FileName, FileType: normalizeFileExtension(source.FileType),
		Title: source.Title, ParserEngine: engine, RequestID: source.ID,
		ParserEngineOverrides: overrides,
	})
	if err != nil {
		return "", fmt.Errorf("图片解析引擎 %s 识别失败：%w", engine, err)
	}
	if result == nil {
		return "", fmt.Errorf("图片解析引擎 %s 未返回结果", engine)
	}
	sanitizeReadResult(result)
	if result.Error != "" {
		return "", fmt.Errorf("图片解析引擎 %s：%s", engine, result.Error)
	}
	return strings.TrimSpace(result.MarkdownContent), nil
}
