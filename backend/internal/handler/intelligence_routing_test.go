package handler

import (
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

func TestBindIntelligenceRouting(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	bindIntelligenceRouting(c, []byte(`{"messages":[{"role":"user","content":"Draw a pelican riding a bicycle"}]}`))
	require.True(t, service.IsIntelligenceRouting(c.Request.Context()))

	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	bindIntelligenceRouting(c, []byte(`{"messages":[{"role":"user","content":"请用 SVG 制作秦始皇开坦克的 2D 动画"}]}`))
	require.True(t, service.IsIntelligenceRouting(c.Request.Context()))

	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	bindIntelligenceRouting(c, []byte(`{"instructions":"用 SVG 输出 HTML","input":"画一个 3D 海底城市"}`))
	require.True(t, service.IsIntelligenceRouting(c.Request.Context()))

	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	bindIntelligenceRouting(c, []byte(`{"messages":[{"role":"user","content":"Hello"}]}`))
	require.False(t, service.IsIntelligenceRouting(c.Request.Context()))
}
