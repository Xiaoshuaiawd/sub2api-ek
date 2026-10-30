package admin

import (
	"errors"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetIntelligenceRules(c *gin.Context) {
	value, err := h.settingService.GetIntelligenceRules(c.Request.Context())
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	response.Success(c, value)
}

func (h *SettingHandler) UpdateIntelligenceRules(c *gin.Context) {
	var req struct {
		Rules *[]service.IntelligenceRule `json:"rules"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Rules == nil {
		response.BadRequest(c, "Invalid intelligence routing rules")
		return
	}
	value, err := h.settingService.SetIntelligenceRules(c.Request.Context(), service.IntelligenceRules{Rules: *req.Rules})
	if err != nil {
		if errors.Is(err, service.ErrInvalidIntelligenceRules) {
			response.BadRequest(c, err.Error())
		} else {
			response.ErrorFrom(c, err)
		}
		return
	}
	response.Success(c, value)
}
