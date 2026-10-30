package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/modeltrace"
	"github.com/stretchr/testify/require"
)

func TestIntelligenceBuiltinMatches(t *testing.T) {
	definitions := IntelligenceBuiltinRules()
	require.Len(t, definitions, 8)
	ids := map[string]bool{}
	for _, def := range definitions {
		require.NotEmpty(t, def.ID)
		require.NotEmpty(t, def.Name)
		require.NotEmpty(t, def.Description)
		require.False(t, ids[def.ID])
		ids[def.ID] = true
	}
	matches := MatchIntelligenceBuiltins("请用 SVG 制作秦始皇开坦克的 2D 动画")
	got := map[string]bool{}
	for _, item := range matches {
		got[item.ID] = true
		require.NotEmpty(t, item.Name)
	}
	require.True(t, got["builtin.qin_tank"])
	require.True(t, got["builtin.svg_scene"])
	require.Empty(t, MatchIntelligenceBuiltins("今天的天气不错"))
	require.Len(t, MergeIntelligenceRuleMatches(matches, matches), len(matches))
	filtered := IntelligenceRules{BuiltinDisabledIDs: []string{"builtin.qin_tank"}}.MatchAll("请用 SVG 制作秦始皇开坦克的 2D 动画")
	require.Len(t, filtered, 1)
	require.Equal(t, "builtin.svg_scene", filtered[0].ID)
}

func TestIntelligenceBuiltinMatchAttribution(t *testing.T) {
	for _, tc := range []struct{ text, id string }{
		{"你的知识库截止日期是什么？最新 iPhone 型号是什么？", "builtin.knowledge_iphone"},
		{"画鹈鹕骑自行车", "builtin.pelican_bicycle"},
		{"画秦始皇开坦克", "builtin.qin_tank"},
		{"糖果测试", "builtin.candy_named"},
		{"圆形糖果最少抽取多少", "builtin.candy_structure"},
		{"ModelTrace 指纹", "builtin.modeltrace_named"},
		{"不要用工具，在 1 到 1000 之间生成 300 个整数", "builtin.modeltrace_structure"},
		{"用 SVG 绘制三维海底城市", "builtin.svg_scene"},
	} {
		t.Run(tc.id, func(t *testing.T) {
			matches := MatchIntelligenceBuiltins(tc.text)
			ids := make([]string, 0, len(matches))
			for _, match := range matches {
				ids = append(ids, match.ID)
			}
			require.Contains(t, ids, tc.id)
		})
	}
}

func TestIntelligenceCustomMatches(t *testing.T) {
	rules := IntelligenceRules{Rules: []IntelligenceRule{
		{Name: "甜食", Type: "contains", Pattern: "糖果", Enabled: true},
		{Name: "数量", Type: "regex", Pattern: `糖果.*数量`, Enabled: true},
		{Name: "关闭", Type: "contains", Pattern: "糖果", Enabled: false},
	}}
	matches := rules.Matches("请计算糖果数量")
	require.Len(t, matches, 2)
	require.Equal(t, "甜食", matches[0].Name)
	require.Equal(t, "数量", matches[1].Name)
	require.NotEqual(t, matches[0].ID, matches[1].ID)
	require.True(t, rules.Match("请计算糖果数量"))
	require.Empty(t, rules.Matches("普通消息"))
}

func TestIsIntelligenceTestRequest(t *testing.T) {
	tests := []struct {
		name string
		body string
		want bool
	}{
		{"candy built in", `{"messages":[{"role":"user","content":"袋子里有苹果味和桃子味糖果，有圆形和星形，最少摸多少颗才能保证不同形状？"}]}`, true},
		{"candy named", `{"input":"来一道糖果测试"}`, true},
		{"candy english", `{"input":"A bag has apple and peach candies of different shapes. What is the minimum number to draw to guarantee a matching pair?"}`, true},
		{"pelican split content", `{"messages":[{"role":"user","content":[{"type":"text","text":"画一只鹈鹕"},{"type":"text","text":"骑着自行车的动画"}]}]}`, true},
		{"pelican english", `{"contents":[{"role":"user","parts":[{"text":"Animate a PELICAN riding a bicycle"}]}]}`, true},
		{"qin svg chinese", `{"messages":[{"role":"user","content":"请用 HTML 和 SVG 制作秦始皇开着坦克的 2D 动画"}]}`, true},
		{"qin traditional split", `{"contents":[{"role":"user","parts":[{"text":"以 SVG 繪製嬴政"},{"text":"駕駛一輛坦克的二維動畫"}]}]}`, true},
		{"qin english", `{"input":"Create a 2D SVG animation of Qin Shi Huang driving a tank."}`, true},
		{"qin english variation", `{"prompt":"Illustrate the First Emperor of China in an armoured vehicle."}`, true},
		{"qin english driving variation", `{"input":"Qin-Shi-Huang rides a tank"}`, true},
		{"qin relation without format", `{"input":"画面里秦始皇开坦克"}`, true},
		{"qin websocket", `{"type":"response.create","response":{"input":[{"role":"user","content":"Draw Ying Zheng driving a TANK"}]}}`, true},
		{"qin history discussion", `{"input":"比较秦始皇时代与现代坦克的发展历史"}`, false},
		{"generic svg tank animation", `{"input":"用 SVG 绘制一辆坦克的 2D 动画"}`, true},
		{"qin carriage animation", `{"input":"用 SVG 绘制秦始皇乘坐马车的 2D 动画"}`, true},
		{"qin unrelated tankard", `{"input":"Illustrate Qin Shi Huang holding a tankard"}`, false},
		{"qin assistant history", `{"messages":[{"role":"assistant","content":"秦始皇开坦克的 SVG 动画"},{"role":"user","content":"你好"}]}`, false},
		{"svg capybara scooter", `{"input":"Generate an SVG of a capybara riding a scooter."}`, true},
		{"svg octopus unicycle", `{"input":"Draw an SVG of an octopus riding a unicycle."}`, true},
		{"svg horse racecar", `{"input":"Make an SVG of a horse sitting in a race car."}`, true},
		{"svg chinese skateboard", `{"input":"请用SVG画一只企鹅骑着滑板车"}`, true},
		{"svg chinese named rider", `{"input":"用 SVG 绘制爱因斯坦骑单车"}`, true},
		{"svg english named rider", `{"input":"Create an SVG of Einstein riding a bicycle"}`, true},
		{"svg zero width variation", `{"input":"用 S​V​G 画一只熊猫跳舞的２Ｄ动画"}`, true},
		{"svg developer and responses user", `{"input":[{"role":"developer","content":"Return a self-contained HTML page using SVG."},{"role":"user","content":"Create a 2D animation of a robot playing piano."}]}`, false},
		{"svg gemini system and user", `{"systemInstruction":{"parts":[{"text":"使用 SVG 输出完整 HTML"}]},"contents":[{"role":"user","parts":[{"text":"生成一只青蛙吹萨克斯的二维动画"}]}]}`, false},
		{"ordinary vehicle tutorial", `{"input":"解释如何骑滑板车，以及如何用 SVG 画一张静态城市地图"}`, false},
		{"ordinary svg vehicle icon", `{"input":"画一个滑板车 SVG 图标"}`, false},
		{"svg animation arbitrary scene", `{"messages":[{"role":"user","content":"创建独立 HTML，用 SVG 绘制一个弹吉他的宇航员的二维动画"}]}`, true},
		{"svg animated flamingo", `{"input":"Generate an animation of a flamingo doing the moonwalk on a beach as SVG code"}`, true},
		{"svg fullwidth", `{"input":"请用ＳＶＧ制作一只熊猫跳舞的２Ｄ动画"}`, true},
		{"svg split message blocks", `{"contents":[{"role":"user","parts":[{"text":"用 SVG 生成"},{"text":"一只狐狸弹钢琴的 2D 动画"}]}]}`, true},
		{"svg system and current user", `{"system":"请用 SVG 输出独立 HTML","messages":[{"role":"user","content":"画一只猫跳舞的二维动画"}]}`, false},
		{"svg current user without system creation", `{"system":"SVG 只是我们的格式要求。","messages":[{"role":"user","content":"生成一个企鹅滑雪的 2D 动画"}]}`, false},
		{"svg static 2d chart also matches", `{"input":"请用 SVG 生成一张二维柱状图"}`, true},
		{"svg spinner component", `{"input":"请用 SVG 制作一个旋转加载图标"}`, false},
		{"svg ordinary css", `{"input":"用 HTML 和 CSS 编写一个 2D 加载动画"}`, false},
		{"svg ordinary 2d animation also matches", `{"input":"用 SVG 制作一个 2D 加载动画"}`, true},
		{"svg html 2d static scene", `{"input":"用 HTML 和 SVG 画一张 2D 图：宇航员坐在月球上"}`, true},
		{"svg html 3d static scene", `{"input":"使用 HTML + SVG 绘制一个三维宇宙飞船"}`, true},
		{"svg 3d animated scene", `{"input":"Create a 3-D SVG animation of a fox flying through space"}`, true},
		{"svg 3 d static scene", `{"input":"Draw a 3 D SVG of a mountain"}`, true},
		{"svg two point five dimensional scene", `{"input":"Draw a 2.5D SVG of a mountain"}`, true},
		{"svg html static without dimension", `{"input":"Write HTML with an SVG drawing of a wizard and a dragon"}`, true},
		{"svg named rider no article", `{"input":"Draw SVG Batman riding a bicycle"}`, true},
		{"svg passive rider", `{"input":"Generate SVG of a bicycle ridden by a dragon"}`, true},
		{"svg rider with comma", `{"input":"Draw an SVG of a capybara, happily riding a scooter"}`, true},
		{"svg named subject animation", `{"input":"Generate an SVG animation of a dog jumping"}`, true},
		{"svg application spinner without dimensions", `{"input":"Generate SVG animation for an application loading spinner"}`, false},
		{"svg system format with unrelated user", `{"system":"用 SVG 制作一个 2D 加载动画","messages":[{"role":"user","content":"解释 SQL 索引"}]}`, false},
		{"svg system cat topic not task", `{"system":"你是专门回答猫问题的助手，请用 SVG 和 HTML 输出回答。","messages":[{"role":"user","content":"解释 SQL 索引"}]}`, false},
		{"svg system format and graph theory", `{"system":"请用 SVG 输出 HTML","messages":[{"role":"user","content":"解释图论的最短路径"}]}`, false},
		{"svg separate embedding inputs", `{"input":["Generate an SVG icon", "Learn HTML and 2D geometry"]}`, false},
		{"svg separate embedding actual match", `{"input":["你好", "用 SVG 绘制 3D 火星城市"]}`, true},
		{"svg static narrative", `{"input":"用 SVG 绘制一只宇航员坐在月球的静态插画"}`, true},
		{"svg system scene as task", `{"system":"制作一个 SVG 3D HTML 图，画一只猫在花园里","messages":[{"role":"user","content":"执行"}]}`, false},
		{"svg system complete scene with output", `{"system":"请输出一只跳舞的猫的 SVG 2D 动画，生成完整 HTML","messages":[{"role":"user","content":"执行"}]}`, false},
		{"svg system format and user scene", `{"system":"用 SVG 输出 HTML 3D 图形","messages":[{"role":"user","content":"一座火山"}]}`, false},
		{"svg system user chinese driving", `{"system":"请用 SVG 输出独立 HTML 页面","messages":[{"role":"user","content":"孙悟空开坦克"}]}`, false},
		{"svg system user english riding", `{"system":"Return SVG inside standalone HTML","messages":[{"role":"user","content":"Batman on a skateboard"}]}`, false},
		{"svg english output instruction", `{"system":"Output SVG in HTML","input":"a cat playing piano in 3D"}`, false},
		{"svg system user component creation", `{"instructions":"Use SVG in HTML","input":"Make a loading spinner"}`, false},
		{"svg websocket nested instructions", `{"type":"response.create","response":{"instructions":"Use SVG in HTML for 3D graphics","input":"Draw a dragon"}}`, false},
		{"svg static svg icon remains ordinary", `{"input":"画一个 SVG 图标"}`, false},
		{"svg english token boundary", `{"input":"Generate SVG animation for an education app loading spinner"}`, false},
		{"svg system object not user", `{"input":{"role":"system","content":"用 SVG 制作一个 2D 加载动画"},"prompt":"介绍 SQL"}`, false},
		{"latest user ignores prior candy", `{"messages":[{"role":"user","content":"来一道糖果测试"},{"role":"assistant","content":"好的"},{"role":"user","content":"你好"}]}`, false},
		{"latest user detects candy", `{"messages":[{"role":"user","content":"你好"},{"role":"assistant","content":"好的"},{"role":"user","content":"来一道糖果测试"}]}`, true},
		{"latest user ignores prior pelican", `{"messages":[{"role":"user","content":"画鹈鹕骑自行车"},{"role":"assistant","content":"好的"},{"role":"user","content":"你好"}]}`, false},
		{"latest user ignores prior modeltrace", `{"contents":[{"role":"user","parts":[{"text":"ModelTrace 指纹测试"}]},{"role":"model","parts":[{"text":"ok"}]},{"role":"user","parts":[{"text":"hello"}]}]}`, false},
		{"latest user ignores previous embedding item", `{"input":["生成鹈鹕骑自行车", "普通内容"]}`, false},
		{"latest user with assistant afterwards", `{"input":[{"role":"user","content":"画鹈鹕骑自行车"},{"role":"assistant","content":"完成"}]}`, true},
		{"latest user empty text blocks", `{"messages":[{"role":"user","content":"画鹈鹕骑自行车"},{"role":"user","content":[]}]}`, false},
		{"latest user nested ws", `{"type":"response.create","response":{"input":[{"role":"user","content":"画鹈鹕骑自行车"},{"role":"user","content":"hello"}]}}`, false},
		{"system one state overrides question", `{"state":"普通内容","questions":{"q":{"type":"noul","instructions":"画鹈鹕骑自行车"}}}`, false},
		{"system one nested conversation history", `{"state":{"context":[{"role":"user","content":"画鹈鹕骑自行车"},{"role":"assistant","content":"好的"},{"role":"user","content":"你好"}]},"questions":{"q":{"type":"noul"}}}`, false},
		{"responses top level text blocks", `{"input":[{"type":"input_text","text":"画鹈鹕"},{"type":"input_text","text":"骑自行车"}]}`, true},
		{"responses image follows previous message", `{"input":[{"role":"user","content":"画鹈鹕骑自行车"},{"role":"assistant","content":"完成"},{"type":"input_image","image_url":"https://example.test/a.png"}]}`, false},
		{"responses text split by image", `{"input":[{"type":"input_text","text":"画鹈鹕"},{"type":"input_image","image_url":"https://example.test/a.png"},{"type":"input_text","text":"骑自行车"}]}`, true},
		{"latest user multimodal without text", `{"messages":[{"role":"user","content":"画鹈鹕骑自行车"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.test/a"}}]}]}`, false},
		{"svg historical full user test", `{"messages":[{"role":"user","content":"用 SVG 绘制企鹅滑雪的 2D 动画"},{"role":"assistant","content":"完成"},{"role":"user","content":"你好"}]}`, false},
		{"svg assistant history excluded", `{"messages":[{"role":"assistant","content":"用 SVG 画二维狐狸跳舞动画"},{"role":"user","content":"你好"}]}`, false},
		{"svg metadata excluded", `{"metadata":{"note":"SVG 二维动画，画企鹅滑雪"},"messages":[{"role":"user","content":"你好"}]}`, false},
		{"svg earlier user not combined", `{"system":"SVG 输出格式","messages":[{"role":"user","content":"做二维动画"},{"role":"assistant","content":"完成"},{"role":"user","content":"今天的天气如何？"}]}`, false},
		{"system one state", `{"model":"jev-latest","state":{"context":[{"text":"鹈鹕骑自行车"}]},"questions":{"q":{"type":"noul","instructions":"check"}}}`, true},
		{"system one split state", `{"state":{"context":[{"text":"鹈鹕"},{"text":"骑自行车"}]},"questions":{"q":{"type":"noul"}}}`, true},
		{"system one question", `{"state":"context","questions":{"q":{"type":"noul","instructions":"画一只鹈鹕骑自行车"}}}`, false},
		{"system one split scene and format", `{"state":"孙悟空开坦克的二维动画","questions":{"q":{"type":"noul","instructions":"请使用 SVG 输出"}}}`, false},
		{"system one format in state", `{"state":"请使用 SVG 输出 HTML","questions":{"q":{"type":"noul","instructions":"画一座三维未来城市"}}}`, true},
		{"system one unrelated questions", `{"state":"解释 SQL","questions":{"a":{"type":"noul","instructions":"使用 SVG 图标"},"b":{"type":"noul","instructions":"解释 2D 图论"}}}`, false},
		{"websocket nested response", `{"type":"response.create","response":{"input":[{"role":"user","content":"画一只鹈鹕骑自行车"}]}}`, true},
		{"system instruction", `{"system":"画一只鹈鹕骑自行车","messages":[{"role":"user","content":"你好"}]}`, false},
		{"developer message", `{"input":[{"role":"developer","content":"画一只鹈鹕骑自行车"},{"role":"user","content":"执行"}]}`, false},
		{"gemini system instruction", `{"systemInstruction":{"parts":[{"text":"画一只鹈鹕骑自行车"}]},"contents":[{"role":"user","parts":[{"text":"执行"}]}]}`, false},
		{"second user turn", `{"messages":[{"role":"user","content":"你好"},{"role":"assistant","content":"好"},{"role":"user","content":"画一只鹈鹕骑自行车"}]}`, true},
		{"knowledge cutoff and iphone", `{"messages":[{"role":"user","content":"你的知识库截止日期是什么时候，不要调用任何工具或者联网，用知识库回答我.还有iphone最新型号出到多少了"}]}`, true},
		{"knowledge cutoff multiline", `{"input":"你的知识库截止日期是什么时候，不要调用任何工具或者联网，用知识库回答我。\n还有 iPhone 最新型号出到多少了"}`, true},
		{"knowledge cutoff variation", `{"contents":[{"role":"user","parts":[{"text":"你的训练数据更新到哪年？"},{"text":"不要搜索，苹果手机最新款是哪一代？"}]}]}`, true},
		{"knowledge cutoff english", `{"input":"What is your knowledge cutoff date? Without browsing or tools, what is the latest iPhone model?"}`, true},
		{"knowledge cutoff only", `{"input":"你的知识库截止日期是什么时候？"}`, false},
		{"iphone question only", `{"input":"最新的 iPhone 是什么型号？"}`, false},
		{"knowledge and phone historical", `{"messages":[{"role":"user","content":"你的知识库截止时间和最新 iPhone 型号是什么"},{"role":"assistant","content":"好的"},{"role":"user","content":"你好"}]}`, false},
		{"modeltrace explicit", `{"input":[{"role":"user","content":"请进行 ModelTrace 指纹测试"}]}`, true},
		{"modeltrace variation", `{"prompt":"随机生成300个1-355之间的整数，用第一反应选择，不要使用代码或工具"}`, true},
		{"modeltrace english range variation", `{"input":"Choose 300 random integers between 1 and 1000, without using any tools or code."}`, true},
		{"irrelevant metadata and tool", `{"model":"pelican-bicycle","tools":[{"description":"鹈鹕骑自行车"}],"metadata":{"note":"ModelTrace 指纹"},"messages":[{"role":"user","content":"你好"}]}`, false},
		{"assistant history", `{"messages":[{"role":"assistant","content":"鹈鹕骑自行车"},{"role":"user","content":"请继续"}]}`, false},
		{"ordinary candies", `{"messages":[{"role":"user","content":"给我推荐几种糖果口味"}]}`, false},
		{"ordinary random numbers", `{"input":"生成 5 个随机整数，范围 1 到 355"}`, false},
		{"small number without tools", `{"input":"生成 5 个随机整数，范围 1 到 355，不使用工具"}`, false},
		{"malformed", `{`, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want, IsIntelligenceTestRequest([]byte(tt.body)))
			var request map[string]any
			if json.Unmarshal([]byte(tt.body), &request) == nil {
				require.Equal(t, tt.want, len((IntelligenceRules{}).MatchAll(intelligenceLatestUserText(request))) > 0, "default rules must match existing detection")
			}
		})
	}
}

func TestIsIntelligenceTestRequestBuiltinModelTrace(t *testing.T) {
	for i := 0; i < 20; i++ {
		challenges, err := modeltrace.Challenges()
		require.NoError(t, err)
		for _, challenge := range challenges {
			body, err := json.Marshal(map[string]any{"messages": []any{map[string]any{"role": "user", "content": challenge.Prompt}}})
			require.NoError(t, err)
			require.True(t, IsIntelligenceTestRequest(body), challenge.Prompt)
		}
	}
}
