package service

import (
	"context"

	"github.com/Tencent/WeKnora/internal/infrastructure/docparser"
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

// resolveQuestionImageModel 沿用当前检索范围中题库的识图配置，智能体的显式配置优先。
func (s *sessionService) resolveQuestionImageModel(ctx context.Context, cm *types.ChatManage) {
	// 兼容内联文件上传，附件地址由服务端保存生成，不接受客户端外部 URL。
	seen := make(map[string]bool, len(cm.Images))
	for _, image := range cm.Images {
		seen[image] = true
	}
	for _, attachment := range cm.Attachments {
		if docparser.IsImageFormat(attachment.FileType) && attachment.URL != "" && !seen[attachment.URL] {
			cm.Images = append(cm.Images, attachment.URL)
			seen[attachment.URL] = true
		}
	}
	if len(cm.Images) == 0 || cm.VLMModelID != "" || cm.ChatModelSupportsVision {
		return
	}
	for _, target := range cm.SearchTargets {
		kb, err := s.knowledgeBaseService.GetKnowledgeBaseByID(types.WithExecutionTenant(ctx, target.TenantID), target.KnowledgeBaseID)
		if err == nil && kb != nil && kb.Type == types.KnowledgeBaseTypeQuestionBank && kb.VLMConfig.IsEnabled() && kb.VLMConfig.ModelID != "" {
			cm.VLMModelID = kb.VLMConfig.ModelID
			cm.VLMModelTenantID = target.TenantID
			return
		}
	}
}
