package router

import (
	"net/http"
	"testing"

	"github.com/Tencent/WeKnora/internal/handler"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

func TestQuestionPaperRoutesDeclareRetrieveCapability(t *testing.T) {
	gin.SetMode(gin.TestMode)
	guards := &rbacGuards{}
	RegisterQuestionBankRoutes(gin.New().Group("/api/v1"), &handler.QuestionBankHandler{}, guards)
	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/knowledge-bases/:id/questions/paper-options"},
		{http.MethodPost, "/api/v1/knowledge-bases/:id/questions/paper"},
	} {
		policy := mustLookupAPIKeyPolicy(t, guards, route.method, route.path)
		if !policy.RequireFullAccess || !policyHasCapability(policy, types.APIKeyCapabilityRetrieve) {
			t.Fatalf("出卷路由必须声明检索权限：%s %s", route.method, route.path)
		}
	}
}
