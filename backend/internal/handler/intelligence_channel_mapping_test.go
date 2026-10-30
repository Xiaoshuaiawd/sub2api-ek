//go:build unit

package handler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/Wei-Shaw/sub2api/internal/server/middleware"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

// A is the client model, B is a channel-only mapping, and C is an account
// mapping. Intelligence requests bypass A→B, but must still apply A→C.
func TestIntelligenceChannelMappingOpenAIHTTP(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, endpoint := range []string{"chat", "responses"} {
		for _, stream := range []bool{false, true} {
			for _, tc := range []struct {
				name         string
				intelligence bool
				mapping      map[string]any
				wantUpstream string
				history      string
			}{
				{"ordinary_retains_channel_mapping", false, map[string]any{"client-model-a": "account-model-c", "upstream-model-b": "upstream-model-b"}, "upstream-model-b", ""},
				{"intelligence_preserves_original_model", true, map[string]any{"client-model-a": "client-model-a"}, "client-model-a", ""},
				{"intelligence_preserves_account_mapping", true, map[string]any{"client-model-a": "account-model-c"}, "account-model-c", ""},
				{"ordinary_after_intelligence_history", false, map[string]any{"upstream-model-b": "upstream-model-b"}, "upstream-model-b", "Create an SVG of a pelican riding a bicycle"},
				{"intelligence_after_ordinary_history", true, map[string]any{"client-model-a": "account-model-c"}, "account-model-c", "hello"},
			} {
				t.Run(fmt.Sprintf("%s/stream=%t/%s", endpoint, stream, tc.name), func(t *testing.T) {
					groupID := int64(4268)
					extra := map[string]any{service.IntelligenceRoutingExtraKey: tc.intelligence}
					if endpoint == "chat" {
						extra["openai_responses_supported"] = false
					}
					account := service.Account{
						ID: 9968, Name: "intelligence-channel-mapping", Platform: service.PlatformOpenAI,
						Type: service.AccountTypeAPIKey, Status: service.StatusActive, Schedulable: true, Concurrency: 1,
						Credentials: map[string]any{"api_key": "sk-test", "base_url": "https://upstream.example/v1", "model_mapping": tc.mapping},
						Extra:       extra,
					}
					accountRepo := &openAIWSUsageHandlerAccountRepoStub{account: account}
					channelSvc := service.NewChannelService(&openAIWSUsageHandlerChannelRepoStub{
						channels:       []service.Channel{{ID: 7768, Status: service.StatusActive, GroupIDs: []int64{groupID}, ModelMapping: map[string]map[string]string{service.PlatformOpenAI: {"client-model-a": "upstream-model-b"}}}},
						groupPlatforms: map[int64]string{groupID: service.PlatformOpenAI},
					}, nil, nil, nil, nil)
					chatUpstream := &openAIChatChannelMappingUpstream{}
					responsesUpstream := &openAIResponsesChannelMappingUpstream{}
					var upstream service.HTTPUpstream = chatUpstream
					if endpoint == "responses" {
						upstream = responsesUpstream
					}
					cfg := &config.Config{RunMode: config.RunModeSimple}
					cfg.Security.URLAllowlist.Enabled = false
					billing := service.NewBillingCacheService(nil, nil, nil, nil, nil, nil, cfg, nil)
					t.Cleanup(billing.Stop)
					gateway := service.NewOpenAIGatewayService(accountRepo, nil, nil, nil, nil, nil, nil, nil, cfg, nil, nil, service.NewBillingService(cfg, nil), nil, billing, upstream, &service.DeferredService{}, nil, nil, nil, channelSvc, nil, nil, nil)
					cache := &concurrencyCacheMock{
						acquireUserSlotFn:    func(context.Context, int64, int, string) (bool, error) { return true, nil },
						acquireAccountSlotFn: func(context.Context, int64, int, string) (bool, error) { return true, nil },
					}
					h := NewOpenAIGatewayHandler(gateway, service.NewConcurrencyService(cache), billing, service.NewAPIKeyService(nil, nil, nil, nil, nil, nil, cfg), nil, nil, nil, nil, cfg)
					content := "hello"
					if tc.intelligence {
						content = "Create an SVG of a pelican riding a bicycle"
					}
					var path, body string
					if endpoint == "chat" {
						path = "/openai/v1/chat/completions"
						if tc.history != "" {
							body = fmt.Sprintf(`{"model":"client-model-a","messages":[{"role":"user","content":%q},{"role":"assistant","content":"ok"},{"role":"user","content":%q}],"stream":%t}`, tc.history, content, stream)
						} else {
							body = fmt.Sprintf(`{"model":"client-model-a","messages":[{"role":"user","content":%q}],"stream":%t}`, content, stream)
						}
					} else {
						path = "/openai/v1/responses"
						if tc.history != "" {
							body = fmt.Sprintf(`{"model":"client-model-a","input":[{"role":"user","content":%q},{"role":"assistant","content":"ok"},{"role":"user","content":%q}],"stream":%t}`, tc.history, content, stream)
						} else {
							body = fmt.Sprintf(`{"model":"client-model-a","input":%q,"stream":%t}`, content, stream)
						}
					}
					rec := httptest.NewRecorder()
					c, _ := gin.CreateTestContext(rec)
					c.Request = httptest.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
					c.Request.Header.Set("Content-Type", "application/json")
					c.Set(string(middleware.ContextKeyAPIKey), &service.APIKey{ID: 1868, GroupID: &groupID, User: &service.User{ID: 1768, Status: service.StatusActive}, Group: &service.Group{ID: groupID, Platform: service.PlatformOpenAI, Status: service.StatusActive}})
					c.Set(string(middleware.ContextKeyUser), middleware.AuthSubject{UserID: 1768, Concurrency: 1})
					if endpoint == "chat" {
						h.ChatCompletions(c)
						require.Equal(t, []string{tc.wantUpstream}, chatUpstream.models(), "status %d body: %s", rec.Code, rec.Body.String())
					} else {
						h.Responses(c)
						require.Equal(t, []string{tc.wantUpstream}, responsesUpstream.models(), "status %d body: %s", rec.Code, rec.Body.String())
					}
					require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
				})
			}
		}
	}
}

func TestIntelligenceChannelMappingResponsesWebSocket(t *testing.T) {
	for _, mode := range []string{service.OpenAIWSIngressModeCtxPool, service.OpenAIWSIngressModePassthrough} {
		t.Run(mode, func(t *testing.T) {
			got := runOpenAIResponsesWebSocketUsageLogCase(t, openAIResponsesWSUsageLogCase{
				firstPayload:        `{"type":"response.create","model":"public-model","input":"Generate an SVG of a pelican riding a bicycle"}`,
				secondPayload:       `{"type":"response.create","model":"public-model","input":"Create a 2D SVG animation of a cat playing piano"}`,
				ingressMode:         mode,
				intelligenceAccount: true,
				channelMapping:      map[string]string{"public-model": "channel-model"},
				accountModelMapping: map[string]any{"public-model": "account-model"},
			})
			require.Len(t, got.upstreamPayloads, 2)
			// WebSocket passthrough has always skipped account-level mapping;
			// ctx_pool retains it. Both must skip the channel mapping.
			want := "account-model"
			if mode == service.OpenAIWSIngressModePassthrough {
				want = "public-model"
			}
			for _, payload := range got.upstreamPayloads {
				require.Equal(t, want, gjson.GetBytes(payload, "model").String())
			}
		})
	}
}
