package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	middleware2 "github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// bindIntelligenceRouting keeps the requirement on the request context for all
// scheduling attempts, including retries and sticky-session selection.
func bindIntelligenceRouting(c *gin.Context, body []byte, settings ...*service.SettingService) bool {
	return bindIntelligenceRoutingWithBody(c, body, body, settings...)
}

func bindIntelligenceRoutingWithBody(c *gin.Context, body, original []byte, settings ...*service.SettingService) bool {
	return bindIntelligenceRoutingInternal(c, body, original, nil, settings...)
}

func bindIntelligenceRoutingWithExtraPrompt(c *gin.Context, body []byte, prompt string, settings ...*service.SettingService) bool {
	return bindIntelligenceRoutingInternal(c, body, body, &prompt, settings...)
}

func bindIntelligenceRoutingInternal(c *gin.Context, body, original []byte, extraPrompt *string, settings ...*service.SettingService) bool {
	defer pkghttputil.ReleaseOriginalRequestBody(c.Request.Context())
	var rules service.IntelligenceRules
	if len(settings) > 0 && settings[0] != nil {
		var err error
		rules, err = settings[0].RuntimeIntelligenceRules(c.Request.Context())
		if err != nil {
			intelligenceRoutingError(c, "Intelligence routing rules unavailable; please retry")
			return false
		}
	}
	matches, _ := service.ClassifyIntelligenceRequest(c.Request.Context(), body, rules)
	if extraPrompt != nil {
		extraBody, _ := json.Marshal(map[string]string{"prompt": *extraPrompt})
		extraMatches, _ := service.ClassifyIntelligenceRequest(c.Request.Context(), extraBody, rules)
		matches = service.MergeIntelligenceRuleMatches(matches, extraMatches)
	}
	if len(matches) > 0 {
		var setting *service.SettingService
		if len(settings) > 0 {
			setting = settings[0]
		}
		key, _ := middleware2.GetAPIKeyFromContext(c)
		ctx, err := service.CaptureIntelligenceRequest(c.Request.Context(), setting, key, original, c.GetHeader("Content-Type"), matches)
		if err != nil {
			intelligenceRoutingError(c, "Intelligence request capture unavailable; please retry")
			return false
		}
		c.Request = c.Request.WithContext(ctx)
	}
	return true
}

func intelligenceRoutingError(c *gin.Context, message string) {
	if strings.Contains(c.Request.URL.Path, "/v1beta/") {
		googleError(c, http.StatusServiceUnavailable, message)
		c.Abort()
	} else {
		c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"type": "error", "error": gin.H{"type": "api_error", "message": message}})
	}
}

func bindIntelligenceRoutingPrompt(c *gin.Context, prompt string, original []byte, settings ...*service.SettingService) bool {
	body, _ := json.Marshal(map[string]string{"prompt": prompt})
	return bindIntelligenceRoutingWithBody(c, body, original, settings...)
}
