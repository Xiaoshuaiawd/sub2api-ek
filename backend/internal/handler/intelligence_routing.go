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
func bindIntelligenceRouting(c *gin.Context, body []byte) bool {
	required, err := service.IntelligenceRouteWithOCR(c.Request.Context(), body)
	if err != nil {
		message := "Image text recognition failed; please retry"
		if strings.Contains(c.Request.URL.Path, "/v1beta/") {
			googleError(c, http.StatusServiceUnavailable, message)
			c.Abort()
		} else {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"type": "error", "error": gin.H{"type": "api_error", "message": message}})
		}
		return false
	}
	if required {
		c.Request = c.Request.WithContext(service.WithIntelligenceRouting(c.Request.Context()))
	}
	return true
}

func bindIntelligenceRoutingPrompt(c *gin.Context, prompt string) {
	body, _ := json.Marshal(map[string]string{"prompt": prompt})
	bindIntelligenceRouting(c, body)
}
