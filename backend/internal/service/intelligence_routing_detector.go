package service

import (
	"encoding/json"
	"regexp"
	"strings"
)

var modelTraceCount = regexp.MustCompile(`(?:\b[1-9][0-9]{2,3}\s*(?:个|個|(?:random\s+)?(?:integers|numbers))|[两二三四五六七八九]百\s*[个個])`)

// IsIntelligenceTestRequest detects known benchmark prompts in request text
// without inspecting metadata, tool definitions, or assistant messages.
// Detection is heuristic and deliberately requires multiple signals.
func IsIntelligenceTestRequest(body []byte) bool {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return false
	}
	var texts []string
	for _, key := range []string{"messages", "input", "contents", "prompt", "system", "instructions", "systemInstruction"} {
		collectIntelligenceTexts(request[key], &texts)
	}
	if request["type"] == "response.create" {
		if response, ok := request["response"].(map[string]any); ok {
			collectIntelligenceTexts(response["input"], &texts)
			collectIntelligenceTexts(response["instructions"], &texts)
		}
	}
	if questions, ok := request["questions"].(map[string]any); ok {
		var parts []string
		collectSystemOneText(request["state"], &parts)
		texts = append(texts, strings.Join(parts, " "))
		for _, raw := range questions {
			if question, ok := raw.(map[string]any); ok {
				parts = nil
				collectSystemOneText(question["instructions"], &parts)
				collectSystemOneText(question["criteria"], &parts)
				texts = append(texts, strings.Join(parts, " "))
			}
		}
	}
	for _, text := range texts {
		if matchesIntelligencePrompt(strings.ToLower(text)) {
			return true
		}
	}
	return false
}

func collectSystemOneText(value any, texts *[]string) {
	switch v := value.(type) {
	case string:
		*texts = append(*texts, v)
	case []any:
		for _, item := range v {
			collectSystemOneText(item, texts)
		}
	case map[string]any:
		for _, item := range v {
			collectSystemOneText(item, texts)
		}
	}
}

// collectIntelligenceTexts only follows protocol content fields. Content
// blocks of one message are joined so that prompts split across blocks match.
func collectIntelligenceTexts(value any, texts *[]string) {
	switch v := value.(type) {
	case string:
		*texts = append(*texts, v)
	case []any:
		for _, item := range v {
			collectIntelligenceTexts(item, texts)
		}
	case map[string]any:
		if role, ok := v["role"].(string); ok && role != "user" && role != "system" && role != "developer" {
			return
		}
		if typ, ok := v["type"].(string); ok && typ != "message" && typ != "text" && typ != "input_text" {
			return
		}
		var pieces []string
		for _, key := range []string{"content", "parts", "text"} {
			collectIntelligenceTexts(v[key], &pieces)
		}
		if len(pieces) > 0 {
			*texts = append(*texts, strings.Join(pieces, " "))
		}
	}
}

func intelligenceContainsAny(s string, terms ...string) bool {
	for _, term := range terms {
		if strings.Contains(s, term) {
			return true
		}
	}
	return false
}

func matchesIntelligencePrompt(s string) bool {
	if intelligenceContainsAny(s, "鹈鹕", "鵜鶘", "pelican") && intelligenceContainsAny(s, "自行车", "單車", "单车", "骑车", "騎車", "骑行", "bicycle", "bike", "cycling") {
		return true
	}
	if intelligenceContainsAny(s, "糖果测试", "糖果測試", "糖果题", "糖果題", "candy test", "candy benchmark") {
		return true
	}
	if intelligenceContainsAny(s, "糖果", "糖块", "糖塊", "candy", "candies", "sweets") &&
		intelligenceContainsAny(s, "形状", "形狀", "圆形", "圓形", "五角星", "shape", "round", "star-shaped") &&
		intelligenceContainsAny(s, "最少", "至少", "保证", "保證", "摸出", "抽取", "minimum", "least", "guarantee", "draw") {
		return true
	}
	if strings.Contains(s, "modeltrace") && intelligenceContainsAny(s, "指纹", "指紋", "fingerprint", "测试", "測試", "test") {
		return true
	}
	return intelligenceContainsAny(s, "到", "至", "之间", "之間", "范围", "範圍", "between", "range", " to ", "-") && intelligenceContainsAny(s, "整数", "整數", "integer", "numbers", "数字", "數字") &&
		modelTraceCount.MatchString(s) && intelligenceContainsAny(s, "禁止", "不要", "不使用", "不借助", "without", "no tool", "do not", "don't", "不得") &&
		intelligenceContainsAny(s, "工具", "代码", "代碼", "随机", "隨機", "tool", "code", "random")
}
