package admin

import (
	"context"
	"encoding/json"
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

func (r *intelligenceRulesHandlerRepo) CompareAndSetIntelligenceRules(_ context.Context, _, previous, next string, exists bool) (bool, error) {
	if (r.raw != "") != exists || (exists && r.raw != previous) {
		return false, nil
	}
	r.raw = next
	return true, nil
}

func TestIntelligenceRulesPatchAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &intelligenceRulesHandlerRepo{}
	h := &SettingHandler{settingService: service.NewSettingService(repo, nil)}
	r := gin.New()
	r.GET("/rules", h.GetIntelligenceRules)
	r.PATCH("/rules", h.PatchIntelligenceRules)
	r.PUT("/rules", h.UpdateIntelligenceRules)
	call := func(method, body string) *httptest.ResponseRecorder {
		w := httptest.NewRecorder()
		req := httptest.NewRequest(method, "/rules", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		r.ServeHTTP(w, req)
		return w
	}
	w := call(http.MethodGet, "")
	var envelope struct {
		Data struct {
			Revision string `json:"revision"`
		} `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &envelope))
	require.Len(t, envelope.Data.Revision, 64)
	patch, err := json.Marshal(service.IntelligenceRulePatch{Revision: envelope.Data.Revision, Operation: "add_rule", Rule: &service.IntelligenceRule{Name: "新题", Type: "contains", Pattern: "测试关键词", Enabled: true}})
	require.NoError(t, err)
	w = call(http.MethodPatch, string(patch))
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "测试关键词")
	w = call(http.MethodPatch, string(patch))
	require.Equal(t, 409, w.Code)
	previous := repo.raw
	w = call(http.MethodPut, `{"revision":"`+envelope.Data.Revision+`","rules":[]}`)
	require.Equal(t, 409, w.Code, "stale admin form must not overwrite API changes")
	require.Equal(t, previous, repo.raw)
	w = call(http.MethodPatch, `{"operation":"add_rule","revision":"ignored","bad_field":true}`)
	require.Equal(t, 400, w.Code)
	require.Equal(t, previous, repo.raw)
	w = call(http.MethodPatch, `{"revision":"`+envelope.Data.Revision+`","operation":"add_rule","rule":{"name":"test","type":"contains","pattern":"`+strings.Repeat("x", 4<<20)+`","enabled":true}}`)
	require.Equal(t, http.StatusRequestEntityTooLarge, w.Code)
	require.Equal(t, previous, repo.raw)
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
	require.Contains(t, w.Body.String(), `"id":"builtin.thibault_sottiaux"`)
	require.Contains(t, w.Body.String(), `"id":"builtin.resistor_100_to_67"`)
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

func TestIntelligenceRulesHandlerBuiltinConditions(t *testing.T) {
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
	require.Contains(t, w.Body.String(), `"default_condition"`)
	require.Contains(t, w.Body.String(), `"condition"`)
	w = call(http.MethodPut, `{"rules":[],"builtin_conditions":{"builtin.candy_named":{"op":"contains_any","text":"plain","values":["超级糖"]}}}`)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "超级糖")
	w = call(http.MethodPut, `{"rules":[]}`)
	require.Equal(t, 200, w.Code)
	require.Contains(t, w.Body.String(), "超级糖", "old client saves must preserve edited default rules")
	w = call(http.MethodPut, `{"rules":[],"builtin_conditions":{}}`)
	require.Equal(t, 200, w.Code)
	require.NotContains(t, w.Body.String(), "超级糖")
}
