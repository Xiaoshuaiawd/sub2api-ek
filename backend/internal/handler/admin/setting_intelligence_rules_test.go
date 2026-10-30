package admin

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type intelligenceRulesHandlerRepo struct {
	service.SettingRepository
	raw string
}

func (r *intelligenceRulesHandlerRepo) GetValue(context.Context, string) (string, error) {
	if r.raw == "" {
		return "", service.ErrSettingNotFound
	}
	return r.raw, nil
}
func (r *intelligenceRulesHandlerRepo) Set(_ context.Context, _, value string) error {
	r.raw = value
	return nil
}

func TestIntelligenceRulesHandler(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &intelligenceRulesHandlerRepo{}
	h := &SettingHandler{settingService: service.NewSettingService(repo, nil)}
	r := gin.New()
	r.GET("/rules", h.GetIntelligenceRules)
	r.PUT("/rules", h.UpdateIntelligenceRules)
	call := func(method, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/rules", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	w := call(http.MethodGet, "")
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"rules":[]`)
	require.Contains(t, w.Body.String(), `"id":"builtin.pelican_bicycle"`)
	w = call(http.MethodPut, `{"rules":[{"name":"Pelican","type":"regex","pattern":"pelican.*bike","enabled":true}]}`)
	require.Equal(t, 200, w.Code)
	w = call(http.MethodPut, `{"rules":[{"name":"Pelican","type":"regex","pattern":"pelican.*bike","enabled":true}],"builtin_disabled_ids":["builtin.pelican_bicycle"]}`)
	require.Equal(t, 200, w.Code)
	w = call(http.MethodPut, `{"rules":[{"name":"Pelican","type":"regex","pattern":"pelican.*bike","enabled":true}]}`)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"builtin_disabled_ids":["builtin.pelican_bicycle"]`)
	w = call(http.MethodPut, `{"rules":[],"builtin_extensions":{"builtin.pelican_bicycle":[{"type":"contains","pattern":"骑滑板"}]}}`)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"pattern":"骑滑板"`)
	w = call(http.MethodPut, `{"rules":[]}`)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), `"pattern":"骑滑板"`, "old clients must preserve extensions")
	require.Contains(t, w.Body.String(), `"builtin_disabled_ids":["builtin.pelican_bicycle"]`)
	before := repo.raw
	w = call(http.MethodPut, `{"rules":[{"name":"bad","type":"regex","pattern":"[","enabled":true}]}`)
	require.Equal(t, 400, w.Code)
	require.Equal(t, before, repo.raw)
}
