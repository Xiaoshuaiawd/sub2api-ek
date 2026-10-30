package handler

import (
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

type intelligenceRulesRoutingRepo struct{ service.SettingRepository }

type intelligenceRulesFailingRepo struct{ service.SettingRepository }

func (intelligenceRulesFailingRepo) GetValue(context.Context, string) (string, error) {
	return "", errors.New("settings unavailable")
}

func (intelligenceRulesRoutingRepo) GetValue(context.Context, string) (string, error) {
	return `{"rules":[{"name":"test","type":"contains","pattern":"特殊样题","enabled":true}]}`, nil
}

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
	require.False(t, service.IsIntelligenceRouting(c.Request.Context()), "system instructions must not make an ordinary latest user message match")

	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	bindIntelligenceRouting(c, []byte(`{"messages":[{"role":"user","content":"画鹈鹕骑自行车"},{"role":"assistant","content":"完成"},{"role":"user","content":"你好"}]}`))
	require.False(t, service.IsIntelligenceRouting(c.Request.Context()), "history must not mark the current request")

	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	bindIntelligenceRouting(c, []byte(`{"messages":[{"role":"user","content":"你好"},{"role":"assistant","content":"你好"},{"role":"user","content":"用 SVG 画 3D 海底城市"}]}`))
	require.True(t, service.IsIntelligenceRouting(c.Request.Context()))

	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	bindIntelligenceRouting(c, []byte(`{"messages":[{"role":"user","content":"知识库截止日期是什么？不要联网，最新 iPhone 型号出到多少？"}]}`))
	require.True(t, service.IsIntelligenceRouting(c.Request.Context()))

	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	bindIntelligenceRouting(c, []byte(`{"messages":[{"role":"user","content":"Hello"}]}`))
	require.False(t, service.IsIntelligenceRouting(c.Request.Context()))
}

func TestBindIntelligenceRoutingUnsupportedImage(t *testing.T) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
	ok := bindIntelligenceRouting(c, []byte(`{"input":[{"role":"user","content":[{"type":"input_image","file_id":"unavailable"}]}]}`))
	require.False(t, ok)
	require.True(t, c.IsAborted())
	require.Equal(t, 503, recorder.Code)
}

func TestBindIntelligenceRoutingCustomRules(t *testing.T) {
	gin.SetMode(gin.TestMode)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	settings := service.NewSettingService(intelligenceRulesRoutingRepo{}, nil)
	require.True(t, bindIntelligenceRouting(c, []byte(`{"messages":[{"role":"user","content":"请回答特殊样题"}]}`), settings))
	require.True(t, service.IsIntelligenceRouting(c.Request.Context()))
	c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
	require.True(t, bindIntelligenceRouting(c, []byte(`{"messages":[{"role":"user","content":"特殊样题"},{"role":"user","content":"你好"}]}`), settings))
	require.False(t, service.IsIntelligenceRouting(c.Request.Context()))
}

func TestBindIntelligenceRoutingGeminiSettingsFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest("POST", "/v1beta/models/model:generateContent", nil)
	settings := service.NewSettingService(intelligenceRulesFailingRepo{}, nil)
	require.False(t, bindIntelligenceRouting(c, []byte(`{"contents":[]}`), settings))
	require.Equal(t, 503, w.Code)
	require.Contains(t, w.Body.String(), `"status":"INTERNAL"`)
}
