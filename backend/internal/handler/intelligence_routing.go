package handler

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// bindIntelligenceRouting keeps the requirement on the request context for all
// scheduling attempts, including retries and sticky-session selection.
func bindIntelligenceRouting(c *gin.Context, body []byte, settings ...*service.SettingService) bool {
	var rules service.IntelligenceRules
	if len(settings) > 0 && settings[0] != nil {
		var err error
		rules, err = settings[0].RuntimeIntelligenceRules(c.Request.Context())
		if err != nil {
			intelligenceRoutingError(c, "Intelligence routing rules unavailable; please retry")
			return false
		}
	}
	required, err := service.IntelligenceRouteWithRules(c.Request.Context(), body, rules)
	if err != nil {
		intelligenceRoutingError(c, "Image text recognition failed; please retry")
		return false
	}
	if required {
		c.Request = c.Request.WithContext(service.WithIntelligenceRouting(c.Request.Context()))
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

func bindIntelligenceRoutingPrompt(c *gin.Context, prompt string, settings ...*service.SettingService) bool {
	body, _ := json.Marshal(map[string]string{"prompt": prompt})
	return bindIntelligenceRouting(c, body, settings...)
}
