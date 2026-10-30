package service

import (
	"math/rand"
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
