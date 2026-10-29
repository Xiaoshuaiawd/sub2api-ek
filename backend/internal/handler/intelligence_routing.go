package handler

import (
	"encoding/json"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// bindIntelligenceRouting keeps the requirement on the request context for all
// scheduling attempts, including retries and sticky-session selection.
func bindIntelligenceRouting(c *gin.Context, body []byte) {
	if service.IsIntelligenceTestRequest(body) {
		c.Request = c.Request.WithContext(service.WithIntelligenceRouting(c.Request.Context()))
	}
}

func bindIntelligenceRoutingPrompt(c *gin.Context, prompt string) {
	body, _ := json.Marshal(map[string]string{"prompt": prompt})
	bindIntelligenceRouting(c, body)
}
