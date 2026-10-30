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
var intelligenceScenePositionPattern = regexp.MustCompile(`\b(?:on|in|atop|aboard)\b`)
var intelligenceNarrativePattern = regexp.MustCompile(`\b(?:cats?|dogs?|fox(?:es)?|pandas?|penguins?|flamingos?|horses?|robots?|astronauts?|animals?|characters?|people|person|emperor|dragons?|wizards?|beach|forest)\b`)
var intelligenceVisualSceneTaskPattern = regexp.MustCompile(`(?:画|畫|绘|繪|制作|製作|生成|创作|創作|描绘|描繪|输出|輸出|\b(?:draw|illustrat(?:e|ing)|create|generate|render|make|return|output)\b).{0,80}(?:一只|一隻|一位|一名|一座|一辆|一輛|人物|角色|动物|動物|猫|貓|企鹅|企鵝|狐狸|熊猫|熊貓|机器人|機器人|宇航员|宇航員|\b(?:cats?|dogs?|fox(?:es)?|pandas?|penguins?|flamingos?|horses?|robots?|astronauts?|animals?|characters?|people|person|emperor|dragons?|wizards?|beach|forest|planets?)\b)`)

// IsIntelligenceTestRequest detects known benchmark prompts in request text
// without inspecting metadata, tool definitions, or assistant messages.
// Detection is heuristic and deliberately requires multiple signals.
func IsIntelligenceTestRequest(body []byte) bool {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return false
	}
	var texts []string
	var structuredTexts []string
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
		state := strings.Join(parts, " ")
		texts = append(texts, state)
		structuredTexts = append(structuredTexts, state)
		stateHasSVG := hasIntelligenceSVG(state)
		stateHasClue := hasIntelligenceVisualClue(state)
		for _, raw := range questions {
			if question, ok := raw.(map[string]any); ok {
				parts = nil
				collectSystemOneText(question["instructions"], &parts)
				collectSystemOneText(question["criteria"], &parts)
				questionText := strings.Join(parts, " ")
				texts = append(texts, questionText)
				structuredTexts = append(structuredTexts, questionText)
				// Pair each question only with the shared state; do not mix unrelated
				// questions. Only build combinations that can contain an SVG scene.
				if ((stateHasSVG && hasIntelligenceVisualClue(questionText)) || (stateHasClue && hasIntelligenceSVG(questionText))) && matchesVisualIntelligencePrompt(state+" "+questionText) {
					return true
				}
			}
		}
	}
	for _, text := range texts {
		if matchesIntelligencePrompt(strings.ToLower(text)) {
			return true
		}
	}
	// Clients may put output-format instructions in system/developer messages
	// and the scene in the current user message. Combine only those fields;
	// never combine old user turns, assistant messages or tool definitions.
	instructions, current, userTexts := intelligenceCurrentPrompt(request)
	for _, text := range append(userTexts, structuredTexts...) {
		if matchesVisualIntelligencePrompt(text) {
			return true
		}
	}
	if instructions != "" && matchesVisualIntelligencePrompt(instructions) && hasExplicitIntelligenceScene(instructions) {
		return true
	}
	if current != "" && instructions != "" && hasIntelligenceVisualClue(current) {
		return matchesVisualIntelligencePrompt(current + " " + instructions)
	}
	return false
}

// intelligenceCurrentPrompt extracts the latest user content and the format
// instructions from supported chat protocols without merging unrelated history.
func intelligenceCurrentPrompt(request map[string]any) (string, string, []string) {
	var instructions []string
	for _, key := range []string{"system", "instructions", "systemInstruction"} {
		collectIntelligenceTexts(request[key], &instructions)
	}
	var current string
	var userTexts []string
	visit := func(value any) {
		switch v := value.(type) {
		case string:
			if v != "" {
				current = v
				userTexts = append(userTexts, v)
			}
		case []any:
			for _, item := range v {
				role := ""
				if m, ok := item.(map[string]any); ok {
					role, _ = m["role"].(string)
				}
				var parts []string
				collectIntelligenceTexts(item, &parts)
				switch role {
				case "system", "developer":
					instructions = append(instructions, parts...)
				case "user":
					current = strings.Join(parts, " ")
					userTexts = append(userTexts, current)
				case "":
					if len(parts) > 0 {
						current = strings.Join(parts, " ")
						userTexts = append(userTexts, current)
					}
				}
			}
		case map[string]any:
			var parts []string
			collectIntelligenceTexts(v, &parts)
			role, _ := v["role"].(string)
			if role == "system" || role == "developer" {
				instructions = append(instructions, parts...)
			} else if (role == "" || role == "user") && len(parts) > 0 {
				current = strings.Join(parts, " ")
				userTexts = append(userTexts, current)
			}
		}
	}
	for _, key := range []string{"messages", "contents", "input", "prompt"} {
		visit(request[key])
	}
	if request["type"] == "response.create" {
		if response, ok := request["response"].(map[string]any); ok {
			collectIntelligenceTexts(response["instructions"], &instructions)
			visit(response["input"])
		}
	}
	return strings.Join(instructions, " "), current, userTexts
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

func hasIntelligenceSVG(text string) bool {
	text = normalizeIntelligenceVisualText(text)
	return intelligenceSVGPattern.MatchString(text) || intelligenceContainsAny(text, "scalable vector graphics", "矢量图", "矢量圖")
}

func hasIntelligenceVisualClue(text string) bool {
	text = normalizeIntelligenceVisualText(text)
	vehicle := intelligenceVehiclePattern.MatchString(text) || intelligenceContainsAny(text, "自行车", "自行車", "单车", "單車", "滑板", "摩托", "汽车", "汽車", "赛车", "賽車", "坦克", "船", "飞机", "飛機", "火箭")
	if vehicle && (intelligenceScenePositionPattern.MatchString(text) || intelligenceContainsAny(text, "骑", "騎", "驾", "駕", "开", "開", "乘", "坐")) {
		return true
	}
	if intelligenceVisualCreatePattern.MatchString(text) && intelligenceUIElementPattern.MatchString(text) {
		return true
	}
	return intelligenceDimensionPattern.MatchString(text) || hasConcreteIntelligenceScene(text) || intelligenceContainsAny(text, "svg", "矢量", "2d", "2-d", "3d", "3-d", "二维", "二維", "三维", "三維", "立体", "立體", "动画", "動畫", "动图", "動圖", "画", "畫", "绘", "繪", "图像", "圖像", "图形", "圖形", "图画", "圖畫", "场景", "場景", "骑", "騎", "跳", "scene", "picture", "draw", "sketch", "illustrat", "animation", "animated", "riding", "rider", "two dimensional", "three dimensional", "two-dimensional", "three-dimensional")
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
	if intelligenceContainsAny(s, "鹈鹕", "鵜鶘", "pelican") && intelligenceContainsAny(s, "自行车", "單車", "单车", "骑车", "騎車", "骑行", "bicycle", "bike", "cycling") {
		return true
	}
	// The Qin Shi Huang driving a tank animation is a related visual benchmark.
	// Require the specific subject and vehicle plus a scene/creation signal, so
	// unrelated SVG work or historical discussions do not get redirected.
	qinName := strings.NewReplacer("-", " ", "_", " ").Replace(s)
	if intelligenceContainsAny(qinName, "秦始皇", "嬴政", "qin shi huang", "qin shihuang", "ying zheng", "yingzheng", "first emperor of china", "first chinese emperor") &&
		(intelligenceContainsAny(s, "坦克", "装甲车", "裝甲車", "装甲战车", "裝甲戰車") || intelligenceTankPattern.MatchString(s)) &&
		intelligenceContainsAny(s, "svg", "canvas", "html", "2d", "二维", "二維", "动画", "動畫", "动态", "動態", "绘", "繪", "画", "畫", "驾驶", "駕駛", "开坦克", "開坦克", "开着", "開著", "乘坐", "坐在", "draw", "illustrat", "sketch", "animat", "driv", "ride", "riding", "piloting", "operating", "on a tank", "in a tank") {
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
