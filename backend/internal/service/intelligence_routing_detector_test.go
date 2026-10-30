package service

import (
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/pkg/modeltrace"
	"github.com/stretchr/testify/require"
)

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
		{"unrelated svg animation", `{"input":"用 SVG 绘制一辆坦克的 2D 动画"}`, false},
		{"qin unrelated vehicle", `{"input":"用 SVG 绘制秦始皇乘坐马车的 2D 动画"}`, false},
		{"qin unrelated tankard", `{"input":"Illustrate Qin Shi Huang holding a tankard"}`, false},
		{"qin assistant history", `{"messages":[{"role":"assistant","content":"秦始皇开坦克的 SVG 动画"},{"role":"user","content":"你好"}]}`, false},
		{"system one state", `{"model":"jev-latest","state":{"context":[{"text":"鹈鹕骑自行车"}]},"questions":{"q":{"type":"noul","instructions":"check"}}}`, true},
		{"system one split state", `{"state":{"context":[{"text":"鹈鹕"},{"text":"骑自行车"}]},"questions":{"q":{"type":"noul"}}}`, true},
		{"system one question", `{"state":"context","questions":{"q":{"type":"noul","instructions":"画一只鹈鹕骑自行车"}}}`, true},
		{"websocket nested response", `{"type":"response.create","response":{"input":[{"role":"user","content":"画一只鹈鹕骑自行车"}]}}`, true},
		{"system instruction", `{"system":"画一只鹈鹕骑自行车","messages":[{"role":"user","content":"你好"}]}`, true},
		{"developer message", `{"input":[{"role":"developer","content":"画一只鹈鹕骑自行车"},{"role":"user","content":"执行"}]}`, true},
		{"gemini system instruction", `{"systemInstruction":{"parts":[{"text":"画一只鹈鹕骑自行车"}]},"contents":[{"role":"user","parts":[{"text":"执行"}]}]}`, true},
		{"second user turn", `{"messages":[{"role":"user","content":"你好"},{"role":"assistant","content":"好"},{"role":"user","content":"画一只鹈鹕骑自行车"}]}`, true},
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
		t.Run(tt.name, func(t *testing.T) { require.Equal(t, tt.want, IsIntelligenceTestRequest([]byte(tt.body))) })
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
