package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/types"
)

// questionBankScope 只检查已经由请求入口授权的检索范围，兼容共享库和指定原图。
func (s *sessionService) questionBankScope(ctx context.Context, targets types.SearchTargets) bool {
	if len(targets) == 0 {
		return false
	}
	for _, target := range targets {
		if target == nil {
			return false
		}
		kb, err := s.knowledgeBaseService.GetKnowledgeBaseByID(types.WithExecutionTenant(ctx, target.TenantID), target.KnowledgeBaseID)
		if err != nil || kb == nil || kb.Type != types.KnowledgeBaseTypeQuestionBank {
			return false
		}
	}
	return true
}
