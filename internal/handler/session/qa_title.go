package session

import (
	"context"
	"strings"
	"sync"

	"github.com/Tencent/WeKnora/internal/event"
)

// setupTitleGeneration 对纯图片消息等待识别结果，其余消息直接使用用户文字。
func (h *Handler) setupTitleGeneration(streamCtx *sseStreamContext, reqCtx *qaRequestContext, enabled bool) {
	if !enabled || reqCtx.session.Title != "" {
		return
	}
	modelID := reqCtx.summaryModelID
	if modelID == "" && reqCtx.customAgent != nil {
		modelID = reqCtx.customAgent.Config.ModelID
	}
	var once sync.Once
	generate := func(query string) {
		if strings.TrimSpace(query) == "" {
			return
		}
		once.Do(func() {
			// 标题异步修改独立副本，避免与问答线程读取会话标题产生竞争。
			session := *reqCtx.session
			h.sessionService.GenerateTitleAsync(streamCtx.asyncCtx, &session, query, modelID, streamCtx.eventBus)
		})
	}
	if strings.TrimSpace(reqCtx.query) != "" {
		generate(reqCtx.query)
		return
	}
	streamCtx.eventBus.On(event.EventQueryRewritten, func(_ context.Context, evt event.Event) error {
		if data, ok := evt.Data.(event.QueryData); ok {
			generate(data.RewrittenQuery)
		}
		return nil
	})
}
