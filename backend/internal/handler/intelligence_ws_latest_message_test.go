//go:build unit

package handler

import (
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	coderws "github.com/coder/websocket"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestIntelligenceWSUsesLatestUserMessage(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		t.Run(mode+"/ordinary_latest", func(t *testing.T) {
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:   `{"type":"response.create","model":"public-model","input":"hello"}`,
				secondPayload:  `{"type":"response.create","model":"public-model","input":[{"role":"user","content":"画鹈鹕骑自行车"},{"role":"assistant","content":"完成"},{"role":"user","content":"hello again"}]}`,
				ingressMode:    mode,
				channelMapping: map[string]string{"public-model": "channel-model"},
			})
			require.Len(t, got.upstreamPayloads, 2)
			for _, payload := range got.upstreamPayloads {
				require.Equal(t, "channel-model", gjson.GetBytes(payload, "model").String())
			}
		})
		t.Run(mode+"/ordinary_to_test_reconnect", func(t *testing.T) {
			runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:            `{"type":"response.create","model":"public-model","input":"hello"}`,
				secondPayload:           `{"type":"response.create","model":"public-model","input":[{"role":"user","content":"hello"},{"role":"assistant","content":"hi"},{"role":"user","content":"画鹈鹕骑自行车"}]}`,
				ingressMode:             mode,
				channelMapping:          map[string]string{"public-model": "channel-model"},
				secondTurnCloseExpected: true,
				closeStatus:             coderws.StatusTryAgainLater,
				closeReason:             "account eligibility changed",
			})
		})
		t.Run(mode+"/test_to_ordinary_reconnect", func(t *testing.T) {
			runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:            `{"type":"response.create","model":"public-model","input":"画鹈鹕骑自行车"}`,
				secondPayload:           `{"type":"response.create","model":"public-model","input":[{"role":"user","content":"画鹈鹕骑自行车"},{"role":"assistant","content":"完成"},{"role":"user","content":"hello again"}]}`,
				ingressMode:             mode,
				intelligenceAccount:     true,
				channelMapping:          map[string]string{"public-model": "channel-model"},
				secondTurnCloseExpected: true,
				closeStatus:             coderws.StatusTryAgainLater,
				closeReason:             "account eligibility changed",
			})
		})
	}
}
