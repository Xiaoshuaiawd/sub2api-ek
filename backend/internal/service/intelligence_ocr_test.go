package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"image"
	"image/png"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntelligenceOCRRouting(t *testing.T) {
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AQ=="}}]}]}`)
	for _, tc := range []struct {
		name, text    string
		err           error
		want, wantErr bool
	}{
		{"candy screenshot", "这 是 糖 果 测 试", nil, true, false},
		{"pelican screenshot", "画一只鹈鹕骑自行车", nil, true, false},
		{"plain image", "", nil, false, false},
		{"ordinary text", "今天的天气不错", nil, false, false},
		{"ocr failure", "", errors.New("unavailable"), false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, err := intelligenceRouteWithOCR(context.Background(), body, func(_ context.Context, _ string) (string, error) { return tc.text, tc.err })
			require.Equal(t, tc.want, got)
			require.Equal(t, tc.wantErr, err != nil)
		})
	}
}

func TestIntelligenceOCRCustomRules(t *testing.T) {
	rules := IntelligenceRules{Rules: []IntelligenceRule{{Name: "special", Type: "contains", Pattern: "自定义测试题", Enabled: true}}}
	image := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AQ=="}}]}]}`)
	result, err := intelligenceRouteWithOCRRules(context.Background(), image, rules, func(context.Context, string) (string, error) { return "自 定 义 测 试 题", nil })
	require.NoError(t, err)
	require.True(t, result)
	result, err = intelligenceRouteWithOCRRules(context.Background(), []byte(`{"messages":[{"role":"user","content":"自定义测试题"},{"role":"user","content":"你好"}]}`), rules, nil)
	require.NoError(t, err)
	require.False(t, result)
	result, err = intelligenceRouteWithOCRRules(context.Background(), []byte(`{"input":"这是自定义测试题"}`), rules, nil)
	require.NoError(t, err)
	require.True(t, result)
	blankRules := IntelligenceRules{Rules: []IntelligenceRule{{Name: "space", Type: "regex", Pattern: `^\s+$`, Enabled: true}}}
	result, err = intelligenceRouteWithOCRRules(context.Background(), image, blankRules, func(context.Context, string) (string, error) { return "", nil })
	require.NoError(t, err)
	require.False(t, result)
}

func TestIntelligenceClassificationReportsMatches(t *testing.T) {
	rules := IntelligenceRules{Rules: []IntelligenceRule{{Name: "我的题目", Type: "contains", Pattern: "特殊样题", Enabled: true}}}
	image := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,AQ=="}}]}]}`)
	matches, err := intelligenceClassifyWithOCRRules(context.Background(), image, rules, func(context.Context, string) (string, error) { return "特殊 样题 糖果测试", nil })
	require.NoError(t, err)
	var names []string
	for _, match := range matches {
		names = append(names, match.Name)
	}
	require.Contains(t, names, "我的题目")
	require.GreaterOrEqual(t, len(matches), 2)
	result, err := intelligenceClassifyWithOCRRules(context.Background(), []byte(`{"messages":[{"role":"user","content":"糖果测试"},{"role":"user","content":"普通请求"}]}`), IntelligenceRules{}, nil)
	require.NoError(t, err)
	require.Empty(t, result)
	failed := []byte(`{"messages":[{"role":"user","content":[{"type":"text","text":"糖果测试"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AQ=="}}]}]}`)
	result, err = intelligenceClassifyWithOCRRules(context.Background(), failed, IntelligenceRules{}, func(context.Context, string) (string, error) { return "", errors.New("ocr failed") })
	require.ErrorIs(t, err, ErrIntelligenceOCR)
	require.NotEmpty(t, result)
}

func TestIntelligenceRulesBatchPrompt(t *testing.T) {
	rules := IntelligenceRules{Rules: []IntelligenceRule{{Name: "custom", Type: "contains", Pattern: "自定义测试题", Enabled: true}}}
	required, err := batchImageIntelligenceRouting([]BatchImageSubmitItem{{Prompt: "画自定义测试题"}}, rules)
	require.NoError(t, err)
	require.True(t, required)
	_, err = batchImageIntelligenceRouting([]BatchImageSubmitItem{{Prompt: "自定义测试题"}, {Prompt: "普通绘画"}}, rules)
	require.ErrorIs(t, err, ErrBatchImageMixedIntelligenceRouting)
}

func TestIntelligenceRulesBatchUsesNormalizedPrompt(t *testing.T) {
	settings := &SettingService{settingRepo: &intelligenceRulesRepo{}}
	_, err := settings.SetIntelligenceRules(t.Context(), IntelligenceRules{Rules: []IntelligenceRule{{Name: "exact", Type: "regex", Pattern: "^foo$", Enabled: true}}})
	require.NoError(t, err)
	svc := &BatchImagePublicService{IntelligenceSettings: settings}
	matches, rules, err := svc.IntelligenceMatches(t.Context(), BatchImageSubmitRequest{Items: []BatchImageSubmitItem{{Prompt: " foo "}}})
	require.NoError(t, err)
	require.Len(t, matches, 1)
	required, err := batchImageIntelligenceRouting([]BatchImageSubmitItem{{Prompt: "foo"}}, rules)
	require.NoError(t, err)
	require.True(t, required)
}

func TestIntelligenceOCRProcessIntegration(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "tesseract")
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nprintf '糖 果 测 试'\n"), 0700))
	t.Setenv("PATH", dir)
	var buf bytes.Buffer
	require.NoError(t, png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 10, 10))))
	body := []byte(`{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,` + base64.StdEncoding.EncodeToString(buf.Bytes()) + `"}}]}]}`)
	required, err := IntelligenceRouteWithOCR(context.Background(), body)
	require.NoError(t, err)
	require.True(t, required)
	require.NoError(t, os.WriteFile(bin, []byte("#!/bin/sh\nexit 1\n"), 0700))
	_, err = IntelligenceRouteWithOCR(context.Background(), body)
	require.ErrorIs(t, err, ErrIntelligenceOCR)
}

func TestIntelligenceOCRPublicIP(t *testing.T) {
	for _, value := range []string{"0.1.2.3", "127.0.0.1", "10.0.0.1", "169.254.169.254", "100.64.0.1", "::1", "fc00::1", "::ffff:127.0.0.1", "64:ff9b::a00:1", "2002:a00:1::"} {
		require.False(t, intelligencePublicIP(net.ParseIP(value)), value)
	}
	require.True(t, intelligencePublicIP(net.ParseIP("8.8.8.8")))
}

func TestIntelligenceOCRRedirectValidation(t *testing.T) {
	good := &http.Request{URL: &url.URL{Scheme: "https", Host: "example.com"}}
	require.NoError(t, intelligenceImageClient.CheckRedirect(good, []*http.Request{{}}))
	require.Error(t, intelligenceImageClient.CheckRedirect(good, []*http.Request{{}, {}, {}, {}}))
	bad := &http.Request{URL: &url.URL{Scheme: "https", Host: "example.com", User: url.User("name")}}
	require.Error(t, intelligenceImageClient.CheckRedirect(bad, []*http.Request{{}}))
}
