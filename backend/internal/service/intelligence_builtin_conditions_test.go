package service

import (
	"math/rand"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDefaultBuiltinConditionTreesMatchNativePredicates(t *testing.T) {
	corpus := []string{"", "hello", "糖果测试", "candy test", "鹈鹕骑自行车", "秦始皇开坦克 SVG", "知识库截止到哪一年 最新 iphone 型号", "modeltrace 指纹", "选择300个1到999的整数不要使用工具", "用 SVG 做 3D 海底城市", "create svg loading spinner", "DRAW ＳＶＧ 3Ｄ", "用s\u200bv\u200bg画小猫跳舞", "do you know Thibault Sottiaux on X?", "不使用工具，100 欧姆电阻串并联得到 67 欧姆，最少多少个？", "using 100-ohm resistors, how many to make 67 ohms?"}
	rng := rand.New(rand.NewSource(42))
	words := []string{"糖果", "形状", "至少", "modeltrace", "指纹", "random", "300", "integer", "without", "code", "qin", "shi", "huang", "tank", "鹈鹕", "自行车", "svg", "html", "2d", "draw", "cat", "button", "animation", ""}
	for i := 0; i < 500; i++ {
		var text string
		for j := 0; j < 10; j++ {
			text += words[rng.Intn(len(words))] + " "
		}
		corpus = append(corpus, text)
	}
	conditions := DefaultIntelligenceBuiltinConditions()
	for _, text := range corpus {
		native := map[string]bool{}
		for _, match := range MatchIntelligenceBuiltins(text) {
			native[match.ID] = true
		}
		for id, condition := range conditions {
			got, err := MatchIntelligenceCondition(condition, text)
			require.NoError(t, err, id)
			require.Equal(t, native[id], got, "rule %s for %q", id, text)
		}
	}
}

func TestBuiltinConditionOverridesAndValidation(t *testing.T) {
	defaults := DefaultIntelligenceBuiltinConditions()
	condition := defaults["builtin.pelican_bicycle"]
	condition.Children[0].Values = []string{"flamingo"}
	prepared, err := prepareIntelligenceRules(IntelligenceRules{BuiltinConditions: map[string]IntelligenceCondition{"builtin.pelican_bicycle": condition}})
	require.NoError(t, err)
	require.Empty(t, prepared.MatchBuiltins("鹈鹕骑自行车"))
	require.Len(t, prepared.MatchBuiltins("flamingo riding bicycle"), 1)
	prepared.BuiltinDisabledIDs = []string{"builtin.pelican_bicycle"}
	require.Empty(t, prepared.MatchBuiltins("flamingo riding bicycle"))
	_, err = prepareIntelligenceRules(IntelligenceRules{BuiltinConditions: map[string]IntelligenceCondition{"builtin.pelican_bicycle": {Op: "regex", Pattern: "[", Text: "plain"}}})
	require.Error(t, err)
	_, err = prepareIntelligenceRules(IntelligenceRules{BuiltinConditions: map[string]IntelligenceCondition{"builtin.pelican_bicycle": {Op: "regex", Pattern: ".*", Text: "plain"}}})
	require.Error(t, err, "a complete condition may not match empty input")
	_, err = prepareIntelligenceRules(IntelligenceRules{BuiltinConditions: map[string]IntelligenceCondition{"builtin.pelican_bicycle": condAll(IntelligenceCondition{Op: "regex", Pattern: ".*", Text: "plain"}, condTerms("plain", "bird"))}})
	require.NoError(t, err, "a subexpression may match empty when the full rule cannot")
}

func TestBuiltinConditionNormalizesEditedTerms(t *testing.T) {
	for _, tc := range []struct{ mode, term, input string }{
		{"visual", "ＳＶＧ", "请使用svg"},
		{"visual", "s\u200bv\u200bg", "请使用SVG"},
		{"qin", "qin-shi_huang", "Qin Shi Huang"},
	} {
		condition := IntelligenceCondition{Op: "contains_any", Text: tc.mode, Values: []string{tc.term}}
		matched, err := MatchIntelligenceCondition(condition, tc.input)
		require.NoError(t, err)
		require.True(t, matched, tc.mode)
	}
}

func TestBuiltinConditionPersistsAndRestores(t *testing.T) {
	repo := &intelligenceRulesRepo{}
	s := &SettingService{settingRepo: repo}
	conditions := DefaultIntelligenceBuiltinConditions()
	condition := conditions["builtin.candy_named"]
	condition.Values = []string{"新的测试题"}
	_, err := s.SetIntelligenceRules(t.Context(), IntelligenceRules{BuiltinConditions: map[string]IntelligenceCondition{"builtin.candy_named": condition}})
	require.NoError(t, err)
	reloaded, err := (&SettingService{settingRepo: repo}).RuntimeIntelligenceRules(t.Context())
	require.NoError(t, err)
	require.Empty(t, reloaded.MatchAll("糖果测试"))
	require.NotEmpty(t, reloaded.MatchAll("新的测试题"))
	require.NotEmpty(t, reloaded.MatchAll("鹈鹕骑自行车"), "other built-in rules must remain unchanged")
	reloaded.BuiltinConditions = map[string]IntelligenceCondition{}
	_, err = s.SetIntelligenceRules(t.Context(), reloaded)
	require.NoError(t, err)
	restored, err := (&SettingService{settingRepo: repo}).GetIntelligenceRules(t.Context())
	require.NoError(t, err)
	require.NotEmpty(t, restored.MatchAll("糖果测试"))
	require.Empty(t, restored.MatchAll("新的测试题"))
}

func TestIntelligenceConditionEvidenceReportsMatchedBranches(t *testing.T) {
	condition := condAll(
		condTerms("plain", "糖果", "点心"),
		condAny(condTerms("plain", "圆形"), condTerms("plain", "星形")),
		condNot(condTerms("plain", "剧透")),
		IntelligenceCondition{Op: "regex", Text: "plain", Pattern: `\b[0-9]+\b`},
	)
	matched, words := MatchIntelligenceConditionEvidence(condition, "请问糖果圆形有 12 个？")
	require.True(t, matched)
	require.Equal(t, []string{"糖果", "圆形", "12"}, words)
	matched, words = MatchIntelligenceConditionEvidence(condition, "糖果圆形 12 个 剧透")
	require.False(t, matched)
	require.Empty(t, words)
	matched, words = MatchIntelligenceConditionEvidence(condTerms("plain", "糖", "糖果"), "糖果题")
	require.True(t, matched)
	require.Equal(t, []string{"糖"}, words, "show the first matching term of an any-of group")
}

func TestIntelligenceBuiltinEvidencePreservesActualMatch(t *testing.T) {
	matches := MatchIntelligenceBuiltins("画一只鹈鹕骑自行车")
	require.NotEmpty(t, matches)
	for _, match := range matches {
		if match.ID == "builtin.pelican_bicycle" {
			require.Contains(t, match.MatchedKeywords, "鹈鹕")
			require.Contains(t, match.MatchedKeywords, "自行车")
			return
		}
	}
	t.Fatal("pelican rule not found")
}

func TestIntelligenceEvidenceLimitAndDedup(t *testing.T) {
	long := strings.Repeat("糖", 200)
	trimmed := NormalizeIntelligenceEvidence(long)
	require.True(t, strings.HasSuffix(trimmed, "…"))
	require.Less(t, len(trimmed), len(long))
	require.Equal(t, []string{"糖果", "圆形"}, MergeIntelligenceEvidence([]string{"糖果", "圆形"}, []string{"糖果", ""}))
	merged := MergeIntelligenceRuleMatches(
		[]IntelligenceRuleMatch{{ID: "a", Name: "题目", Type: "builtin", MatchedKeywords: []string{"糖果"}}},
		[]IntelligenceRuleMatch{{ID: "a", Name: "题目", Type: "builtin", MatchedKeywords: []string{"圆形", "糖果"}}},
	)
	require.Len(t, merged, 1)
	require.Equal(t, []string{"糖果", "圆形"}, merged[0].MatchedKeywords)
	multiplePatterns := MergeIntelligenceRuleMatches(
		[]IntelligenceRuleMatch{{ID: "a", Type: "builtin", Pattern: "糖果", MatchedKeywords: []string{"糖果"}}},
		[]IntelligenceRuleMatch{{ID: "a", Type: "builtin", Pattern: "圆形", MatchedKeywords: []string{"圆形"}}},
	)
	require.Empty(t, multiplePatterns[0].Pattern)
	require.Equal(t, []string{"糖果", "圆形"}, multiplePatterns[0].MatchedKeywords)
	nativeAndExtension := MergeIntelligenceRuleMatches(
		[]IntelligenceRuleMatch{{ID: "a", Type: "builtin", Pattern: "糖果", MatchedKeywords: []string{"糖果"}}},
		[]IntelligenceRuleMatch{{ID: "a", Type: "builtin", MatchedKeywords: []string{"圆形"}}},
	)
	require.Empty(t, nativeAndExtension[0].Pattern)
	require.Equal(t, []string{"糖果", "圆形"}, nativeAndExtension[0].MatchedKeywords)
}
