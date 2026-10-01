package session

import (
	"context"
	"testing"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

func TestQuickAnswerFailurePersistsBeforeCompletion(t *testing.T) {
	for _, content := range []string{"", "已经返回的部分正文"} {
		messages := &imageCompletionMessages{}
		stream := &imageCompletionStream{}
		h := &Handler{messageService: messages, streamManager: stream}
		ctx := types.WithExecutionTenant(context.Background(), 42)
		msg := &types.Message{ID: "m", SessionID: "s", Role: "assistant", Content: content}
		bus := event.NewEventBus()
		handler := h.setupStreamHandler(ctx, "s", "m", "req", 42, time.Now(), msg, bus)
		stream.onComplete = func() {
			require.NotNil(t, messages.saved)
			require.True(t, messages.saved.IsCompleted)
			require.Equal(t, uint64(42), messages.tenant)
		}
		released := false
		h.failQuickAnswerTurn(ctx, &sseStreamContext{
			assistantMessage: msg, streamHandler: handler, eventBus: bus,
			releaseTurn: func() { released = true },
		}, "图片识别失败，请重试")
		require.True(t, released)
		if content == "" {
			require.Equal(t, "图片识别失败，请重试", messages.saved.Content)
		} else {
			require.Equal(t, content, messages.saved.Content)
		}
		require.Equal(t, types.ResponseTypeComplete, stream.events[len(stream.events)-1].Type)
	}
}

func TestImageRecognitionErrorClosesSSEWithoutWaitingForTitle(t *testing.T) {
	h := &Handler{streamManager: &stubStreamManager{events: []interfaces.StreamEvent{
		{ID: "failed", Type: types.ResponseTypeError, Content: "图片识别失败", Done: true},
	}}}
	c, recorder := newTestGinContext(t, "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	c.Request = c.Request.WithContext(ctx)
	h.handleAgentEventsForSSE(ctx, c, "s", "m", "req", nil, true, nil)
	require.NoError(t, ctx.Err(), "错误必须直接结束流，不能等待客户端超时")
	require.Contains(t, recorder.Body.String(), `"response_type":"error"`)
}
