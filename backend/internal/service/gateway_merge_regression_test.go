//go:build unit

package service

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/Wei-Shaw/sub2api/internal/pkg/apicompat"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestGatewayMergedAnthropicRelaysPreserveClientModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, transport := range []string{"gateway", "passthrough", "openai-native"} {
		t.Run(transport, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			SetGatewayClientModel(c, "public-alias")
			resp := nativeAnthropicStreamResponse()
			account := &Account{ID: 1}
			var result *streamingResult
			var err error
			switch transport {
			case "gateway":
				result, err = newMinimalGatewayService().handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "channel-model", "k3", false)
			case "passthrough":
				result, err = newMinimalGatewayService().handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, account, time.Now(), "k3")
			case "openai-native":
				result, err = (&OpenAIGatewayService{}).relayAnthropicStream(context.Background(), resp, c, account, "channel-model", "k3", time.Now(), true)
			}
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, 93, result.usage.InputTokens)
			require.Equal(t, 16, result.usage.OutputTokens)
			require.Contains(t, rec.Body.String(), `"model":"public-alias"`)
			require.NotContains(t, rec.Body.String(), `"model":"k3"`)
			require.Equal(t, "k3", observedUpstreamResponseModel(c), "audit the actual upstream before client rewriting")
		})
	}
}

func TestGatewayMergedMappedErrorAfterKeepalive(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name      string
		status    int
		rule      bool
		exhausted bool
	}{
		{name: "bad request", status: http.StatusBadRequest},
		{name: "passthrough rule", status: http.StatusUnprocessableEntity, rule: true},
		{name: "exhausted rule", status: http.StatusUnprocessableEntity, rule: true, exhausted: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			c.Set("ops_model", "public-alias")
			SetOpsUpstreamModel(c, "xy")
			const ping = ": ping\n\n"
			c.Header("Content-Type", "text/event-stream")
			_, err := c.Writer.WriteString(ping)
			require.NoError(t, err)
			c.Writer.Flush()
			if tc.rule {
				rule := newNonFailoverPassthroughRule(tc.status, "Invalid schema", http.StatusTeapot, "model xy rejected")
				rules := &ErrorPassthroughService{}
				rules.setLocalCache([]*model.ErrorPassthroughRule{rule})
				BindErrorPassthroughService(c, rules)
			}
			resp := &http.Response{StatusCode: tc.status, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"error":{"type":"invalid_request_error","message":"Invalid schema for model xy"}}`))}
			svc := &GatewayService{}
			account := &Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeAPIKey}
			if tc.exhausted {
				_, err = svc.handleRetryExhaustedError(context.Background(), resp, c, account)
			} else {
				_, err = svc.handleErrorResponse(context.Background(), resp, c, account)
			}
			require.Error(t, err)
			require.Contains(t, err.Error(), "xy", "retain the upstream diagnostic internally")
			require.Equal(t, http.StatusOK, rec.Code)
			payload := requireGatewayKeepaliveErrorEvent(t, rec.Body.String(), ping)
			require.Equal(t, UpstreamModelLeakClientType, payload.Error.Type)
			require.Equal(t, UpstreamModelLeakClientMessage, payload.Error.Message)
			require.NotContains(t, rec.Body.String(), "xy")
			require.True(t, IsResponseCommitted(c))
		})
	}
}

func TestGatewayMergedNativeFailureHidesModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, transport := range []string{"gateway", "passthrough", "openai-native"} {
		t.Run(transport, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/messages", nil)
			c.Set("ops_model", "public-alias")
			SetOpsUpstreamModel(c, "xy")
			wire := issue985Stream() + issue985Frame("error", `{"type":"error","error":{"type":"safety_error","message":"model xy rejected"}}`)
			resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}
			var result *streamingResult
			var err error
			if transport == "gateway" {
				result, err = newMinimalGatewayService().handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "public-alias", "xy", false)
			} else if transport == "passthrough" {
				result, err = (&GatewayService{}).handleStreamingResponseAnthropicAPIKeyPassthrough(context.Background(), resp, c, &Account{ID: 1}, time.Now(), "xy")
			} else {
				result, err = (&OpenAIGatewayService{}).relayAnthropicStream(context.Background(), resp, c, &Account{ID: 1}, "public-alias", "xy", time.Now(), true)
			}
			require.Error(t, err)
			require.NotNil(t, result)
			require.Equal(t, 17, result.usage.InputTokens)
			require.Equal(t, 3, result.usage.OutputTokens)
			require.NotContains(t, rec.Body.String(), "model xy")
			require.Contains(t, rec.Body.String(), UpstreamModelLeakClientMessage)
			streamErrors := GetOpsStreamErrors(c)
			require.NotEmpty(t, streamErrors)
			require.Contains(t, streamErrors[len(streamErrors)-1].Message, "model xy", "retain raw upstream diagnostics for Ops")
		})
	}
}

func TestGatewayMergedResponsesFailurePreservesUsageAndHidesModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(fmt.Sprintf("stream=%v", stream), func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			c.Set("ops_model", "public-alias")
			SetOpsUpstreamModel(c, "xy")
			wire := issue985Stream() + issue985Frame("error", `{"type":"error","error":{"type":"safety_error","message":"model xy rejected"}}`)
			resp := &http.Response{Header: http.Header{}, Body: io.NopCloser(strings.NewReader(wire))}
			svc := &GatewayService{}
			var result *ForwardResult
			var err error
			if stream {
				result, err = svc.handleResponsesStreamingResponse(resp, c, "public-alias", "xy", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
			} else {
				result, err = svc.handleResponsesBufferedStreamingResponse(resp, c, "public-alias", "xy", nil, time.Now(), apicompat.ResponsesClientToolMapping{})
			}
			require.Error(t, err)
			require.NotNil(t, result, "retain metered usage on a terminal safety failure")
			require.Equal(t, 17, result.Usage.InputTokens)
			require.Equal(t, 3, result.Usage.OutputTokens)
			require.NotContains(t, rec.Body.String(), "model xy")
			require.Contains(t, rec.Body.String(), UpstreamModelLeakClientMessage)
			require.NotContains(t, rec.Body.String(), "response.completed")
		})
	}
}
