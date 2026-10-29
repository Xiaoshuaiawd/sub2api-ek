//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"hash/crc32"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/Wei-Shaw/sub2api/internal/pkg/websearch"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestGatewayCompatibilityPreservesClientModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	anthropicSSE := strings.Join([]string{
		`event:message_start`, `data:{"type":"message_start","message":{"id":"msg_1","type":"message","role":"assistant","content":[],"model":"upstream","usage":{"input_tokens":10}}}`, ``,
		`event:content_block_start`, `data:{"type":"content_block_start","index":0,"content_block":{"type":"text","text":"OK"}}`, ``,
		`event:message_delta`, `data:{"type":"message_delta","delta":{"stop_reason":"end_turn"},"usage":{"output_tokens":3}}`, ``,
		`event:message_stop`, `data:{"type":"message_stop"}`, ``,
	}, "\n")
	for _, protocol := range []string{"chat", "responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", protocol, stream), func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/", nil)
				SetGatewayClientModel(c, "public-alias")
				resp := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(anthropicSSE))}
				svc := &GatewayService{}
				var result *ForwardResult
				var err error
				if protocol == "chat" && stream {
					result, err = svc.handleCCStreamingFromAnthropic(resp, c, "channel-model", "upstream", nil, time.Now())
				} else if protocol == "chat" {
					result, err = svc.handleCCBufferedFromAnthropic(resp, c, "channel-model", "upstream", nil, time.Now())
				} else if stream {
					result, err = svc.handleResponsesStreamingResponse(resp, c, "channel-model", "upstream", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				} else {
					result, err = svc.handleResponsesBufferedStreamingResponse(resp, c, "channel-model", "upstream", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
				}
				require.NoError(t, err)
				require.Equal(t, "channel-model", result.Model)
				require.Equal(t, "upstream", result.UpstreamModel)
				require.Contains(t, rec.Body.String(), `"model":"public-alias"`)
				require.NotContains(t, rec.Body.String(), `"model":"channel-model"`)
			})
		}
	}
}

func TestAntigravityCompatibilityPreservesClientModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"chat", "responses"} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/stream=%v", protocol, stream), func(t *testing.T) {
				upstream := &queuedHTTPUpstreamStub{responses: []*http.Response{antigravityCompatSuccessResponse()}}
				svc := newAntigravityCompatService(config.GatewayConfig{MaxLineSize: defaultMaxLineSize}, upstream)
				body := []byte(fmt.Sprintf(`{"model":"gemini-3.1-pro-high","messages":[{"role":"user","content":"ok"}],"input":"ok","stream":%v}`, stream))
				c, rec := newAntigravityCompatContext(http.MethodPost, "/", body)
				SetGatewayClientModel(c, "public-alias")
				var result *ForwardResult
				var err error
				if protocol == "chat" {
					result, err = svc.ForwardAsChatCompletions(context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil)
				} else {
					result, err = svc.ForwardAsResponses(context.Background(), c, newAntigravityCompatAccount(AccountTypeOAuth), body, nil)
				}
				require.NoError(t, err)
				require.Equal(t, "gemini-3.1-pro-high", result.Model)
				require.Equal(t, "gemini-3.1-pro-high", gjson.GetBytes(upstream.requestBodies[0], "model").String())
				require.Contains(t, rec.Body.String(), `"model":"public-alias"`)
				require.NotContains(t, rec.Body.String(), `"model":"gemini-3.1-pro-high"`)
			})
		}
	}
}

func TestGeminiCompatibilityPreservesClientModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const response = `{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3}}`
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(nil))
			SetGatewayClientModel(c, "public-alias")
			payload := response
			if stream {
				payload = "data: " + response + "\n\n"
			}
			resp := &http.Response{Header: make(http.Header), Body: io.NopCloser(strings.NewReader(payload))}
			svc := &GeminiMessagesCompatService{cfg: &config.Config{}}
			var err error
			if stream {
				_, err = svc.handleChatCompletionsStreamingResponseFromGemini(c, resp, time.Now(), "channel-model", false, false)
			} else {
				_, err = svc.handleChatCompletionsNonStreamingResponseFromGemini(c, resp, "channel-model", false)
			}
			require.NoError(t, err)
			require.Contains(t, rec.Body.String(), `"model":"public-alias"`)
			require.NotContains(t, rec.Body.String(), `"model":"channel-model"`)
		})
	}
}

func TestGatewayCompatUpstreamErrorsHideMappedModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, protocol := range []string{"chat", "responses"} {
		for _, test := range []struct {
			name, message string
			wantStatus    int
			wantText      string
		}{
			{"leaked alias", "upstream model xy rejected", http.StatusBadGateway, UpstreamModelLeakClientMessage},
			{"ordinary error", "quota exceeded", http.StatusForbidden, "quota exceeded"},
		} {
			t.Run(protocol+"/"+test.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Set("ops_model", "client-model")
				SetOpsUpstreamModel(c, "xy")
				if protocol == "chat" {
					writeGatewayCCError(c, http.StatusForbidden, "permission_error", test.message)
				} else {
					writeGatewayResponsesUpstreamError(c, http.StatusForbidden, "permission_error", test.message)
				}
				require.Equal(t, test.wantStatus, rec.Code)
				require.Contains(t, rec.Body.String(), test.wantText)
				if test.name == "leaked alias" {
					require.NotContains(t, rec.Body.String(), "xy")
				}
			})
		}
	}
}

func TestGatewayMessagesPreservesClientModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const jsonBody = `{"id":"msg","type":"message","model":"account-upstream","content":[{"type":"text","text":"ok"}],"usage":{"input_tokens":5,"output_tokens":3}}`
	const streamBody = "event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"model\":\"account-upstream\",\"usage\":{\"input_tokens\":5}}}\n\nevent: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"
	for _, passthrough := range []bool{false, true} {
		for _, stream := range []bool{false, true} {
			t.Run(fmt.Sprintf("passthrough=%v/stream=%v", passthrough, stream), func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
				SetGatewayClientModel(c, "public-alias")
				payload := jsonBody
				if stream {
					payload = streamBody
				}
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload))}
				svc := newMinimalGatewayService()
				var err error
				if passthrough && stream {
					_, err = svc.handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "account-upstream")
				} else if passthrough {
					_, err = svc.handleNonStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1})
				} else if stream {
					_, err = svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "channel-model", "account-upstream", false)
				} else {
					_, err = svc.handleNonStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, "channel-model", "account-upstream")
				}
				require.NoError(t, err)
				require.Contains(t, rec.Body.String(), `"model":"public-alias"`)
				require.NotContains(t, rec.Body.String(), `"model":"account-upstream"`)
			})
		}
	}
}

func TestGeminiMessagesPreservesClientModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	const upstream = `{"response":{"candidates":[{"content":{"parts":[{"text":"ok"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":8,"candidatesTokenCount":3}}}`
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			SetGatewayClientModel(c, "public-alias")
			payload := upstream
			if stream {
				payload = "data: " + upstream + "\n\n"
			}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(payload))}
			svc := &GeminiMessagesCompatService{cfg: &config.Config{}}
			var err error
			if stream {
				_, err = svc.handleStreamingResponse(c, resp, time.Now(), "channel-model")
			} else {
				_, err = svc.handleNonStreamingResponse(c, resp, "channel-model")
			}
			require.NoError(t, err)
			require.Contains(t, rec.Body.String(), `"model":"public-alias"`)
			require.NotContains(t, rec.Body.String(), `"model":"channel-model"`)
		})
	}
}

func TestAntigravityMessagesPreservesClientModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			SetGatewayClientModel(c, "public-alias")
			svc := newAntigravityTestService(&config.Config{Gateway: config.GatewayConfig{MaxLineSize: defaultMaxLineSize}})
			resp := antigravityCompatSuccessResponse()
			var err error
			if stream {
				_, err = svc.handleClaudeStreamingResponse(c, resp, time.Now(), "channel-model")
			} else {
				_, err = svc.handleClaudeStreamToNonStreaming(c, resp, time.Now(), "channel-model")
			}
			require.NoError(t, err)
			require.Contains(t, rec.Body.String(), `"model":"public-alias"`)
			require.NotContains(t, rec.Body.String(), `"model":"channel-model"`)
		})
	}
}

func TestBedrockMessagesPreservesClientModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			SetGatewayClientModel(c, "public-alias")
			payload := []byte(`{"type":"message","model":"account-upstream","content":[],"usage":{"input_tokens":5,"output_tokens":3}}`)
			if stream {
				chunk := base64.StdEncoding.EncodeToString([]byte(`{"type":"message_start","message":{"model":"account-upstream","usage":{"input_tokens":5}}}`))
				body := []byte(`{"bytes":"` + chunk + `"}`)
				var header bytes.Buffer
				require.NoError(t, header.WriteByte(byte(len(":event-type"))))
				_, err := header.WriteString(":event-type")
				require.NoError(t, err)
				require.NoError(t, header.WriteByte(7))
				require.NoError(t, binary.Write(&header, binary.BigEndian, uint16(len("chunk"))))
				_, err = header.WriteString("chunk")
				require.NoError(t, err)
				var frame bytes.Buffer
				require.NoError(t, binary.Write(&frame, binary.BigEndian, uint32(12+header.Len()+len(body)+4)))
				require.NoError(t, binary.Write(&frame, binary.BigEndian, uint32(header.Len())))
				require.NoError(t, binary.Write(&frame, binary.BigEndian, crc32.ChecksumIEEE(frame.Bytes())))
				_, err = frame.Write(header.Bytes())
				require.NoError(t, err)
				_, err = frame.Write(body)
				require.NoError(t, err)
				require.NoError(t, binary.Write(&frame, binary.BigEndian, crc32.ChecksumIEEE(frame.Bytes())))
				payload = frame.Bytes()
			}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(bytes.NewReader(payload))}
			svc := newMinimalGatewayService()
			var err error
			if stream {
				_, err = svc.handleBedrockStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "channel-model")
			} else {
				_, err = svc.handleBedrockNonStreamingResponse(context.Background(), resp, c, &Account{ID: 1})
			}
			require.NoError(t, err)
			require.Contains(t, rec.Body.String(), `"model":"public-alias"`)
			require.NotContains(t, rec.Body.String(), `"model":"account-upstream"`)
		})
	}
}

func TestWebSearchMessagesPreservesClientModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			SetGatewayClientModel(c, "public-alias")
			var result *ForwardResult
			var err error
			if stream {
				result, err = writeWebSearchStreamResponse(c, "query", &websearch.SearchResponse{}, "channel-model", time.Now())
			} else {
				result, err = writeWebSearchNonStreamResponse(c, "query", &websearch.SearchResponse{}, "channel-model", time.Now())
			}
			require.NoError(t, err)
			require.Equal(t, "channel-model", result.Model)
			require.Contains(t, rec.Body.String(), `"model":"public-alias"`)
			require.NotContains(t, rec.Body.String(), `"model":"channel-model"`)
		})
	}
}
