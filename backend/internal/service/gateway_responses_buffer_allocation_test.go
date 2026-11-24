//go:build unit

package service

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func largeBufferedAnthropicWire(kind, content string, fragmentBytes int) string {
	var wire strings.Builder
	write := func(event string, payload any) {
		encoded, err := json.Marshal(payload)
		if err != nil {
			panic(err)
		}
		wire.WriteString("event: " + event + "\ndata: ")
		wire.Write(encoded)
		wire.WriteString("\n\n")
	}
	write("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": "msg_buffer", "role": "assistant", "model": "claude-fixture", "content": []any{},
		"usage": map[string]int{"input_tokens": 17, "output_tokens": 0},
	}})
	block := map[string]any{"type": "text", "text": "seed:"}
	deltaType, deltaField := "text_delta", "text"
	stopReason := "end_turn"
	if kind == "tool" {
		block = map[string]any{"type": "tool_use", "id": "call_buffer", "name": "exec", "input": map[string]any{}}
		deltaType, deltaField, stopReason = "input_json_delta", "partial_json", "tool_use"
	}
	write("content_block_start", map[string]any{"type": "content_block_start", "index": 0, "content_block": block})
	for offset := 0; offset < len(content); offset += fragmentBytes {
		write("content_block_delta", map[string]any{"type": "content_block_delta", "index": 0,
			"delta": map[string]string{"type": deltaType, deltaField: content[offset:min(offset+fragmentBytes, len(content))]},
		})
	}
	write("content_block_stop", map[string]any{"type": "content_block_stop", "index": 0})
	write("message_delta", map[string]any{"type": "message_delta", "delta": map[string]string{"stop_reason": stopReason}, "usage": map[string]int{"output_tokens": 3}})
	write("message_stop", map[string]any{"type": "message_stop"})
	return wire.String()
}

func TestResponsesBufferedStreamingAllocationIsLinear(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, kind := range []string{"text", "tool"} {
		t.Run(kind, func(t *testing.T) {
			content := strings.Repeat("x", 256<<10)
			if kind == "tool" {
				content = `{"source":"` + content + `"}`
			}
			wire := largeBufferedAnthropicWire(kind, content, 256)
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}
			runtime.GC()
			var before, after runtime.MemStats
			runtime.ReadMemStats(&before)
			result, err := (&GatewayService{}).handleResponsesBufferedStreamingResponse(resp, c, "claude-fixture", "claude-fixture", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
			runtime.ReadMemStats(&after)
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 17, result.Usage.InputTokens)
			require.Equal(t, 3, result.Usage.OutputTokens)
			if kind == "text" {
				require.Equal(t, "seed:"+content, gjson.GetBytes(rec.Body.Bytes(), "output.0.content.0.text").String())
			} else {
				require.Equal(t, content, gjson.GetBytes(rec.Body.Bytes(), "output.0.arguments").String())
			}
			require.Less(t, after.TotalAlloc-before.TotalAlloc, uint64(32<<20), "buffered conversion must not allocate a copy of every growing content prefix")
		})
	}
}
