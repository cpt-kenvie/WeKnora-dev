package handler

import (
	"net/http"

	"github.com/Tencent/WeKnora/internal/application/service"
	apperrors "github.com/Tencent/WeKnora/internal/errors"
	"github.com/Tencent/WeKnora/internal/types"
	"github.com/gin-gonic/gin"
)

// QuestionBankHandler 的读写权限由知识库路由守卫统一解析。
type QuestionBankHandler struct{ service *service.QuestionBankService }

func NewQuestionBankHandler(service *service.QuestionBankService) *QuestionBankHandler {
	return &QuestionBankHandler{service: service}
}

func (h *QuestionBankHandler) List(c *gin.Context) {
	var filter types.QuestionListFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.Error(apperrors.NewBadRequestError("查询参数不合法"))
		return
	}
	if filter.QuestionType != "" && !types.IsQuestionType(filter.QuestionType) {
		c.Error(apperrors.NewBadRequestError("题型不合法"))
		return
	}
	if filter.ReviewStatus != "" && filter.ReviewStatus != types.QuestionReady && filter.ReviewStatus != types.QuestionNeedsReview {
		c.Error(apperrors.NewBadRequestError("核对状态不合法"))
		return
	}
	data, err := h.service.List(c.Request.Context(), c.Param("id"), filter)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func (h *QuestionBankHandler) Update(c *gin.Context) {
	var body types.QuestionUpdate
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Error(apperrors.NewBadRequestError("题目格式不合法").WithDetails(err.Error()))
		return
	}
	data, err := h.service.Update(c.Request.Context(), c.Param("id"), c.Param("question_id"), body)
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func (h *QuestionBankHandler) RetryIndex(c *gin.Context) {
	data, err := h.service.RetryIndex(c.Request.Context(), c.Param("id"), c.Param("question_id"))
	if err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true, "data": data})
}

func (h *QuestionBankHandler) Delete(c *gin.Context) {
	var body struct {
		Revision int `json:"revision" binding:"required,min=1"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.Error(apperrors.NewBadRequestError("需要提供当前题目版本"))
		return
	}
	if err := h.service.Delete(c.Request.Context(), c.Param("id"), c.Param("question_id"), body.Revision); err != nil {
		c.Error(err)
		return
	}
	c.JSON(http.StatusOK, gin.H{"success": true})
}
