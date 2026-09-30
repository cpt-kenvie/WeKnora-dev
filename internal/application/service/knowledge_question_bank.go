package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/Tencent/WeKnora/internal/types"
)

// processQuestionImage 使用原文件读取通道，完整原图始终由知识文件持有。
func (s *knowledgeService) processQuestionImage(ctx context.Context, kb *types.KnowledgeBase, knowledge *types.Knowledge, lastRetry bool) error {
	attempt := attemptFromCtx(ctx)
	span := s.tracker().BeginStage(ctx, knowledge.ID, attempt, "题目识别", types.JSONMap{"file_name": knowledge.FileName})
	err := func() error {
		if !IsImageType(knowledge.FileType) {
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
		return s.questionBank.Extract(ctx, kb, knowledge, image)
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
