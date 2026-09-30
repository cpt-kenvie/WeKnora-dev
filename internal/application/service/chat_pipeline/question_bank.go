package chatpipeline

import (
	"context"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/google/uuid"
)

func questionBankAnswer(cm *types.ChatManage) (string, bool) {
	questions, _ := types.SelectQuestionResults(cm.Query, cm.MergeResult)
	if len(questions) == 0 && !cm.QuestionBankOnly {
		return "", false
	}
	return types.QuestionAnswerMarkdown(cm.Query, cm.MergeResult), true
}

func emitQuestionBankAnswer(ctx context.Context, cm *types.ChatManage, content string) *PluginError {
	if cm.EventBus == nil {
		return ErrModelCall
	}
	if err := cm.EventBus.Emit(ctx, types.Event{ID: uuid.NewString(), Type: types.EventType(event.EventAgentFinalAnswer),
		SessionID: cm.SessionID, Data: event.AgentFinalAnswerData{Content: content, Done: true}}); err != nil {
		return ErrModelCall.WithError(err)
	}
	return nil
}
