package service

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Tencent/WeKnora/internal/event"
	"github.com/Tencent/WeKnora/internal/types"
)

// questionBankAgentQA 保持 AgentQA 的同步完成约定，避免异步题库回答被提前保存为空消息。
func (s *sessionService) questionBankAgentQA(ctx context.Context, req *types.QARequest, bus *event.EventBus) error {
	stream := &questionBankAgentStream{
		done:    make(chan struct{}),
		step:    types.AgentStep{Timestamp: time.Now()},
		pending: make(map[string]event.AgentToolCallData),
	}
	for _, kind := range []event.EventType{
		event.EventAgentFinalAnswer, event.EventAgentThought, event.EventAgentReferences,
		event.EventAgentToolCall, event.EventAgentToolResult, event.EventError,
	} {
		bus.On(kind, stream.onEvent)
	}

	err := s.KnowledgeQA(ctx, req, bus)
	if err == nil {
		select {
		case <-stream.done:
		case <-ctx.Done():
			err = ctx.Err()
		}
	}

	// 停止或报错也要交付已生成的内容；外层先保存消息，再发布完成通知。
	completeErr := bus.Emit(context.WithoutCancel(ctx), event.Event{
		Type:      event.EventAgentComplete,
		SessionID: req.Session.ID,
		Data:      stream.complete(req),
	})
	return errors.Join(err, completeErr)
}

// questionBankAgentStream 汇总题库管线的回答、思考和检索步骤，供 Agent 完成事件统一落库。
type questionBankAgentStream struct {
	mu         sync.Mutex
	done       chan struct{}
	closed     bool
	answer     strings.Builder
	step       types.AgentStep
	references []*types.SearchResult
	pending    map[string]event.AgentToolCallData
}

func (s *questionBankAgentStream) onEvent(_ context.Context, evt event.Event) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil
	}

	switch data := evt.Data.(type) {
	case event.AgentFinalAnswerData:
		s.answer.WriteString(data.Content)
		s.step.Truncated = s.step.Truncated || data.Truncated
		if data.Done {
			s.closed = true
			close(s.done)
		}
	case event.AgentThoughtData:
		s.step.ReasoningContent += data.Content
	case event.AgentReferencesData:
		if refs, ok := data.References.([]*types.SearchResult); ok {
			s.references = append(s.references, refs...)
		}
	case event.AgentToolCallData:
		s.pending[data.ToolCallID] = data
	case event.AgentToolResultData:
		call := s.pending[data.ToolCallID]
		delete(s.pending, data.ToolCallID)
		// 与快速问答一致，只保存已经返回结果的管线步骤。
		s.step.ToolCalls = append(s.step.ToolCalls, types.ToolCall{
			ID:       types.PipelineToolCallIDPrefix + data.ToolCallID,
			Name:     data.ToolName,
			Args:     call.Arguments,
			Duration: data.Duration,
			Result: &types.ToolResult{
				Success: data.Success,
				Output:  data.Output,
				Error:   data.Error,
				Data:    data.Data,
			},
		})
	case event.ErrorData:
		// 错误已由原事件通知客户端，唤醒等待方即可，避免重复发送错误。
		s.closed = true
		close(s.done)
	}
	return nil
}

func (s *questionBankAgentStream) complete(req *types.QARequest) event.AgentCompleteData {
	s.mu.Lock()
	defer s.mu.Unlock()
	// 取消与流回调可能并发；冻结结果后忽略迟到的片段及重复结束事件。
	s.closed = true
	refs := make([]interface{}, 0, len(s.references))
	for _, ref := range s.references {
		refs = append(refs, ref)
	}
	return event.AgentCompleteData{
		SessionID:       req.Session.ID,
		MessageID:       req.AssistantMessageID,
		FinalAnswer:     s.answer.String(),
		KnowledgeRefs:   refs,
		AgentSteps:      []types.AgentStep{s.step},
		TotalSteps:      1,
		TotalDurationMs: time.Since(s.step.Timestamp).Milliseconds(),
	}
}
