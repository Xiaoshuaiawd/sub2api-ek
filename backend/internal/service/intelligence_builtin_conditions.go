package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"
)

// IntelligenceCondition is a bounded boolean tree. Leaf text specifies the
// original built-in's exact normalization; it is never inferred from input.
type IntelligenceCondition struct {
	Op               string                  `json:"op"`
	Children         []IntelligenceCondition `json:"children,omitempty"`
	Values           []string                `json:"values,omitempty"`
	Pattern          string                  `json:"pattern,omitempty"`
	Text             string                  `json:"text,omitempty"`
	compiled         *regexp.Regexp
	normalizedValues []string
}

func (c *IntelligenceCondition) UnmarshalJSON(data []byte) error {
	type plain IntelligenceCondition
	var value plain
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&value); err != nil {
		return err
	}
	*c = IntelligenceCondition(value)
	return nil
}

func condAll(children ...IntelligenceCondition) IntelligenceCondition {
	return IntelligenceCondition{Op: "all", Children: children}
}
func condAny(children ...IntelligenceCondition) IntelligenceCondition {
	return IntelligenceCondition{Op: "any", Children: children}
}
func condNot(child IntelligenceCondition) IntelligenceCondition {
	return IntelligenceCondition{Op: "not", Children: []IntelligenceCondition{child}}
}
func condTerms(text string, values ...string) IntelligenceCondition {
	return IntelligenceCondition{Op: "contains_any", Text: text, Values: values}
}
func condRegex(text string, pattern *regexp.Regexp) IntelligenceCondition {
	return IntelligenceCondition{Op: "regex", Text: text, Pattern: pattern.String(), compiled: pattern}
}

// DefaultIntelligenceBuiltinConditions constructs independent trees faithfully
// representing the legacy predicates. Mutating a returned tree is safe.
func DefaultIntelligenceBuiltinConditions() map[string]IntelligenceCondition {
	p := func(values ...string) IntelligenceCondition { return condTerms("plain", values...) }
	v := func(values ...string) IntelligenceCondition { return condTerms("visual", values...) }
	rv := func(pattern *regexp.Regexp) IntelligenceCondition { return condRegex("visual", pattern) }
	knowledge := condAll(
		p("知识库", "知識庫", "知识截止", "知識截止", "知识更新", "知識更新", "训练数据", "訓練資料", "训练资料", "訓練數據", "knowledge cutoff", "knowledge cut-off", "knowledge cut off", "training data", "knowledge base"),
		p("截止", "截至", "更新到", "更新至", "到哪年", "到哪一年", "到什么时间", "到什麼時間", "截止日期", "cutoff", "cut-off", "cut off", "up to", "updated through", "updated until", "through what date"),
		p("iphone", "i phone", "苹果手机", "蘋果手機"),
		p("最新", "新款", "最近", "出到", "第几代", "第幾代", "哪一代", "哪款", "什么型号", "什麼型號", "最新型号", "最新型號", "latest", "newest", "most recent", "current model", "current iphone", "which iphone", "what iphone"))
	pelican := condAll(p("鹈鹕", "鵜鶘", "pelican"), p("自行车", "單車", "单车", "骑车", "騎車", "骑行", "bicycle", "bike", "cycling"))
	qin := condAll(
		condTerms("qin", "秦始皇", "嬴政", "qin shi huang", "qin shihuang", "ying zheng", "yingzheng", "first emperor of china", "first chinese emperor"),
		condAny(p("坦克", "装甲车", "裝甲車", "装甲战车", "裝甲戰車"), condRegex("plain", intelligenceTankPattern)),
		p("svg", "canvas", "html", "2d", "二维", "二維", "动画", "動畫", "动态", "動態", "绘", "繪", "画", "畫", "驾驶", "駕駛", "开坦克", "開坦克", "开着", "開著", "乘坐", "坐在", "draw", "illustrat", "sketch", "animat", "driv", "ride", "riding", "piloting", "operating", "on a tank", "in a tank"))
	candyName := p("糖果测试", "糖果測試", "糖果题", "糖果題", "candy test", "candy benchmark")
	candy := condAll(p("糖果", "糖块", "糖塊", "candy", "candies", "sweets"), p("形状", "形狀", "圆形", "圓形", "五角星", "shape", "round", "star-shaped"), p("最少", "至少", "保证", "保證", "摸出", "抽取", "minimum", "least", "guarantee", "draw"))
	traceName := condAll(p("modeltrace"), p("指纹", "指紋", "fingerprint", "测试", "測試", "test"))
	trace := condAll(p("到", "至", "之间", "之間", "范围", "範圍", "between", "range", " to ", "-"), p("整数", "整數", "integer", "numbers", "数字", "數字"), condRegex("plain", modelTraceCount), p("禁止", "不要", "不使用", "不借助", "without", "no tool", "do not", "don't", "不得"), p("工具", "代码", "代碼", "随机", "隨機", "tool", "code", "random"))
	svg := condAny(rv(intelligenceSVGPattern), v("scalable vector graphics", "矢量图", "矢量圖"))
	create := condAny(rv(intelligenceVisualCreatePattern), v("生成", "创建", "創建", "创作", "創作", "制作", "製作", "绘", "繪", "画", "畫", "编写", "編寫", "写", "寫", "实现", "實現", "设计", "設計", "做", "输出", "輸出"))
	vehicle := condAny(rv(intelligenceVehiclePattern), v("自行车", "自行車", "单车", "單車", "独轮车", "獨輪車", "滑板", "滑板车", "滑板車", "摩托", "汽车", "汽車", "赛车", "賽車", "坦克", "船", "飞机", "飛機", "火箭", "火车", "火車"))
	dimension := condAny(rv(intelligenceDimensionPattern), v("2d", "2-d", "3d", "3-d", "二维", "二維", "三维", "三維", "立体", "立體", "two-dimensional", "two dimensional", "three-dimensional", "three dimensional"))
	motion := condAny(rv(intelligenceVisualMotionPattern), v("动画", "動畫", "动态", "動態", "运动", "運動", "動起來", "动起来", "跳舞", "跳跃", "跳躍", "弹跳", "彈跳", "行驶", "行駛", "骑行", "騎行", "摇摆", "搖擺", "飞行", "飛行"))
	ui := condAny(rv(intelligenceUIElementPattern), v("图标", "圖標", "按钮", "按鈕", "加载", "載入", "进度条", "進度條", "图表", "圖表", "流程图", "流程圖"))
	scene := condAny(rv(intelligenceNarrativePattern), rv(intelligenceRiderPattern), v("一只", "一隻", "一位", "一名", "一座", "一辆", "一輛", "人物", "角色", "动物", "動物", "猫", "貓", "企鹅", "企鵝", "狐狸", "熊猫", "熊貓", "火烈鸟", "火烈鳥", "海滩", "海灘", "森林", "沙滩", "沙灘", "机器人", "機器人", "宇航员", "宇航員"))
	visual := condAll(svg, create, condAny(condAll(vehicle, rv(intelligenceRiderPattern)), dimension, v("html"), condAll(condAny(motion, rv(intelligenceVisualSceneTaskPattern)), condAny(condNot(ui), scene))))
	thibault := condAll(rv(intelligenceThibaultName), condAny(rv(intelligencePersonQuestion), v("认识", "認識", "知道", "了解", "熟悉", "听过", "聽過", "听说过", "聽說過", "认得", "認得", "见过", "見過")))
	resistors := condAll(rv(intelligenceHundredOhms), rv(intelligenceSixtySevenOhms),
		v("电阻", "電阻", "resistor", "resistance", "欧姆", "歐姆", "ohm", "ω"),
		v("最少", "最小", "至少", "几只", "幾隻", "几个", "幾個", "几颗", "幾顆", "几根", "幾根", "多少个", "多少個", "多少只", "多少隻", "多少颗", "多少顆", "最低", "minimum", "fewest", "least", "how many", "minimize", "minimise"),
		condAny(rv(intelligenceResistorGet), v("串联", "串聯", "并联", "並聯", "串并联", "串並聯", "组合", "組合", "组成", "組成", "获得", "獲得", "得到", "获取", "獲取", "构成", "構成", "实现", "實現", "等效", "凑出", "湊出", "合成", "series", "parallel", "combine", "build", "make", "construct", "obtain", "achieve", "equivalent", "yield", "produce", "reach")))
	return map[string]IntelligenceCondition{
		"builtin.knowledge_iphone": knowledge, "builtin.pelican_bicycle": pelican, "builtin.qin_tank": qin,
		"builtin.candy_named": candyName, "builtin.candy_structure": candy, "builtin.modeltrace_named": traceName,
		"builtin.modeltrace_structure": trace, "builtin.svg_scene": visual,
		"builtin.thibault_sottiaux": thibault, "builtin.resistor_100_to_67": resistors,
	}
}

func prepareIntelligenceCondition(value IntelligenceCondition, depth int, count *int) (IntelligenceCondition, error) {
	*count++
	if depth > 16 || *count > 256 {
		return value, fmt.Errorf("%w: condition is too complex", ErrInvalidIntelligenceRules)
	}
	out := IntelligenceCondition{Op: value.Op, Text: value.Text, Pattern: value.Pattern, Values: append([]string(nil), value.Values...)}
	switch value.Op {
	case "all", "any", "not":
		if len(value.Children) == 0 || (value.Op == "not" && len(value.Children) != 1) || len(value.Children) > 100 || value.Text != "" || value.Pattern != "" || len(value.Values) > 0 {
			return value, fmt.Errorf("%w: invalid condition group", ErrInvalidIntelligenceRules)
		}
		out.Children = make([]IntelligenceCondition, len(value.Children))
		for i, child := range value.Children {
			prepared, err := prepareIntelligenceCondition(child, depth+1, count)
			if err != nil {
				return value, err
			}
			out.Children[i] = prepared
		}
	case "contains_any":
		if !validIntelligenceConditionText(value.Text) || len(value.Values) == 0 || len(value.Values) > 200 || value.Pattern != "" || len(value.Children) > 0 {
			return value, fmt.Errorf("%w: invalid contains condition", ErrInvalidIntelligenceRules)
		}
		out.normalizedValues = make([]string, len(value.Values))
		for i, term := range value.Values {
			if term == "" || len(term) > 4096 {
				return value, fmt.Errorf("%w: invalid condition term", ErrInvalidIntelligenceRules)
			}
			out.normalizedValues[i] = normalizeIntelligenceConditionText(value.Text, term)
			if out.normalizedValues[i] == "" {
				return value, fmt.Errorf("%w: invalid condition term", ErrInvalidIntelligenceRules)
			}
		}
	case "regex":
		if !validIntelligenceConditionText(value.Text) || value.Pattern == "" || len(value.Pattern) > 4096 || len(value.Children) > 0 || len(value.Values) > 0 {
			return value, fmt.Errorf("%w: invalid regex condition", ErrInvalidIntelligenceRules)
		}
		var err error
		out.compiled, err = regexp.Compile(value.Pattern)
		if err != nil {
			return value, fmt.Errorf("%w: invalid condition regex", ErrInvalidIntelligenceRules)
		}
	default:
		return value, fmt.Errorf("%w: unknown condition operation", ErrInvalidIntelligenceRules)
	}
	return out, nil
}

func validIntelligenceConditionText(text string) bool {
	return text == "plain" || text == "qin" || text == "visual"
}

func normalizeIntelligenceConditionText(mode, text string) string {
	switch mode {
	case "qin":
		return strings.NewReplacer("-", " ", "_", " ").Replace(strings.ToLower(text))
	case "visual":
		return normalizeIntelligenceVisualText(text)
	default:
		return strings.ToLower(text)
	}
}

func cloneIntelligenceCondition(value IntelligenceCondition) IntelligenceCondition {
	value.Values = append([]string(nil), value.Values...)
	value.normalizedValues = append([]string(nil), value.normalizedValues...)
	if value.Children != nil {
		children := make([]IntelligenceCondition, len(value.Children))
		for i, child := range value.Children {
			children[i] = cloneIntelligenceCondition(child)
		}
		value.Children = children
	}
	return value
}

func MatchIntelligenceCondition(condition IntelligenceCondition, input string) (bool, error) {
	count := 0
	prepared, err := prepareIntelligenceCondition(condition, 0, &count)
	if err != nil {
		return false, err
	}
	return matchIntelligenceCondition(prepared, input), nil
}

type intelligenceConditionInput struct {
	input       string
	plain       string
	qin         string
	visual      string
	qinReady    bool
	visualReady bool
}

func matchIntelligenceCondition(condition IntelligenceCondition, input string) bool {
	texts := &intelligenceConditionInput{input: input, plain: strings.ToLower(input)}
	return matchIntelligenceConditionNode(condition, texts)
}

func matchIntelligenceConditionNode(condition IntelligenceCondition, texts *intelligenceConditionInput) bool {
	switch condition.Op {
	case "all":
		for _, child := range condition.Children {
			if !matchIntelligenceConditionNode(child, texts) {
				return false
			}
		}
		return true
	case "any":
		for _, child := range condition.Children {
			if matchIntelligenceConditionNode(child, texts) {
				return true
			}
		}
		return false
	case "not":
		if len(condition.Children) != 1 {
			return false
		}
		return !matchIntelligenceConditionNode(condition.Children[0], texts)
	}
	text := texts.plain
	switch condition.Text {
	case "qin":
		if !texts.qinReady {
			texts.qin = strings.NewReplacer("-", " ", "_", " ").Replace(texts.plain)
			texts.qinReady = true
		}
		text = texts.qin
	case "visual":
		if !texts.visualReady {
			texts.visual = normalizeIntelligenceVisualText(texts.input)
			texts.visualReady = true
		}
		text = texts.visual
	}
	if condition.Op == "contains_any" {
		values := condition.normalizedValues
		if len(values) != len(condition.Values) {
			values = make([]string, len(condition.Values))
			for i, value := range condition.Values {
				values[i] = normalizeIntelligenceConditionText(condition.Text, value)
			}
		}
		return intelligenceContainsAny(text, values...)
	}
	compiled := condition.compiled
	if compiled == nil && condition.Op == "regex" {
		compiled, _ = regexp.Compile(condition.Pattern)
	}
	return compiled != nil && compiled.MatchString(text)
}

const maxIntelligenceEvidenceItems = 32
const maxIntelligenceEvidenceRunes = 160

// NormalizeIntelligenceEvidence bounds stored matches and detaches them from
// the full input buffer. Truncation never splits a UTF-8 character.
func NormalizeIntelligenceEvidence(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	count := 0
	for index := range value {
		if count == maxIntelligenceEvidenceRunes {
			return strings.Clone(value[:index]) + "…"
		}
		count++
	}
	return strings.Clone(value)
}

// MergeIntelligenceEvidence keeps the first occurrence of each matched piece.
func MergeIntelligenceEvidence(groups ...[]string) []string {
	var result []string
	seen := make(map[string]bool)
	for _, group := range groups {
		for _, value := range group {
			value = NormalizeIntelligenceEvidence(value)
			if value == "" || seen[value] {
				continue
			}
			seen[value] = true
			result = append(result, value)
			if len(result) >= maxIntelligenceEvidenceItems {
				return result
			}
		}
	}
	return result
}

// MatchIntelligenceConditionEvidence uses the same predicate as routing and
// reports only positive branches that actually match the normalized input.
func MatchIntelligenceConditionEvidence(condition IntelligenceCondition, input string) (bool, []string) {
	texts := &intelligenceConditionInput{input: input, plain: strings.ToLower(input)}
	return matchIntelligenceConditionEvidenceWithTexts(condition, texts)
}

func matchIntelligenceConditionEvidenceWithTexts(condition IntelligenceCondition, texts *intelligenceConditionInput) (bool, []string) {
	if !matchIntelligenceConditionNode(condition, texts) {
		return false, nil
	}
	return true, conditionMatchedEvidence(condition, texts)
}

func conditionMatchedEvidence(condition IntelligenceCondition, texts *intelligenceConditionInput) []string {
	switch condition.Op {
	case "all", "any":
		var result []string
		for _, child := range condition.Children {
			if matchIntelligenceConditionNode(child, texts) {
				result = MergeIntelligenceEvidence(result, conditionMatchedEvidence(child, texts))
				if len(result) >= maxIntelligenceEvidenceItems {
					return result
				}
			}
		}
		return result
	case "not":
		return nil // A successful exclusion has no matching positive keyword.
	}
	text := texts.plain
	switch condition.Text {
	case "qin":
		if !texts.qinReady {
			texts.qin = strings.NewReplacer("-", " ", "_", " ").Replace(texts.plain)
			texts.qinReady = true
		}
		text = texts.qin
	case "visual":
		if !texts.visualReady {
			texts.visual = normalizeIntelligenceVisualText(texts.input)
			texts.visualReady = true
		}
		text = texts.visual
	}
	if condition.Op == "contains_any" {
		values := condition.normalizedValues
		if len(values) != len(condition.Values) {
			values = make([]string, len(condition.Values))
			for i, value := range condition.Values {
				values[i] = normalizeIntelligenceConditionText(condition.Text, value)
			}
		}
		var result []string
		for _, value := range values {
			if value == "" {
				continue
			}
			if index := strings.Index(text, value); index >= 0 {
				result = MergeIntelligenceEvidence(result, []string{text[index : index+len(value)]})
				// One hit explains this any-of group without scanning the
				// remainder of a potentially large request repeatedly.
				break
			}
		}
		return result
	}
	if condition.Op == "regex" {
		compiled := condition.compiled
		if compiled == nil {
			compiled, _ = regexp.Compile(condition.Pattern)
		}
		if compiled != nil {
			return MergeIntelligenceEvidence([]string{compiled.FindString(text)})
		}
	}
	return nil
}

var builtinEvidenceConditions = sync.OnceValue(func() map[string]IntelligenceCondition {
	conditions := DefaultIntelligenceBuiltinConditions()
	for id, condition := range conditions {
		count := 0
		if prepared, err := prepareIntelligenceCondition(condition, 0, &count); err == nil {
			conditions[id] = prepared
		}
	}
	return conditions
})
