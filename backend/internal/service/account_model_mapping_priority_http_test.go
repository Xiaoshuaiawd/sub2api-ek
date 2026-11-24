//go:build unit

package service

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/openai"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

const priorityHTTPSource = "gpt-6.1-sol"
const priorityHTTPChannel = "terra"
const priorityHTTPTarget = "luna"

func priorityHTTPResponse(protocol, model string, stream bool) *http.Response {
	var payload string
	contentType := "application/json"
	switch protocol {
	case "anthropic":
		if stream {
			payload = strings.ReplaceAll(miniAnthropicSSEStream(), "glm-4.7", model)
		} else {
			payload = fmt.Sprintf(`{"id":"msg_priority","type":"message","role":"assistant","model":%q,"content":[{"type":"text","text":"OK"}],"stop_reason":"end_turn","usage":{"input_tokens":10,"output_tokens":5}}`, model)
		}
	case "chat":
		if stream {
			payload = fmt.Sprintf("data: {\"id\":\"chat_priority\",\"model\":%q,\"choices\":[{\"index\":0,\"delta\":{\"content\":\"OK\"},\"finish_reason\":\"stop\"}]}\n\ndata: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":5}}\n\ndata: [DONE]\n\n", model)
		} else {
			payload = fmt.Sprintf(`{"id":"chat_priority","object":"chat.completion","model":%q,"choices":[{"index":0,"message":{"role":"assistant","content":"OK"},"finish_reason":"stop"}],"usage":{"prompt_tokens":10,"completion_tokens":5}}`, model)
		}
	default:
		payload = fmt.Sprintf(`{"id":"resp_priority","object":"response","status":"completed","model":%q,"output":[{"id":"msg_priority","type":"message","role":"assistant","status":"completed","content":[{"type":"output_text","text":"OK"}]}],"usage":{"input_tokens":10,"output_tokens":5}}`, model)
		if stream {
			payload = fmt.Sprintf("data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_priority\",\"model\":%q,\"status\":\"in_progress\",\"output\":[]}}\n\ndata: {\"type\":\"response.output_text.delta\",\"output_index\":0,\"content_index\":0,\"delta\":\"OK\"}\n\ndata: {\"type\":\"response.completed\",\"response\":%s}\n\n", model, payload)
		}
	}
	if stream {
		contentType = "text/event-stream"
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {contentType}}, Body: io.NopCloser(strings.NewReader(payload))}
}

func priorityHTTPBody(inbound, model string, stream bool) []byte {
	if inbound == "responses" {
		return []byte(fmt.Sprintf(`{"model":%q,"stream":%t,"input":"hello","instructions":"fixture instructions"}`, model, stream))
	}
	return []byte(fmt.Sprintf(`{"model":%q,"stream":%t,"max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`, model, stream))
}

func priorityHTTPContext(path string, body []byte, ctx context.Context) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body)).WithContext(ctx)
	c.Request.Header.Set("Content-Type", "application/json")
	SetGatewayClientModel(c, priorityHTTPSource)
	return c, rec
}

func TestAccountOriginalModelMappingHTTPForward(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name, inbound, upstream string
		forceStream             bool
	}{
		{"responses", "responses", "responses", false},
		{"passthrough", "responses", "responses", false},
		{"chat-responses", "chat", "responses", true},
		{"chat-raw", "chat", "chat", false},
		{"messages-responses", "messages", "responses", true},
		{"messages-chat", "messages", "chat", false},
		{"responses-chat", "responses", "chat", false},
		{"messages-native", "messages", "anthropic", false},
		{"chat-native", "chat", "anthropic", true},
		{"responses-native", "responses", "anthropic", true},
		{"generic-messages", "messages", "anthropic", false},
		{"generic-passthrough", "messages", "anthropic", false},
		{"generic-chat", "chat", "anthropic", true},
		{"generic-responses", "responses", "anthropic", true},
		{"grok", "responses", "responses", false},
	} {
		for _, scenario := range []string{"original-wins", "identity-wins", "channel-fallback", "resolved-wire"} {
			for _, stream := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/%s/stream=%t", tc.name, scenario, stream), func(t *testing.T) {
					account := rawChatCompletionsTestAccount()
					account.Extra = map[string]any{"openai_responses_supported": true}
					mapping := map[string]any{priorityHTTPSource: priorityHTTPTarget, priorityHTTPChannel: "wrong-channel-target", priorityHTTPTarget: "must-not-map-again"}
					bodyModel, want := priorityHTTPChannel, priorityHTTPTarget
					switch scenario {
					case "identity-wins":
						mapping[priorityHTTPSource], want = priorityHTTPSource, priorityHTTPSource
					case "channel-fallback", "resolved-wire":
						delete(mapping, priorityHTTPSource)
						mapping[priorityHTTPChannel] = priorityHTTPTarget
						if tc.name == "passthrough" && scenario == "channel-fallback" {
							want = priorityHTTPChannel
						}
					}
					account.Credentials["model_mapping"] = mapping
					if tc.upstream == "anthropic" && !strings.HasPrefix(tc.name, "generic-") {
						account.Platform = PlatformKimi
						account.Credentials["api_protocol"] = APIProtocolAnthropic
						account.Credentials["api_base_urls"] = map[string]any{APIProtocolAnthropic: "http://upstream.example"}
					}
					if tc.name == "passthrough" {
						account.Extra["openai_passthrough"] = true
					}
					if tc.name == "chat-raw" || tc.name == "messages-chat" || tc.name == "responses-chat" {
						account.Extra["openai_responses_supported"] = false
						account.Credentials["api_protocol"] = APIProtocolChatCompletions
					}
					if tc.name == "grok" {
						account.Platform = PlatformGrok
					}
					if strings.HasPrefix(tc.name, "generic-") {
						account.Platform = PlatformAnthropic
						account.Extra["anthropic_passthrough"] = tc.name == "generic-passthrough"
					}
					ctx := WithAccountModelMapping(context.Background(), priorityHTTPSource, priorityHTTPChannel)
					if scenario == "resolved-wire" {
						bodyModel = priorityHTTPTarget
						ctx = WithResolvedAccountModel(ctx, account.ID, bodyModel)
					}
					body := priorityHTTPBody(tc.inbound, bodyModel, stream)
					path := "/v1/responses"
					if tc.inbound != "responses" {
						path = "/v1/" + map[string]string{"chat": "chat/completions", "messages": "messages"}[tc.inbound]
					}
					c, rec := priorityHTTPContext(path, body, ctx)
					upstream := &httpUpstreamRecorder{resp: priorityHTTPResponse(tc.upstream, want, stream || tc.forceStream)}
					svc := openAIClientToolsTestService(upstream)
					svc.cfg = rawChatCompletionsTestConfig()
					var result *OpenAIForwardResult
					var err error
					if strings.HasPrefix(tc.name, "generic-") {
						generic := &GatewayService{cfg: svc.cfg, httpUpstream: upstream, rateLimitService: &RateLimitService{}, deferredService: &DeferredService{}}
						var forwarded *ForwardResult
						switch tc.name {
						case "generic-chat":
							forwarded, err = generic.ForwardAsChatCompletions(ctx, c, account, body, nil)
						case "generic-responses":
							forwarded, err = generic.ForwardAsResponses(ctx, c, account, body, nil)
						default:
							var parsed *ParsedRequest
							parsed, err = ParseGatewayRequest(NewRequestBodyRef(body), PlatformAnthropic)
							require.NoError(t, err)
							forwarded, err = generic.Forward(ctx, c, account, parsed)
						}
						if forwarded != nil {
							result = &OpenAIForwardResult{UpstreamModel: forwarded.UpstreamModel, Usage: claudeUsageToOpenAIUsage(&forwarded.Usage)}
						}
					} else {
						switch tc.name {
						case "chat-responses", "chat-raw", "chat-native":
							result, err = svc.ForwardAsChatCompletions(ctx, c, account, body, "", "")
						case "messages-responses", "messages-chat", "messages-native":
							result, err = svc.ForwardAsAnthropic(ctx, c, account, body, "", "")
						default:
							result, err = svc.ForwardWithOriginalModel(ctx, c, account, body, priorityHTTPSource)
						}
					}
					require.NoError(t, err, rec.Body.String())
					require.NotNil(t, upstream.lastReq)
					require.Equal(t, want, gjson.GetBytes(upstream.lastBody, "model").String(), "wire model must resolve the original account rule exactly once")
					require.NotNil(t, result)
					if tc.name != "passthrough" || scenario != "channel-fallback" {
						require.Equal(t, want, result.UpstreamModel)
					}
					require.Equal(t, 10, result.Usage.InputTokens)
					require.Contains(t, rec.Body.String(), `"model":"`+priorityHTTPSource+`"`, "client response keeps its public model")
				})
			}
		}
	}
}

func TestAccountOriginalModelMappingHTTPSIWC(t *testing.T) {
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			reauth, reader, repo, _, _, _ := newReauthTestService("acct-1")
			defer reauth.oauth.Stop()
			store := &siwcTestRepo{reauthTestRepo: repo}
			reauth.repo = store
			grant := siwcGrant{AccessToken: "independent-siwc", RefreshToken: "siwc-refresh", ClientID: "issued", Subject: "subscriber", Workspace: "acct-1", Email: "user@example.com", Scope: openai.SIWCDirectScope, ExpiresAt: time.Now().Add(time.Hour).Unix()}
			raw, err := json.Marshal(grant)
			require.NoError(t, err)
			store.ciphertext = "encrypted:" + string(raw)
			reader.account.Extra = map[string]any{OpenAISIWCEnabledKey: true}
			reader.account.Credentials["model_mapping"] = map[string]any{priorityHTTPSource: priorityHTTPTarget, priorityHTTPChannel: "wrong-channel-target", priorityHTTPTarget: "must-not-map-again"}
			ctx := WithAccountModelMapping(context.Background(), priorityHTTPSource, priorityHTTPChannel)
			body := priorityHTTPBody("responses", priorityHTTPChannel, stream)
			c, rec := priorityHTTPContext("/v1/responses", body, ctx)
			upstream := &httpUpstreamRecorder{resp: priorityHTTPResponse("responses", priorityHTTPTarget, true)}
			gateway := openAIClientToolsTestService(upstream)
			gateway.excelOAuthReauth = reauth
			result, err := gateway.ForwardWithOriginalModel(ctx, c, reader.account, body, priorityHTTPSource)
			require.NoError(t, err, rec.Body.String())
			require.Equal(t, "api.openai.com", upstream.lastReq.URL.Host)
			require.Equal(t, "Bearer independent-siwc", upstream.lastReq.Header.Get("Authorization"))
			require.Equal(t, priorityHTTPTarget, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, priorityHTTPTarget, result.UpstreamModel)
			require.Contains(t, rec.Body.String(), `"model":"`+priorityHTTPSource+`"`)
		})
	}
}

func TestAccountOriginalModelMappingHTTPBPS(t *testing.T) {
	const target = "gpt-5.6-sol"
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprint(stream), func(t *testing.T) {
			account := excelAccount()
			account.Credentials["model_mapping"] = map[string]any{priorityHTTPSource: target, priorityHTTPChannel: "gpt-5.4", target: "gpt-5.4"}
			account.Extra["openai_excel_bps_models"] = []any{target}
			ctx := WithAccountModelMapping(context.Background(), priorityHTTPSource, priorityHTTPChannel)
			body := priorityHTTPBody("responses", priorityHTTPChannel, stream)
			c, rec := priorityHTTPContext("/v1/responses", body, ctx)
			upstream := &httpUpstreamRecorder{resp: priorityHTTPResponse("responses", target, true)}
			result, err := openAIClientToolsTestService(upstream).ForwardWithOriginalModel(ctx, c, account, body, priorityHTTPSource)
			require.NoError(t, err, rec.Body.String())
			require.Equal(t, "bps.openai.com", upstream.lastReq.URL.Host)
			require.Equal(t, target, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, target, result.UpstreamModel)
			require.Contains(t, rec.Body.String(), `"model":"`+priorityHTTPSource+`"`)
		})
	}
}

func TestAccountOriginalModelMappingHTTPCompactKeepsSpecificRules(t *testing.T) {
	for _, tc := range []struct {
		name, want string
		original   bool
		compactA   bool
	}{
		{"original compact first", "compact-original", true, true},
		{"mapped compact fallback", "compact-target", true, false},
		{"channel compact on miss", "compact-channel", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			account := rawChatCompletionsTestAccount()
			account.Credentials["base_url"] = "https://upstream.example"
			mapping := map[string]any{priorityHTTPChannel: priorityHTTPTarget, priorityHTTPTarget: "must-not-map-again"}
			if tc.original {
				mapping[priorityHTTPSource] = priorityHTTPTarget
			}
			compact := map[string]any{priorityHTTPChannel: "compact-channel", priorityHTTPTarget: "compact-target"}
			if tc.compactA {
				compact[priorityHTTPSource] = "compact-original"
			}
			account.Credentials["model_mapping"], account.Credentials["compact_model_mapping"] = mapping, compact
			ctx := WithAccountModelMapping(context.Background(), priorityHTTPSource, priorityHTTPChannel)
			body := priorityHTTPBody("responses", priorityHTTPChannel, false)
			c, rec := priorityHTTPContext("/v1/responses/compact", body, ctx)
			upstream := &httpUpstreamRecorder{resp: priorityHTTPResponse("responses", tc.want, false)}
			result, err := openAIClientToolsTestService(upstream).ForwardWithOriginalModel(ctx, c, account, body, priorityHTTPSource)
			require.NoError(t, err, rec.Body.String())
			require.Equal(t, tc.want, gjson.GetBytes(upstream.lastBody, "model").String())
			require.Equal(t, tc.want, result.UpstreamModel)
			require.Contains(t, rec.Body.String(), `"model":"`+priorityHTTPSource+`"`)
		})
	}
}

func TestAccountOriginalModelMappingHTTPWithoutBasisKeepsLegacy(t *testing.T) {
	for _, passthrough := range []bool{false, true} {
		t.Run(fmt.Sprint(passthrough), func(t *testing.T) {
			account := rawChatCompletionsTestAccount()
			account.Credentials["base_url"] = "https://upstream.example"
			account.Extra = map[string]any{"openai_passthrough": passthrough}
			account.Credentials["model_mapping"] = map[string]any{priorityHTTPSource: priorityHTTPTarget, priorityHTTPChannel: "legacy-channel-target"}
			want := "legacy-channel-target"
			if passthrough {
				want = priorityHTTPChannel
			}
			body := priorityHTTPBody("responses", priorityHTTPChannel, false)
			c, rec := priorityHTTPContext("/v1/responses", body, context.Background())
			upstream := &httpUpstreamRecorder{resp: priorityHTTPResponse("responses", want, false)}
			_, err := openAIClientToolsTestService(upstream).ForwardWithOriginalModel(context.Background(), c, account, body, priorityHTTPSource)
			require.NoError(t, err, rec.Body.String())
			require.Equal(t, want, gjson.GetBytes(upstream.lastBody, "model").String())
		})
	}
}

func TestAccountOriginalModelMappingHTTPNativeResponseModelBoundary(t *testing.T) {
	for _, explicitClient := range []bool{false, true} {
		t.Run(fmt.Sprint(explicitClient), func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			want := priorityHTTPTarget
			if explicitClient {
				SetGatewayClientModel(c, priorityHTTPSource)
				want = priorityHTTPSource
			}
			resp := priorityHTTPResponse("anthropic", priorityHTTPTarget, false)
			gateway := &OpenAIGatewayService{cfg: rawChatCompletionsTestConfig()}
			result, err := gateway.handleNativeAnthropicBufferedResponse(context.Background(), resp, c, rawChatCompletionsTestAccount(), priorityHTTPSource, priorityHTTPTarget, priorityHTTPTarget, nil, time.Now())
			require.NoError(t, err)
			require.Equal(t, want, gjson.GetBytes(rec.Body.Bytes(), "model").String())
			require.Equal(t, priorityHTTPTarget, result.UpstreamModel)
			require.Equal(t, priorityHTTPTarget, result.UpstreamResponseModel)
			require.Equal(t, 10, result.Usage.InputTokens)
		})
	}
}
