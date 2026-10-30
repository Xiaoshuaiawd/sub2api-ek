package handler

import (
	"bytes"
	"compress/gzip"
	"context"
	"errors"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"

	pkghttputil "github.com/Wei-Shaw/sub2api/internal/pkg/httputil"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

type intelligenceRulesRoutingRepo struct{ service.SettingRepository }

type intelligenceRulesFailingRepo struct{ service.SettingRepository }

type intelligenceCaptureRepo struct {
	service.IntelligenceContextRepository
	stored []*service.IntelligenceRequestContext
	fail   bool
}

func (r *intelligenceCaptureRepo) Create(_ context.Context, item *service.IntelligenceRequestContext) error {
	if r.fail {
		return errors.New("database unavailable")
	}
	r.stored = append(r.stored, item)
	return nil
}

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

func TestBindIntelligenceRoutingCapturesOnlyMatches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &intelligenceCaptureRepo{}
	settings := service.NewSettingService(intelligenceRulesRoutingRepo{}, nil)
	settings.SetIntelligenceContextRepository(store)
	makeContext := func() (*gin.Context, *httptest.ResponseRecorder) {
		w := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(w)
		c.Request = httptest.NewRequest("POST", "/v1/chat/completions", nil)
		c.Request.Header.Set("Content-Type", "application/json")
		c.Set("api_key", &service.APIKey{ID: 7, UserID: 9})
		return c, w
	}
	ordinary, _ := makeContext()
	require.True(t, bindIntelligenceRouting(ordinary, []byte(`{"input":"你好"}`), settings))
	require.Empty(t, store.stored)
	matched, _ := makeContext()
	body := []byte(`{"messages":[{"role":"user","content":"特殊样题"}]}`)
	require.True(t, bindIntelligenceRouting(matched, body, settings))
	require.Len(t, store.stored, 1)
	require.Equal(t, body, store.stored[0].Body)
	require.Equal(t, "test", store.stored[0].MatchedRules[0].Name)
	require.Equal(t, store.stored[0].ID, service.IntelligenceContextID(matched.Request.Context()))
	store.fail = true
	failed, recorder := makeContext()
	require.False(t, bindIntelligenceRouting(failed, body, settings))
	require.Equal(t, 503, recorder.Code)
}

func TestBindIntelligenceRoutingNewBenchmarksCapture(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &intelligenceCaptureRepo{}
	settings := service.NewSettingService(intelligenceRulesRoutingRepo{}, nil)
	settings.SetIntelligenceContextRepository(store)
	for _, tc := range []struct{ body, id string }{
		{`{"input":"don't search the internet, do you know Thibault Sottiaux on X. answer yes or no"}`, "builtin.thibault_sottiaux"},
		{`{"input":"不使用外部工具或者代码，只有许多100欧姆的电阻，如何串联和并联获取67欧姆？最少需要多少个？"}`, "builtin.resistor_100_to_67"},
	} {
		c, _ := gin.CreateTestContext(httptest.NewRecorder())
		c.Request = httptest.NewRequest("POST", "/v1/responses", nil)
		c.Set("api_key", &service.APIKey{ID: 7, UserID: 9})
		require.True(t, bindIntelligenceRouting(c, []byte(tc.body), settings))
		require.True(t, service.IsIntelligenceRouting(c.Request.Context()))
		require.Equal(t, tc.body, string(store.stored[len(store.stored)-1].Body))
		require.Equal(t, tc.id, store.stored[len(store.stored)-1].MatchedRules[0].ID)
	}
}

func TestBindIntelligenceRoutingPreservesCompressedOriginal(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &intelligenceCaptureRepo{}
	settings := service.NewSettingService(intelligenceRulesRoutingRepo{}, nil)
	settings.SetIntelligenceContextRepository(store)
	var compressed bytes.Buffer
	w := gzip.NewWriter(&compressed)
	_, err := w.Write([]byte(`{"input":"特殊样题"}`))
	require.NoError(t, err)
	require.NoError(t, w.Close())
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/responses", bytes.NewReader(compressed.Bytes()))
	c.Request.Header.Set("Content-Type", "application/json")
	c.Request.Header.Set("Content-Encoding", "gzip")
	c.Set("api_key", &service.APIKey{ID: 7, UserID: 9})
	decoded, err := pkghttputil.ReadLenientJSONRequestBodyWithPrealloc(c.Request, 1024)
	require.NoError(t, err)
	require.True(t, bindIntelligenceRouting(c, decoded, settings))
	require.Len(t, store.stored, 1)
	require.Equal(t, compressed.Bytes(), store.stored[0].Body)
	require.Equal(t, "gzip", store.stored[0].ContentEncoding)
	_, _, retained := pkghttputil.OriginalRequestBody(c.Request.Context())
	require.False(t, retained)
}

func TestBindIntelligenceRoutingCombinesPromptMatches(t *testing.T) {
	gin.SetMode(gin.TestMode)
	store := &intelligenceCaptureRepo{}
	settings := service.NewSettingService(intelligenceRulesRoutingRepo{}, nil)
	settings.SetIntelligenceContextRepository(store)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest("POST", "/v1/alpha/search", nil)
	c.Set("api_key", &service.APIKey{ID: 7, UserID: 9})
	body := []byte(`{"input":"特殊样题","query":"糖果测试"}`)
	require.True(t, bindIntelligenceRoutingWithExtraPrompt(c, body, "糖果测试", settings))
	require.Len(t, store.stored, 1)
	require.Equal(t, body, store.stored[0].Body)
	var names []string
	for _, match := range store.stored[0].MatchedRules {
		names = append(names, match.Name)
	}
	require.Contains(t, names, "test")
	require.Contains(t, names, "糖果测试名称")
}
