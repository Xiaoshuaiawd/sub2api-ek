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
	response.Success(c, intelligenceRulesResponse(value))
}

func (h *SettingHandler) UpdateIntelligenceRules(c *gin.Context) {
	var req struct {
		Rules              *[]service.IntelligenceRule                        `json:"rules"`
		BuiltinDisabledIDs *[]string                                          `json:"builtin_disabled_ids"`
		BuiltinExtensions  *map[string][]service.IntelligenceBuiltinExtension `json:"builtin_extensions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Rules == nil {
		response.BadRequest(c, "Invalid intelligence routing rules")
		return
	}
	var previous service.IntelligenceRules
	if req.BuiltinDisabledIDs == nil || req.BuiltinExtensions == nil {
		var err error
		previous, err = h.settingService.GetIntelligenceRules(c.Request.Context())
		if err != nil {
			response.ErrorFrom(c, err)
			return
		}
	}
	var disabled []string
	if req.BuiltinDisabledIDs != nil {
		disabled = *req.BuiltinDisabledIDs
	} else {
		disabled = previous.BuiltinDisabledIDs
	}
	extensions := previous.BuiltinExtensions
	if req.BuiltinExtensions != nil {
		extensions = *req.BuiltinExtensions
	}
	value, err := h.settingService.SetIntelligenceRules(c.Request.Context(), service.IntelligenceRules{Rules: *req.Rules, BuiltinDisabledIDs: disabled, BuiltinExtensions: extensions})
	if err != nil {
		if errors.Is(err, service.ErrInvalidIntelligenceRules) {
			response.BadRequest(c, err.Error())
		} else {
			response.ErrorFrom(c, err)
		}
		return
	}
	response.Success(c, intelligenceRulesResponse(value))
}

func intelligenceRulesResponse(value service.IntelligenceRules) gin.H {
	disabled := make(map[string]bool, len(value.BuiltinDisabledIDs))
	for _, id := range value.BuiltinDisabledIDs {
		disabled[id] = true
	}
	builtins := make([]gin.H, 0, 8)
	for _, rule := range service.IntelligenceBuiltinRules() {
		builtins = append(builtins, gin.H{"id": rule.ID, "name": rule.Name, "description": rule.Description, "enabled": !disabled[rule.ID]})
	}
	extensions := value.BuiltinExtensions
	if extensions == nil {
		extensions = map[string][]service.IntelligenceBuiltinExtension{}
	}
	return gin.H{"rules": value.Rules, "builtin_disabled_ids": append([]string{}, value.BuiltinDisabledIDs...), "builtin_extensions": extensions, "builtins": builtins}
}
