package service

import (
	"encoding/json"
	"regexp"
	"strings"

	"golang.org/x/text/unicode/norm"
)

var modelTraceCount = regexp.MustCompile(`(?:\b[1-9][0-9]{2,3}\s*(?:个|個|(?:random\s+)?(?:integers|numbers))|[两二三四五六七八九]百\s*[个個])`)
var intelligenceTankPattern = regexp.MustCompile(`\b(?:tanks?|armou?red\s+(?:vehicles?|tanks?))\b`)
var intelligenceSVGPattern = regexp.MustCompile(`\bs[\s.\-]*v[\s.\-]*g\b`)
var intelligenceDimensionPattern = regexp.MustCompile(`\b(?:2(?:\.5)?|3)\s*[- ]?\s*d\b`)
var intelligenceVehiclePattern = regexp.MustCompile(`\b(?:bicycles?|bikes?|unicycles?|scooters?|skateboards?|cars?|racecars?|tanks?|motorcycles?|motorbikes?|boats?|ships?|planes?|airplanes?|trains?|rockets?)\b`)
var intelligenceRiderPattern = regexp.MustCompile(`(?:\b[a-z][a-z0-9'-]*(?:,?\s+[a-z][a-z0-9'-]*){0,4},?\s+(?:riding|rides|rode|ridden|driving|drives|driven|sitting|seated|piloting|steering|flying)\b|(?:一只|一隻|一位|一个|一個|一名|一头|一頭|一条|一條|隻|画|畫|绘|繪|生成|制作|製作|创作|創作).{0,32}(?:骑|騎|驾|駕|开|開|坐|乘))`)
var intelligenceVisualCreatePattern = regexp.MustCompile(`\b(?:creat(?:e|es|ed|ing)|generat(?:e|es|ed|ing)|draw(?:s|n|ing)?|render(?:s|ed|ing)?|illustrat(?:e|es|ed|ing|ion)|writ(?:e|es|ing|ten)|build(?:s|ing)?|ma(?:ke|kes|king)|design(?:s|ed|ing)?|implement(?:s|ed|ing)?|return(?:s|ed|ing)?|output(?:s|ted|ting)?)\b`)
var intelligenceVisualMotionPattern = regexp.MustCompile(`\b(?:animat(?:e|es|ed|ing|ion|ions)|motion|movements?|moving|danc(?:e|es|ed|ing)|moonwalk(?:ing)?|walking|running|flying|bouncing|jump(?:ing|s)?|cycling|juggling|rotat(?:e|es|ed|ing|ion)|loop(?:ing)?)\b`)
var intelligenceUIElementPattern = regexp.MustCompile(`\b(?:icons?|logos?|spinners?|loading|buttons?|charts?|graphs?|dashboards?|progress\s+bars?)\b`)
var intelligenceNarrativePattern = regexp.MustCompile(`\b(?:cats?|dogs?|fox(?:es)?|pandas?|penguins?|flamingos?|horses?|robots?|astronauts?|animals?|characters?|people|person|emperor|dragons?|wizards?|beach|forest)\b`)
var intelligenceVisualSceneTaskPattern = regexp.MustCompile(`(?:画|畫|绘|繪|制作|製作|生成|创作|創作|描绘|描繪|输出|輸出|\b(?:draw|illustrat(?:e|ing)|create|generate|render|make|return|output)\b).{0,80}(?:一只|一隻|一位|一名|一座|一辆|一輛|人物|角色|动物|動物|猫|貓|企鹅|企鵝|狐狸|熊猫|熊貓|机器人|機器人|宇航员|宇航員|\b(?:cats?|dogs?|fox(?:es)?|pandas?|penguins?|flamingos?|horses?|robots?|astronauts?|animals?|characters?|people|person|emperor|dragons?|wizards?|beach|forest|planets?)\b)`)

type IntelligenceRuleMatch struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Type    string `json:"type"`
	Pattern string `json:"pattern,omitempty"`
}

type IntelligenceBuiltinRule struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
}

var intelligenceBuiltinRules = []IntelligenceBuiltinRule{
	{ID: "builtin.knowledge_iphone", Name: "知识截止与 iPhone 型号", Description: "同时询问知识截止日期和最新 iPhone 型号"},
	{ID: "builtin.pelican_bicycle", Name: "鹈鹕骑自行车", Description: "包含鹈鹕和骑自行车的场景及语言变体"},
	{ID: "builtin.qin_tank", Name: "秦始皇驾驶坦克", Description: "秦始皇或嬴政与坦克的绘制、动画或驾驶场景"},
	{ID: "builtin.candy_named", Name: "糖果测试名称", Description: "明确提及糖果测试、糖果题或 candy benchmark"},
	{ID: "builtin.candy_structure", Name: "糖果抽取题结构", Description: "同时包含糖果、形状和最少抽取或保证等条件"},
	{ID: "builtin.modeltrace_named", Name: "ModelTrace 指纹名称", Description: "同时提及 ModelTrace 与指纹或测试"},
	{ID: "builtin.modeltrace_structure", Name: "ModelTrace 随机整数题结构", Description: "满足数量、范围和不使用工具等条件的随机整数题"},
	{ID: "builtin.svg_scene", Name: "SVG 视觉绘制题结构", Description: "SVG 及二维、三维、HTML、运动场景等绘制变体"},
}

func IntelligenceBuiltinRules() []IntelligenceBuiltinRule {
	return append([]IntelligenceBuiltinRule(nil), intelligenceBuiltinRules...)
}

func MergeIntelligenceRuleMatches(groups ...[]IntelligenceRuleMatch) []IntelligenceRuleMatch {
	var result []IntelligenceRuleMatch
	seen := make(map[string]bool)
	for _, group := range groups {
		for _, match := range group {
			key := match.Type + "\x00" + match.ID
			if !seen[key] {
				seen[key] = true
				result = append(result, match)
			}
		}
	}
	return result
}

// MatchIntelligenceBuiltins reports all applicable built-in rules. Callers
// decide which user message or OCR output to pass and whether rules are enabled.
func MatchIntelligenceBuiltins(text string) []IntelligenceRuleMatch {
	indexes := intelligencePromptMatchIndexes(strings.ToLower(text))
	if matchesVisualIntelligencePrompt(text) {
		indexes = append(indexes, 7)
	}
	var matches []IntelligenceRuleMatch
	for _, index := range indexes {
		rule := intelligenceBuiltinRules[index]
		matches = append(matches, IntelligenceRuleMatch{ID: rule.ID, Name: rule.Name, Type: "builtin"})
	}
	return matches
}

// IsIntelligenceTestRequest classifies only the latest user message. History,
// assistant/tool output and system/developer instructions never influence it.
func IsIntelligenceTestRequest(body []byte) bool {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return false
	}
	text := intelligenceLatestUserText(request)
	return matchesIntelligencePrompt(strings.ToLower(text)) || matchesVisualIntelligencePrompt(text)
}

// intelligenceLatestUserText keeps messages separate. An empty latest user
// message must never fall back to earlier messages.
func intelligenceLatestUserText(request map[string]any) string {
	if request["type"] == "response.create" {
		if response, ok := request["response"].(map[string]any); ok {
			if input, exists := response["input"]; exists {
				return intelligenceLatestInputText(input)
			}
		}
	}
	if _, ok := request["questions"].(map[string]any); ok {
		if state, exists := request["state"]; exists {
			return intelligenceLatestSystemOneState(state)
		}
	}
	for _, key := range []string{"messages", "contents", "input", "prompt"} {
		if value, exists := request[key]; exists {
			return intelligenceLatestInputText(value)
		}
	}
	return ""
}

func intelligenceLatestInputText(value any) string {
	switch v := value.(type) {
	case string:
		return v
	case map[string]any:
		role, _ := v["role"].(string)
		if role != "" && role != "user" {
			return ""
		}
		typ, _ := v["type"].(string)
		if typ != "" && typ != "message" && typ != "text" && typ != "input_text" {
			return ""
		}
		var parts []string
		collectIntelligenceTexts(v, &parts)
		return strings.Join(parts, " ")
	case []any:
		for i := len(v) - 1; i >= 0; i-- {
			switch item := v[i].(type) {
			case string:
				return item // independent input strings are not message blocks
			case map[string]any:
				role, _ := item["role"].(string)
				if role != "" && role != "user" {
					continue
				}
				typ, _ := item["type"].(string)
				if typ != "" && typ != "message" && !intelligenceInputBlockType(typ) {
					continue
				}
				if role == "" && intelligenceInputBlockType(typ) {
					// Adjacent top-level text and image blocks form one user input.
					start := i
					for start > 0 {
						prev, ok := v[start-1].(map[string]any)
						prevType, _ := prev["type"].(string)
						if !ok || prev["role"] != nil || !intelligenceInputBlockType(prevType) {
							break
						}
						start--
					}
					var parts []string
					for _, block := range v[start : i+1] {
						collectIntelligenceTexts(block, &parts)
					}
					return strings.Join(parts, " ")
				}
				return intelligenceLatestInputText(item)
			}
		}
	}
	return ""
}

func intelligenceInputBlockType(typ string) bool {
	return typ == "text" || typ == "input_text" || typ == "input_image" || typ == "image_url"
}

// System One has no chat roles at the envelope level: state is submitted user
// content, while question instructions and criteria are not user messages.
func intelligenceLatestSystemOneState(state any) string {
	if text, found := intelligenceSystemOneConversation(state); found {
		return text
	}
	var parts []string
	collectSystemOneText(state, &parts)
	return strings.Join(parts, " ")
}

// Only interpret arrays with explicit roles as conversation history; ordinary
// System One arrays and objects can be structured content of one user input.
func intelligenceSystemOneConversation(value any) (string, bool) {
	switch v := value.(type) {
	case map[string]any:
		if _, hasRole := v["role"]; hasRole {
			return intelligenceLatestInputText(v), true
		}
		for _, key := range []string{"messages", "contents", "input", "context"} {
			if nested, ok := v[key]; ok {
				if text, found := intelligenceSystemOneConversation(nested); found {
					return text, true
				}
				if key != "context" {
					return intelligenceLatestInputText(nested), true
				}
			}
		}
	case []any:
		for _, item := range v {
			if m, ok := item.(map[string]any); ok {
				if _, hasRole := m["role"]; hasRole {
					return intelligenceLatestInputText(v), true
				}
			}
		}
	}
	return "", false
}

// matchesVisualIntelligencePrompt recognises the structure of SVG visual
// benchmark variants without restricting the scene to a fixed list of names.
// Ordinary animation requests with the same structure are indistinguishable.
func normalizeIntelligenceVisualText(text string) string {
	text = strings.ToLower(norm.NFKC.String(text))
	return strings.Map(func(r rune) rune {
		switch r {
		case '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			return -1
		}
		return r
	}, text)
}

func hasExplicitIntelligenceScene(text string) bool {
	return intelligenceVisualSceneTaskPattern.MatchString(normalizeIntelligenceVisualText(text))
}

func hasConcreteIntelligenceScene(text string) bool {
	text = normalizeIntelligenceVisualText(text)
	return intelligenceNarrativePattern.MatchString(text) || intelligenceRiderPattern.MatchString(text) || intelligenceContainsAny(text, "一只", "一隻", "一位", "一名", "一座", "一辆", "一輛", "人物", "角色", "动物", "動物", "猫", "貓", "企鹅", "企鵝", "狐狸", "熊猫", "熊貓", "火烈鸟", "火烈鳥", "海滩", "海灘", "森林", "沙滩", "沙灘", "机器人", "機器人", "宇航员", "宇航員")
}

func matchesVisualIntelligencePrompt(text string) bool {
	text = normalizeIntelligenceVisualText(text)
	isSVG := intelligenceSVGPattern.MatchString(text) || intelligenceContainsAny(text, "scalable vector graphics", "矢量图", "矢量圖")
	create := intelligenceVisualCreatePattern.MatchString(text) || intelligenceContainsAny(text, "生成", "创建", "創建", "创作", "創作", "制作", "製作", "绘", "繪", "画", "畫", "编写", "編寫", "写", "寫", "实现", "實現", "设计", "設計", "做", "输出", "輸出")
	if !isSVG || !create {
		return false
	}
	vehicle := intelligenceVehiclePattern.MatchString(text) || intelligenceContainsAny(text, "自行车", "自行車", "单车", "單車", "独轮车", "獨輪車", "滑板", "滑板车", "滑板車", "摩托", "汽车", "汽車", "赛车", "賽車", "坦克", "船", "飞机", "飛機", "火箭", "火车", "火車")
	if vehicle && intelligenceRiderPattern.MatchString(text) {
		return true
	}
	// The user explicitly opted for broad matching of SVG + 2D/3D or HTML
	// drawing requests, including static images. Similar ordinary requests may
	// therefore be routed to the dedicated test accounts as well.
	dimension := intelligenceDimensionPattern.MatchString(text) || intelligenceContainsAny(text, "2d", "2-d", "3d", "3-d", "二维", "二維", "三维", "三維", "立体", "立體", "two-dimensional", "two dimensional", "three-dimensional", "three dimensional")
	if dimension || intelligenceContainsAny(text, "html") {
		return true
	}
	motion := intelligenceVisualMotionPattern.MatchString(text) || intelligenceContainsAny(text, "动画", "動畫", "动态", "動態", "运动", "運動", "動起來", "动起来", "跳舞", "跳跃", "跳躍", "弹跳", "彈跳", "行驶", "行駛", "骑行", "騎行", "摇摆", "搖擺", "飞行", "飛行")
	if !motion && !hasExplicitIntelligenceScene(text) {
		return false
	}
	// Without 2D/3D or HTML, avoid treating an ordinary animated UI component
	// as a benchmark unless the prompt also describes a concrete scene.
	ui := intelligenceUIElementPattern.MatchString(text) || intelligenceContainsAny(text, "图标", "圖標", "按钮", "按鈕", "加载", "載入", "进度条", "進度條", "图表", "圖表", "流程图", "流程圖")
	scene := hasConcreteIntelligenceScene(text)
	return !ui || scene
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
	return len(intelligencePromptMatchIndexes(s)) > 0
}

func intelligencePromptMatchIndexes(s string) []int {
	var matches []int
	// Knowledge-cutoff probes often pair a cutoff question with the newest
	// iPhone model, with or without explicit instructions not to browse.
	knowledge := intelligenceContainsAny(s, "知识库", "知識庫", "知识截止", "知識截止", "知识更新", "知識更新", "训练数据", "訓練資料", "训练资料", "訓練數據", "knowledge cutoff", "knowledge cut-off", "knowledge cut off", "training data", "knowledge base")
	cutoff := intelligenceContainsAny(s, "截止", "截至", "更新到", "更新至", "到哪年", "到哪一年", "到什么时间", "到什麼時間", "截止日期", "cutoff", "cut-off", "cut off", "up to", "updated through", "updated until", "through what date")
	phone := intelligenceContainsAny(s, "iphone", "i phone", "苹果手机", "蘋果手機")
	latest := intelligenceContainsAny(s, "最新", "新款", "最近", "出到", "第几代", "第幾代", "哪一代", "哪款", "什么型号", "什麼型號", "最新型号", "最新型號", "latest", "newest", "most recent", "current model", "current iphone", "which iphone", "what iphone")
	if knowledge && cutoff && phone && latest {
		matches = append(matches, 0)
	}
	if intelligenceContainsAny(s, "鹈鹕", "鵜鶘", "pelican") && intelligenceContainsAny(s, "自行车", "單車", "单车", "骑车", "騎車", "骑行", "bicycle", "bike", "cycling") {
		matches = append(matches, 1)
	}
	// The Qin Shi Huang driving a tank animation is a related visual benchmark.
	// Require the specific subject and vehicle plus a scene/creation signal, so
	// unrelated SVG work or historical discussions do not get redirected.
	qinName := strings.NewReplacer("-", " ", "_", " ").Replace(s)
	if intelligenceContainsAny(qinName, "秦始皇", "嬴政", "qin shi huang", "qin shihuang", "ying zheng", "yingzheng", "first emperor of china", "first chinese emperor") &&
		(intelligenceContainsAny(s, "坦克", "装甲车", "裝甲車", "装甲战车", "裝甲戰車") || intelligenceTankPattern.MatchString(s)) &&
		intelligenceContainsAny(s, "svg", "canvas", "html", "2d", "二维", "二維", "动画", "動畫", "动态", "動態", "绘", "繪", "画", "畫", "驾驶", "駕駛", "开坦克", "開坦克", "开着", "開著", "乘坐", "坐在", "draw", "illustrat", "sketch", "animat", "driv", "ride", "riding", "piloting", "operating", "on a tank", "in a tank") {
		matches = append(matches, 2)
	}
	if intelligenceContainsAny(s, "糖果测试", "糖果測試", "糖果题", "糖果題", "candy test", "candy benchmark") {
		matches = append(matches, 3)
	}
	if intelligenceContainsAny(s, "糖果", "糖块", "糖塊", "candy", "candies", "sweets") &&
		intelligenceContainsAny(s, "形状", "形狀", "圆形", "圓形", "五角星", "shape", "round", "star-shaped") &&
		intelligenceContainsAny(s, "最少", "至少", "保证", "保證", "摸出", "抽取", "minimum", "least", "guarantee", "draw") {
		matches = append(matches, 4)
	}
	if strings.Contains(s, "modeltrace") && intelligenceContainsAny(s, "指纹", "指紋", "fingerprint", "测试", "測試", "test") {
		matches = append(matches, 5)
	}
	if intelligenceContainsAny(s, "到", "至", "之间", "之間", "范围", "範圍", "between", "range", " to ", "-") && intelligenceContainsAny(s, "整数", "整數", "integer", "numbers", "数字", "數字") &&
		modelTraceCount.MatchString(s) && intelligenceContainsAny(s, "禁止", "不要", "不使用", "不借助", "without", "no tool", "do not", "don't", "不得") &&
		intelligenceContainsAny(s, "工具", "代码", "代碼", "随机", "隨機", "tool", "code", "random") {
		matches = append(matches, 6)
	}
	return matches
}
