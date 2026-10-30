package service

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceRulesImagesBeforeValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &SettingService{settingRepo: &intelligenceRulesRepo{}}
	_, err := settings.SetIntelligenceRules(t.Context(), IntelligenceRules{Rules: []IntelligenceRule{{Name: "draw", Type: "contains", Pattern: "自定义测试题", Enabled: true}}})
	require.NoError(t, err)
	body := []byte(`{"model":"gpt-image-2","prompt":"绘制自定义测试题"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	_, err = (&OpenAIGatewayService{settingService: settings}).ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.True(t, IsIntelligenceRouting(c.Request.Context()))
}

func TestIntelligenceRulesImagesFreezeAtFirstParse(t *testing.T) {
	gin.SetMode(gin.TestMode)
	settings := &SettingService{settingRepo: &intelligenceRulesRepo{}}
	body := []byte(`{"model":"gpt-image-2","prompt":"特殊样题"}`)
	req := httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = req
	svc := &OpenAIGatewayService{settingService: settings}
	_, err := svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.False(t, IsIntelligenceRouting(c.Request.Context()))
	_, err = settings.SetIntelligenceRules(t.Context(), IntelligenceRules{Rules: []IntelligenceRule{{Name: "new", Type: "contains", Pattern: "特殊样题", Enabled: true}}})
	require.NoError(t, err)
	_, err = svc.ParseOpenAIImagesRequest(c, body)
	require.NoError(t, err)
	require.False(t, IsIntelligenceRouting(c.Request.Context()))
	fresh, _ := gin.CreateTestContext(httptest.NewRecorder())
	fresh.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", bytes.NewReader(body))
	fresh.Request.Header.Set("Content-Type", "application/json")
	_, err = svc.ParseOpenAIImagesRequest(fresh, body)
	require.NoError(t, err)
	require.True(t, IsIntelligenceRouting(fresh.Request.Context()))
}

type intelligenceRulesRepo struct {
	SettingRepository
	mu    sync.Mutex
	value string
	fail  bool
}

type intelligenceLinkRepo struct {
	IntelligenceContextRepository
	linked []string
}

func (r *intelligenceLinkRepo) Link(_ context.Context, _ string, _ int64, requestID string) error {
	r.linked = append(r.linked, requestID)
	return nil
}

func TestIntelligenceContextLinksOnlyUniqueRequestIDs(t *testing.T) {
	repo := &intelligenceLinkRepo{}
	settings := &SettingService{intelligenceContexts: repo}
	ctx := WithIntelligenceContextID(context.Background(), "capture")
	linkIntelligenceUsage(ctx, settings, 7, "grok-video:shared")
	linkIntelligenceUsage(ctx, settings, 7, "local:client-controlled")
	linkIntelligenceUsage(ctx, settings, 7, "client:not-a-uuid")
	require.Empty(t, repo.linked)
	id := "client:00000000-0000-4000-8000-000000000001"
	linkIntelligenceUsage(ctx, settings, 7, id)
	require.Equal(t, []string{id}, repo.linked)
}

func (r *intelligenceRulesRepo) GetValue(context.Context, string) (string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return "", errors.New("unavailable")
	}
	if r.value == "" {
		return "", ErrSettingNotFound
	}
	return r.value, nil
}
func (r *intelligenceRulesRepo) Set(_ context.Context, _, value string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.fail {
		return errors.New("unavailable")
	}
	r.value = value
	return nil
}

func TestIntelligenceRulesPersistMatchAndCache(t *testing.T) {
	repo := &intelligenceRulesRepo{}
	s := &SettingService{settingRepo: repo}
	initial, err := s.RuntimeIntelligenceRules(t.Context())
	require.NoError(t, err)
	require.Empty(t, initial.Rules)
	want := IntelligenceRules{Rules: []IntelligenceRule{{Name: "糖果", Type: "contains", Pattern: "Candy Test", Enabled: true}, {Name: "变体", Type: "regex", Pattern: `pelican.*bicycl(e|ing)`, Enabled: true}, {Name: "关闭", Type: "contains", Pattern: "忽略我", Enabled: false}}}
	_, err = s.SetIntelligenceRules(t.Context(), want)
	require.NoError(t, err)
	rules, err := s.RuntimeIntelligenceRules(t.Context())
	require.NoError(t, err)
	require.True(t, rules.Match("hello CANDY TEST please"))
	require.True(t, rules.Match("Pelican riding BICYCLE"))
	require.False(t, rules.Match("忽略我"))
	s2 := &SettingService{settingRepo: repo}
	loaded, err := s2.GetIntelligenceRules(t.Context())
	require.NoError(t, err)
	require.Equal(t, 3, len(loaded.Rules))
	repo.mu.Lock()
	repo.fail = true
	repo.mu.Unlock()
	s.intelligenceRulesExpires = time.Time{}
	stale, err := s.RuntimeIntelligenceRules(t.Context())
	require.NoError(t, err)
	require.True(t, stale.Match("CANDY TEST"))
	_, err = (&SettingService{settingRepo: repo}).RuntimeIntelligenceRules(t.Context())
	require.Error(t, err)
}

func TestIntelligenceRulesRejectInvalid(t *testing.T) {
	s := &SettingService{settingRepo: &intelligenceRulesRepo{}}
	for _, rule := range []IntelligenceRule{{Name: "x", Type: "regex", Pattern: "[", Enabled: true}, {Name: "x", Type: "regex", Pattern: ".*", Enabled: true}, {Name: "x", Type: "contains", Pattern: " ", Enabled: true}, {Name: "x", Type: "wat", Pattern: "abc", Enabled: true}} {
		_, err := s.SetIntelligenceRules(t.Context(), IntelligenceRules{Rules: []IntelligenceRule{rule}})
		require.Error(t, err)
	}
}

func TestIntelligenceCustomRuleMatchedKeywords(t *testing.T) {
	rules, err := prepareIntelligenceRules(IntelligenceRules{Rules: []IntelligenceRule{
		{Name: "关键词", Type: "contains", Pattern: "青苹果", Enabled: true},
		{Name: "正则", Type: "regex", Pattern: `糖果.{0,8}数量`, Enabled: true},
	}})
	require.NoError(t, err)
	matches := rules.MatchAll("袋子里有青苹果味糖果，请计算数量")
	byName := map[string]IntelligenceRuleMatch{}
	for _, match := range matches {
		byName[match.Name] = match
	}
	require.Contains(t, byName["关键词"].MatchedKeywords, "青苹果")
	require.Contains(t, byName["正则"].MatchedKeywords, "糖果,请计算数量")
	require.NotContains(t, byName["正则"].MatchedKeywords, `糖果.{0,8}数量`)
}

func TestIntelligenceBuiltinExtensionMatchedKeywords(t *testing.T) {
	rules, err := prepareIntelligenceRules(IntelligenceRules{BuiltinExtensions: map[string][]IntelligenceBuiltinExtension{
		"builtin.pelican_bicycle": {{Type: "regex", Pattern: `鸟.{0,5}单车`}},
	}})
	require.NoError(t, err)
	matches := rules.MatchAll("那只鸟骑着单车")
	require.Len(t, matches, 1)
	require.Equal(t, "builtin.pelican_bicycle", matches[0].ID)
	require.Contains(t, matches[0].MatchedKeywords, "鸟骑着单车")
}

func TestIntelligenceBuiltinDisabledPersistAndOldSettings(t *testing.T) {
	repo := &intelligenceRulesRepo{value: `{"rules":[{"name":"custom","type":"contains","pattern":"自定义","enabled":true}]}`}
	s := &SettingService{settingRepo: repo}
	old, err := s.GetIntelligenceRules(t.Context())
	require.NoError(t, err)
	require.Len(t, old.MatchBuiltins("鹈鹕骑自行车"), 1)
	require.True(t, old.Match("自定义"))
	old.BuiltinDisabledIDs = []string{"builtin.pelican_bicycle"}
	_, err = s.SetIntelligenceRules(t.Context(), old)
	require.NoError(t, err)
	loaded, err := (&SettingService{settingRepo: repo}).GetIntelligenceRules(t.Context())
	require.NoError(t, err)
	require.Empty(t, loaded.MatchBuiltins("鹈鹕骑自行车"))
	require.True(t, loaded.Match("自定义"))
	require.Empty(t, loaded.MatchAll("鹈鹕骑自行车"))
	require.True(t, IsIntelligenceTestRequest([]byte(`{"prompt":"鹈鹕骑自行车"}`)), "legacy detection retains old behavior")
}

func TestIntelligenceBuiltinExtensionsPreserveNativeAndTrackMatches(t *testing.T) {
	s := &SettingService{settingRepo: &intelligenceRulesRepo{}}
	input := IntelligenceRules{BuiltinExtensions: map[string][]IntelligenceBuiltinExtension{
		"builtin.pelican_bicycle": {{Type: "contains", Pattern: "骑滑板的鹈鹕"}, {Type: "regex", Pattern: `pelican.*skateboard`}},
	}}
	_, err := s.SetIntelligenceRules(t.Context(), input)
	require.NoError(t, err)
	loaded, err := (&SettingService{settingRepo: s.settingRepo}).GetIntelligenceRules(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, loaded.MatchAll("鹈鹕骑自行车"))
	hits := loaded.MatchAll("请画骑滑板的鹈鹕")
	require.Len(t, hits, 1)
	require.Equal(t, "builtin.pelican_bicycle", hits[0].ID)
	require.Equal(t, "骑滑板的鹈鹕", hits[0].Pattern)
	require.NotEmpty(t, loaded.MatchAll("A pelican on a skateboard"))
	loaded.BuiltinDisabledIDs = []string{"builtin.pelican_bicycle"}
	require.Empty(t, loaded.MatchAll("请画骑滑板的鹈鹕"))
	require.Empty(t, loaded.MatchAll("鹈鹕骑自行车"))
	input.BuiltinExtensions["builtin.pelican_bicycle"][0] = IntelligenceBuiltinExtension{Type: "regex", Pattern: "["}
	_, err = s.SetIntelligenceRules(t.Context(), input)
	require.Error(t, err)
}
