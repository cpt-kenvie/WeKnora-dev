package session

import (
	"context"
	"testing"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/Tencent/WeKnora/internal/types/interfaces"
	"github.com/stretchr/testify/require"
)

type imageTitleSessions struct {
	interfaces.SessionService
	queries []string
	modelID string
}

func (s *imageTitleSessions) GenerateTitleAsync(_ context.Context, session *types.Session, query, modelID string, _ *event.EventBus) {
	s.queries = append(s.queries, query)
	s.modelID = modelID
	session.Title = "图片里的消防疏散题"
}

func TestImageOnlyTitleWaitsForRecognizedContent(t *testing.T) {
	sessions := &imageTitleSessions{}
	h := &Handler{sessionService: sessions}
	bus := event.NewEventBus()
	req := &qaRequestContext{session: &types.Session{}, summaryModelID: "answer-model"}
	h.setupTitleGeneration(&sseStreamContext{asyncCtx: context.Background(), eventBus: bus}, req, true)
	require.Empty(t, sessions.queries)
	for _, query := range []string{"", "遇到火灾时能否使用电梯疏散？", "重复识图事件"} {
		require.NoError(t, bus.Emit(context.Background(), event.Event{
			Type: event.EventQueryRewritten, Data: event.QueryData{RewrittenQuery: query},
		}))
	}
	require.Equal(t, []string{"遇到火灾时能否使用电梯疏散？"}, sessions.queries)
	require.Equal(t, "answer-model", sessions.modelID)
	require.Empty(t, req.session.Title, "标题线程不能修改问答线程的会话对象")
}

func TestTitleGenerationHonorsTextAndDisableFlag(t *testing.T) {
	for _, disabled := range []bool{false, true} {
		sessions := &imageTitleSessions{}
		h := &Handler{sessionService: sessions}
		req := &qaRequestContext{session: &types.Session{}, query: "文字问题"}
		h.setupTitleGeneration(&sseStreamContext{asyncCtx: context.Background(), eventBus: event.NewEventBus()}, req, !disabled)
		if disabled {
			require.Empty(t, sessions.queries)
		} else {
			require.Equal(t, []string{"文字问题"}, sessions.queries)
		}
	}
}
