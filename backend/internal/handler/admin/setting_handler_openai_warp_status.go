package admin

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

func (h *SettingHandler) GetOpenAIWarpStatus(c *gin.Context) {
	response.Success(c, service.GetOpenAIWarpStatus(c.Request.Context()))
}
