package service

import (
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

const gatewayClientModelKey = "gateway_client_response_model"

// SetGatewayClientModel records the model requested by the client separately
// from the channel-mapped model used for upstream routing and billing.
func SetGatewayClientModel(c *gin.Context, model string) {
	if c != nil {
		c.Set(gatewayClientModelKey, strings.TrimSpace(model))
	}
}

func gatewayClientResponseModel(c *gin.Context, fallback string) string {
	if c != nil {
		if model := strings.TrimSpace(c.GetString(gatewayClientModelKey)); model != "" {
			return model
		}
	}
	return fallback
}

// rewriteGatewayClientModel updates an existing protocol model field only.
func rewriteGatewayClientModel(c *gin.Context, body []byte, path string) []byte {
	model := gatewayClientResponseModel(c, "")
	if model == "" || gjson.GetBytes(body, path).Type != gjson.String {
		return body
	}
	if rewritten, err := sjson.SetBytes(body, path, model); err == nil {
		return rewritten
	}
	return body
}
