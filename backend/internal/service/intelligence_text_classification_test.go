package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntelligenceClassificationIgnoresImageContent(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		want       bool
	}{
		{"image only data", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,%%%"}}]}]}`, false},
		{"image file id", `{"input":[{"role":"user","content":[{"type":"input_image","file_id":"file-123"}]}]}`, false},
		{"gemini image", `{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"not-base64"}}]}]}`, false},
		{"text and image", `{"messages":[{"role":"user","content":[{"type":"text","text":"糖果测试"},{"type":"image_url","image_url":{"url":"invalid"}}]}]}`, true},
		{"history text ignored", `{"messages":[{"role":"user","content":"糖果测试"},{"role":"assistant","content":"好的"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"invalid"}}]}]}`, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			matches, err := ClassifyIntelligenceRequest(context.Background(), []byte(tc.body), IntelligenceRules{})
			require.NoError(t, err)
			require.Equal(t, tc.want, len(matches) > 0)
		})
	}
}

func TestIntelligenceTextClassificationCustomAndDisabled(t *testing.T) {
	rules := IntelligenceRules{Rules: []IntelligenceRule{{Name: "special", Type: "contains", Pattern: "自定义测试题", Enabled: true}}, BuiltinDisabledIDs: []string{"builtin.thibault_sottiaux"}}
	matches, err := ClassifyIntelligenceRequest(context.Background(), []byte(`{"input":"这是自定义测试题"}`), rules)
	require.NoError(t, err)
	require.Len(t, matches, 1)
	require.Equal(t, "special", matches[0].Name)
	matches, err = ClassifyIntelligenceRequest(context.Background(), []byte(`{"input":"Do you know Thibault Sottiaux?"}`), rules)
	require.NoError(t, err)
	require.Empty(t, matches)
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
