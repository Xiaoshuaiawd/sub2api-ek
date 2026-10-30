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

type intelligenceRulesRepo struct {
	SettingRepository
	mu    sync.Mutex
	value string
	fail  bool
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
