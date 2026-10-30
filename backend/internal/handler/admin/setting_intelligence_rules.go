package admin

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// PatchIntelligenceRules modifies one rule while guarding against stale edits.
func (h *SettingHandler) PatchIntelligenceRules(c *gin.Context) {
	var patch service.IntelligenceRulePatch
	decoder := json.NewDecoder(http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&patch); err != nil {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			response.Error(c, http.StatusRequestEntityTooLarge, "Intelligence rule update is too large")
			return
		}
		response.BadRequest(c, "Invalid intelligence rule update")
		return
	}
	var trailing any
	if err := decoder.Decode(&trailing); err != io.EOF {
		var limit *http.MaxBytesError
		if errors.As(err, &limit) {
			response.Error(c, http.StatusRequestEntityTooLarge, "Intelligence rule update is too large")
			return
		}
		response.BadRequest(c, "Invalid intelligence rule update")
		return
	}
	value, err := h.settingService.PatchIntelligenceRules(c.Request.Context(), patch)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrIntelligenceRulesConflict):
			response.Error(c, http.StatusConflict, "Intelligence rules changed; reload and retry")
		case errors.Is(err, service.ErrInvalidIntelligenceRules):
			response.BadRequest(c, err.Error())
		default:
			response.ErrorFrom(c, err)
		}
		return
	}
	response.Success(c, intelligenceRulesResponse(value))
}

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
		Revision           *string                                            `json:"revision"`
		Rules              *[]service.IntelligenceRule                        `json:"rules"`
		BuiltinDisabledIDs *[]string                                          `json:"builtin_disabled_ids"`
		BuiltinExtensions  *map[string][]service.IntelligenceBuiltinExtension `json:"builtin_extensions"`
		BuiltinConditions  *map[string]service.IntelligenceCondition          `json:"builtin_conditions"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || req.Rules == nil {
		response.BadRequest(c, "Invalid intelligence routing rules")
		return
	}
	if req.Revision != nil {
		value, err := h.settingService.ReplaceIntelligenceRulesIfRevision(c.Request.Context(), service.IntelligenceRulesReplacement{
			Revision: *req.Revision, Rules: *req.Rules, BuiltinDisabledIDs: req.BuiltinDisabledIDs,
			BuiltinExtensions: req.BuiltinExtensions, BuiltinConditions: req.BuiltinConditions,
		})
		if err != nil {
			switch {
			case errors.Is(err, service.ErrIntelligenceRulesConflict):
				response.Error(c, http.StatusConflict, "Intelligence rules changed; reload and retry")
			case errors.Is(err, service.ErrInvalidIntelligenceRules):
				response.BadRequest(c, err.Error())
			default:
				response.ErrorFrom(c, err)
			}
			return
		}
		response.Success(c, intelligenceRulesResponse(value))
		return
	}
	var previous service.IntelligenceRules
	if req.BuiltinDisabledIDs == nil || req.BuiltinExtensions == nil || req.BuiltinConditions == nil {
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
	conditions := previous.BuiltinConditions
	if req.BuiltinConditions != nil {
		conditions = *req.BuiltinConditions
	}
	value, err := h.settingService.SetIntelligenceRules(c.Request.Context(), service.IntelligenceRules{Rules: *req.Rules, BuiltinDisabledIDs: disabled, BuiltinExtensions: extensions, BuiltinConditions: conditions})
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
	builtins := make([]gin.H, 0, len(service.IntelligenceBuiltinRules()))
	defaults := service.DefaultIntelligenceBuiltinConditions()
	for _, rule := range service.IntelligenceBuiltinRules() {
		condition := defaults[rule.ID]
		if override, ok := value.BuiltinConditions[rule.ID]; ok {
			condition = override
		}
		builtins = append(builtins, gin.H{"id": rule.ID, "name": rule.Name, "description": rule.Description, "enabled": !disabled[rule.ID], "condition": condition, "default_condition": defaults[rule.ID]})
	}
	extensions := value.BuiltinExtensions
	if extensions == nil {
		extensions = map[string][]service.IntelligenceBuiltinExtension{}
	}
	conditions := value.BuiltinConditions
	if conditions == nil {
		conditions = map[string]service.IntelligenceCondition{}
	}
	return gin.H{"revision": service.IntelligenceRulesRevision(value), "rules": value.Rules, "builtin_disabled_ids": append([]string{}, value.BuiltinDisabledIDs...), "builtin_extensions": extensions, "builtin_conditions": conditions, "builtins": builtins}
}
