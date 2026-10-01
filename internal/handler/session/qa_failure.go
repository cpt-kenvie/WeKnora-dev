package session

import (
	"context"
	"strings"
	"time"

	"github.com/Tencent/WeKnora/internal/logger"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/google/uuid"
)

// failQuickAnswerTurn 保存失败状态，再发布结束事件；错误不写入对话知识库或长期记忆。
func (h *Handler) failQuickAnswerTurn(ctx context.Context, streamCtx *sseStreamContext, failure string) {
	if streamCtx.releaseTurn != nil {
		defer streamCtx.releaseTurn()
	}
	message := streamCtx.assistantMessage
	if strings.TrimSpace(message.Content) == "" {
		message.Content = failure
	}
	message.IsCompleted = true
	message.UpdatedAt = time.Now()
	if err := h.messageService.UpdateMessage(ctx, message); err != nil {
		logger.Errorf(ctx, "Failed to persist failed assistant message %s: %v", message.ID, err)
		return
	}
	if err := h.streamManager.AppendEvent(ctx, message.SessionID, message.ID, interfaces.StreamEvent{
		ID: uuid.New().String(), Type: types.ResponseTypeComplete, Done: true, Timestamp: time.Now(),
		Data: map[string]interface{}{"final_content": message.Content},
	}); err != nil {
		logger.Errorf(ctx, "Failed to publish failed turn completion: %v", err)
	}
}
