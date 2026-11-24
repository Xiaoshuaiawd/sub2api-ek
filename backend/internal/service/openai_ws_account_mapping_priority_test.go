//go:build unit

package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/tlsfingerprint"
	coderws "github.com/coder/websocket"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

type wsAccountMappingNativeConn struct{ *stagedPassthroughConn }

func (c *wsAccountMappingNativeConn) WriteJSON(ctx context.Context, value any) error {
	var payload []byte
	switch v := value.(type) {
	case openAIWSPreparedJSON:
		payload = []byte(v)
	case json.RawMessage:
		payload = []byte(v)
	case []byte:
		payload = v
	default:
		var err error
		payload, err = json.Marshal(value)
		if err != nil {
			return err
		}
	}
	return c.WriteFrame(ctx, coderws.MessageText, payload)
}

type wsAccountMappingHTTPUpstream struct {
	writes chan []byte
	turns  atomic.Int64
}

func wsAccountMappingCompleted(model string, turn int64) string {
	response, _ := json.Marshal(map[string]any{"type": "response.completed", "response": map[string]any{
		"id": fmt.Sprintf("resp_mapping_%d", turn), "model": model, "usage": map[string]int{"input_tokens": 1, "output_tokens": 1},
	}})
	return string(response)
}

func (u *wsAccountMappingHTTPUpstream) Do(req *http.Request, _ string, _ int64, _ int) (*http.Response, error) {
	payload, err := io.ReadAll(req.Body)
	if err != nil {
		return nil, err
	}
	_ = req.Body.Close()
	u.writes <- payload
	completed := wsAccountMappingCompleted(gjson.GetBytes(payload, "model").String(), u.turns.Add(1))
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": {"text/event-stream"}}, Body: io.NopCloser(strings.NewReader("data: " + completed + "\n\n"))}, nil
}

func (u *wsAccountMappingHTTPUpstream) DoWithTLS(req *http.Request, proxy string, id int64, concurrency int, _ *tlsfingerprint.Profile) (*http.Response, error) {
	return u.Do(req, proxy, id, concurrency)
}

// The hook already promises a final upstream model. Its output is allowed to
// be a key in the same account mapping and must still be sent exactly once.
func TestWSAccountMappingFinalHookIsNotMappedAgain(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, scenario := range []struct {
		mode string
		hook bool
	}{
		{OpenAIWSIngressModeCtxPool, true}, {OpenAIWSIngressModeHTTPBridge, true}, {OpenAIWSIngressModePassthrough, true},
		{OpenAIWSIngressModeCtxPool, false}, {OpenAIWSIngressModeHTTPBridge, false},
	} {
		mode := scenario.mode
		t.Run(fmt.Sprintf("%s/hook=%t", mode, scenario.hook), func(t *testing.T) {
			const original = "client-model-a"
			wireModels := []string{"account-model-c", "account-model-e"}
			if !scenario.hook {
				wireModels[1] = wireModels[0]
			}
			cfg := passthroughLifecycleConfig()
			cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 3
			cfg.Gateway.OpenAIWS.IngressInterTurnIdleTimeoutSeconds = 3
			upstream := newStagedPassthroughConn()
			svc := newPassthroughLifecycleService(cfg, upstream)
			account := passthroughLifecycleAccount()
			account.Extra["openai_apikey_responses_websockets_v2_mode"] = mode
			account.Credentials["model_mapping"] = map[string]any{
				original: "account-model-c", "account-model-c": "wrong-model-d", "account-model-e": "wrong-model-f",
			}
			var httpUpstream *wsAccountMappingHTTPUpstream
			if mode == OpenAIWSIngressModeCtxPool {
				pool := newOpenAIWSConnPool(cfg)
				pool.setClientDialerForTest(&stagedPassthroughDialer{conn: &wsAccountMappingNativeConn{upstream}})
				svc.openaiWSPool = pool
				t.Cleanup(pool.Close)
			} else if mode == OpenAIWSIngressModeHTTPBridge {
				httpUpstream = &wsAccountMappingHTTPUpstream{writes: make(chan []byte, 4)}
				svc.httpUpstream = httpUpstream
			}
			ctx, cancel := context.WithCancelCause(context.Background())
			t.Cleanup(func() { cancel(context.Canceled) })
			mappedFrom := make(chan string, 4)
			results := make(chan *OpenAIForwardResult, 4)
			hooks := &OpenAIWSIngressHooks{
				InitialRequestModel: original,
				MapRequestModel: func(turn int, model string) (string, error) {
					mappedFrom <- model
					return wireModels[turn-1], nil
				},
				AfterTurn: func(_ int, result *OpenAIForwardResult, _ error) {
					if result != nil {
						results <- result
					}
				},
			}
			if !scenario.hook {
				hooks.MapRequestModel = nil
			}
			server, serverErr := startPassthroughHookRecordingServer(t, ctx, svc, account, hooks)
			t.Cleanup(server.Close)
			client := dialPassthroughLifecycleClientWithPayload(t, server, `{"type":"response.create","model":"client-model-a","stream":false,"input":"first"}`)
			t.Cleanup(func() { _ = client.CloseNow() })
			for turn, want := range wireModels {
				if turn > 0 {
					writeCtx, stop := context.WithTimeout(context.Background(), time.Second)
					err := client.Write(writeCtx, coderws.MessageText, []byte(`{"type":"response.create","stream":false,"input":"second"}`))
					stop()
					require.NoError(t, err)
				}
				var payload []byte
				if httpUpstream != nil {
					select {
					case payload = <-httpUpstream.writes:
					case <-time.After(3 * time.Second):
						t.Fatal("bridge did not write upstream")
					}
				} else {
					payload = requirePassthroughUpstreamWrite(t, upstream, 3*time.Second)
					upstream.Send(wsAccountMappingCompleted(gjson.GetBytes(payload, "model").String(), int64(turn+1)))
				}
				assert.Equal(t, want, gjson.GetBytes(payload, "model").String(), "final hook result must not undergo C→D account mapping")
				if scenario.hook {
					select {
					case got := <-mappedFrom:
						assert.Equal(t, original, got, "omitted follow-up model must inherit the client model, not the previous wire model")
					case <-time.After(time.Second):
						t.Fatal("mapping hook was not invoked for this turn")
					}
				}
				event, err := readPassthroughLifecycleFrame(t, client, 3*time.Second)
				require.NoError(t, err)
				assert.Equal(t, original, gjson.GetBytes(event, "response.model").String())
				select {
				case result := <-results:
					assert.Equal(t, original, result.Model)
					assert.Equal(t, want, result.UpstreamModel)
				case <-time.After(time.Second):
					t.Fatal("turn usage result was not reported")
				}
			}
			_ = client.Close(coderws.StatusNormalClosure, "done")
			select {
			case err := <-serverErr:
				require.NoError(t, err)
			case <-time.After(3 * time.Second):
				t.Fatal("ingress did not exit after client close")
			}
		})
	}
}

func TestWSAccountMappingSnapshotPreservesCurrentTurnBasis(t *testing.T) {
	type traceKey struct{}
	base, cancel := context.WithCancel(context.WithValue(context.Background(), traceKey{}, "preserved"))
	defer cancel()
	base = WithAccountModelMapping(base, "handshake-a", "handshake-b")
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil).WithContext(WithAccountModelMapping(context.Background(), "turn-a", "turn-b"))
	account := &Account{ID: 71, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
	snapshot := captureOpenAIWSModelMapping(c, account, "turn-e")
	// A later callback may replace Request. Pool callbacks must only consume
	// the captured values, rather than concurrently reading Gin.Request.
	c.Request = c.Request.WithContext(WithAccountModelMapping(context.Background(), "later-a", "later-b"))
	turn := snapshot.withContext(base)
	a, b, ok := AccountModelMappingModels(turn)
	require.True(t, ok)
	require.Equal(t, "turn-a", a)
	require.Equal(t, "turn-b", b)
	require.Equal(t, "preserved", turn.Value(traceKey{}))
	wire, ok := ResolvedAccountModel(turn, account, "turn-e")
	require.True(t, ok)
	require.Equal(t, "turn-e", wire)
	_, ok = ResolvedAccountModel(turn, &Account{ID: 72}, "turn-e")
	require.False(t, ok)
	_, ok = ResolvedAccountModel(turn, account, "other-wire")
	require.False(t, ok)
	fresh := *account
	fresh.Credentials = map[string]any{"model_mapping": map[string]any{"turn-a": "fresh-target"}}
	// The wire marker must not cache the current account's authorization.
	require.Equal(t, "turn-a", AccountModelMappingInput(turn, &fresh, "turn-e", false))
	target, matched := ResolveAccountOriginalModelMapping(turn, &fresh)
	require.True(t, matched)
	require.Equal(t, "fresh-target", target)
	cancel()
	require.ErrorIs(t, turn.Err(), context.Canceled)
}

func TestWSAccountMappingSnapshotWithoutBasisClearsHandshakeSource(t *testing.T) {
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	account := &Account{ID: 71}
	base := WithAccountModelMapping(context.Background(), "old-a", "old-b")
	turn := captureOpenAIWSModelMapping(c, account, "new-e").withContext(base)
	_, _, ok := AccountModelMappingModels(turn)
	require.False(t, ok)
	wire, ok := ResolvedAccountModel(turn, account, "new-e")
	require.True(t, ok)
	require.Equal(t, "new-e", wire)
}

func TestWSAccountMappingGrokIdentityMarkerIsScoped(t *testing.T) {
	account := &Account{ID: 71, Platform: PlatformGrok, Type: AccountTypeAPIKey,
		Credentials: map[string]any{"model_mapping": map[string]any{"grok": "grok-4.3"}}}
	payload := []byte(`{"model":"grok","input":"hi"}`)
	legacy := resolveGrokWSUpstreamModel(account, payload, "grok")
	require.NotEqual(t, "grok", legacy)
	ctx := WithResolvedAccountModel(context.Background(), account.ID, "grok")
	require.Equal(t, "grok", resolveGrokWSUpstreamModelForRequest(ctx, account, payload, "grok"))
	other := *account
	other.ID++
	require.Equal(t, legacy, resolveGrokWSUpstreamModelForRequest(ctx, &other, payload, "grok"))
	ctx = WithAccountModelMapping(ctx, "new-source", "new-channel")
	require.Equal(t, legacy, resolveGrokWSUpstreamModelForRequest(ctx, account, payload, "grok"))
}

func TestWSAccountMappingCrossAccountReplayRestoresOriginalInput(t *testing.T) {
	const original, channel, firstWire, replacementWire = "client-a", "channel-b", "account-c", "replacement-e"
	wire := []byte(`{"type":"response.create","model":"account-c","previous_response_id":"resp_old","input":[]}`)
	fullInput := []json.RawMessage{json.RawMessage(`{"role":"user","content":"retained history"}`)}
	retry, safe, err := buildOpenAIWSCurrentTurnRetryPayload(wire, fullInput, true, original)
	require.NoError(t, err)
	require.True(t, safe)
	require.Equal(t, original, gjson.GetBytes(retry, "model").String())
	require.False(t, gjson.GetBytes(retry, "previous_response_id").Exists())
	require.Equal(t, "retained history", gjson.GetBytes(retry, "input.0.content").String())
	replacement := &Account{ID: 72, Platform: PlatformOpenAI, Type: AccountTypeAPIKey, Credentials: map[string]any{
		"model_mapping": map[string]any{original: replacementWire, channel: "wrong-channel-target", firstWire: "wrong-second-map"},
	}}
	old := WithResolvedAccountModel(WithAccountModelMapping(context.Background(), original, channel), 71, firstWire)
	next := WithAccountModelMapping(old, original, channel)
	require.Equal(t, replacementWire, AccountMappedModelForRequest(next, replacement, gjson.GetBytes(retry, "model").String()))
	// A same-account reconnect deliberately keeps its already resolved model;
	// it does not reinterpret that model as another configured source key.
	same, safe, err := buildOpenAIWSCurrentTurnRetryPayload(wire, fullInput, true, "")
	require.NoError(t, err)
	require.True(t, safe)
	require.Equal(t, firstWire, gjson.GetBytes(same, "model").String())
}

func TestWSAccountMappingGrokUnscopedDefaultAliasRemainsMapped(t *testing.T) {
	account := &Account{ID: 71, Platform: PlatformGrok, Type: AccountTypeOAuth}
	payload := []byte(`{"model":"grok","input":"hi"}`)
	c, _ := gin.CreateTestContext(httptest.NewRecorder())
	c.Request = httptest.NewRequest(http.MethodGet, "/v1/responses", nil)
	ctx := captureOpenAIWSModelMapping(c, account, "grok").withContext(context.Background())
	legacy := resolveGrokWSUpstreamModel(account, payload, "grok")
	require.Equal(t, "grok-4.6", legacy)
	require.Equal(t, legacy, resolveGrokWSUpstreamModelForRequest(ctx, account, payload, "grok"), "an unscoped legacy hook echo must not suppress provider defaults")
}

func TestWSAccountMappingGrokExplicitIdentitySkipsDefaultAlias(t *testing.T) {
	account := &Account{ID: 71, Platform: PlatformGrok, Type: AccountTypeOAuth,
		Credentials: map[string]any{"model_mapping": map[string]any{"grok": "grok"}}}
	payload := []byte(`{"model":"grok","input":"hi"}`)
	ctx := WithResolvedAccountModel(context.Background(), account.ID, "grok")
	require.Equal(t, "grok", resolveGrokWSUpstreamModelForRequest(ctx, account, payload, "grok"), "explicit identity remains an account decision, not a missing mapping")
}
