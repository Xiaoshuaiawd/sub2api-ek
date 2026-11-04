package service

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestOpenAIWSFirstWordReadBudgetUsesTotalDeadline(t *testing.T) {
	started := time.Now().Add(-2 * time.Second)
	remaining, expired := openAIWSFirstWordReadBudget(started, time.Second)
	require.True(t, expired)
	require.Zero(t, remaining)

	remaining, expired = openAIWSFirstWordReadBudget(time.Now(), time.Minute)
	require.False(t, expired)
	require.Greater(t, remaining, 59*time.Second)
}

func TestForwardOpenAIWSV2_EmptyTerminalAfterCreatedEndsWith502WithoutReplay(t *testing.T) {
	for _, eventType := range []string{"response.completed", "response.done"} {
		t.Run(eventType, func(t *testing.T) {
			testForwardOpenAIWSV2EmptyTerminalAfterCreated(t, eventType)
		})
	}
}

func testForwardOpenAIWSV2EmptyTerminalAfterCreated(t *testing.T, eventType string) {
	gin.SetMode(gin.TestMode)
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	c.Request.Header.Set("User-Agent", "unit-test-agent/1.0")

	cfg := &config.Config{}
	cfg.Security.URLAllowlist.Enabled = false
	cfg.Security.URLAllowlist.AllowInsecureHTTP = true
	cfg.Gateway.OpenAIFirstWordProtectionEnabled = true
	cfg.Gateway.OpenAIWS.Enabled = true
	cfg.Gateway.OpenAIWS.APIKeyEnabled = true
	cfg.Gateway.OpenAIWS.ResponsesWebsocketsV2 = true
	cfg.Gateway.OpenAIWS.MaxConnsPerAccount = 1
	cfg.Gateway.OpenAIWS.MaxIdlePerAccount = 1
	cfg.Gateway.OpenAIWS.QueueLimitPerConn = 8
	cfg.Gateway.OpenAIWS.DialTimeoutSeconds = 3
	cfg.Gateway.OpenAIWS.ReadTimeoutSeconds = 5
	cfg.Gateway.OpenAIWS.WriteTimeoutSeconds = 3

	conn := &openAIWSCaptureConn{events: [][]byte{
		[]byte(`{"type":"response.created","response":{"id":"resp_empty","model":"gpt-5.5"}}`),
		[]byte(fmt.Sprintf(`{"type":%q,"response":{"id":"resp_empty","model":"gpt-5.5","status":"completed"}}`, eventType)),
	}}
	pool := newOpenAIWSConnPool(cfg)
	pool.setClientDialerForTest(&openAIWSCaptureDialer{conn: conn})
	svc := &OpenAIGatewayService{
		cfg: cfg, httpUpstream: &httpUpstreamRecorder{}, cache: &stubGatewayCache{},
		openaiWSResolver: NewOpenAIWSProtocolResolver(cfg), toolCorrector: NewCodexToolCorrector(), openaiWSPool: pool,
	}
	account := &Account{
		ID: 5883, Name: "openai-ws-empty", Platform: PlatformOpenAI, Type: AccountTypeAPIKey,
		Status: StatusActive, Schedulable: true, Concurrency: 1,
		Credentials: map[string]any{"api_key": "sk-test"},
		Extra:       map[string]any{"responses_websockets_v2_enabled": true},
	}

	_, err := svc.Forward(context.Background(), c, account, []byte(`{"model":"gpt-5.5","stream":false,"input":"hi"}`))
	var failoverErr *UpstreamFailoverError
	require.True(t, errors.As(err, &failoverErr), "empty completed must fail, got: %v", err)
	require.Equal(t, http.StatusBadGateway, failoverErr.StatusCode)
	require.False(t, failoverErr.ShouldRetryNextAccount())
	require.Empty(t, recorder.Body.String(), "forwarder must not write a successful response")
}
