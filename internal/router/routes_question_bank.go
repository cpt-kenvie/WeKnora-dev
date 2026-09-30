package router

import (
	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/gin-gonic/gin"
)

// RegisterQuestionBankRoutes 与原文件上传复用相同的知识库授权边界。
func RegisterQuestionBankRoutes(r *gin.RouterGroup, handler *handler.QuestionBankHandler, g *rbacGuards) {
	if handler == nil {
		return
	}
	questions := g.apiKeyGroup(r.Group("/knowledge-bases/:id/questions"), apiKeyIngest(apiKeyFullAccess()))
	questions.With(apiKeyRetrieve(apiKeyFullAccess())).GET("", g.Viewer(), g.KBAccessRead("id"), handler.List)
	questions.PUT("/:question_id", g.OwnedKBOrAdmin(), g.KBAccessWrite("id"), handler.Update)
	questions.POST("/:question_id/reindex", g.OwnedKBOrAdmin(), g.KBAccessWrite("id"), handler.RetryIndex)
	questions.DELETE("/:question_id", g.OwnedKBOrAdmin(), g.KBAccessWrite("id"), handler.Delete)
}
