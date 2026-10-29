package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/model"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestResponsesStreamHidesUpstreamModelInFailedEvents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		for _, tc := range []struct{ name, eventType, payload string }{
			{"nested error", "error", `{"type":"error","error":{"message":"private-model is unavailable","code":"private-model"}}`},
			{"top-level error", "error", `{"type":"error","message":"private-model is unavailable","code":"private-model"}`},
			{"event-only error", "error", `{"message":"private-model is unavailable","code":"private-model"}`},
			{"failed", "response.failed", `{"type":"response.failed","error":{"message":"private-model is unavailable","code":"private-model"}}`},
		} {
			t.Run(map[bool]string{false: "native", true: "passthrough"}[passthrough]+"/"+tc.name, func(t *testing.T) {
				rec := httptest.NewRecorder()
				c, _ := gin.CreateTestContext(rec)
				c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
				SetOpsUpstreamModel(c, "private-model")
				c.Set("ops_model", "public-model")
				body := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hello\"}\n\n" +
					"event: " + tc.eventType + "\ndata: " + tc.payload + "\n\n"
				resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
				svc := &OpenAIGatewayService{toolCorrector: NewCodexToolCorrector()}
				account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
				var err error
				if passthrough {
					_, err = svc.handleStreamingResponsePassthrough(context.Background(), resp, c, account, time.Now(), "public-model", "private-model")
				} else {
					_, err = svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "public-model", "private-model")
				}
				require.Error(t, err)
				require.Contains(t, rec.Body.String(), "Hello")
				require.NotContains(t, rec.Body.String(), "private-model")
			})
		}
	}
}

func TestResponsesStreamHidesModelInPassthroughRule(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, passthrough := range []bool{false, true} {
		t.Run(map[bool]string{false: "native", true: "passthrough"}[passthrough], func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			SetOpsUpstreamModel(c, "private-model")
			c.Set("ops_model", "public-model")
			rule := newNonFailoverPassthroughRule(http.StatusBadRequest, "context_length_exceeded", http.StatusBadRequest, "private-model custom error")
			rule.Platforms = []string{PlatformOpenAI}
			ruleService := &ErrorPassthroughService{}
			ruleService.setLocalCache([]*model.ErrorPassthroughRule{rule})
			BindErrorPassthroughService(c, ruleService)
			body := `event: response.failed

data: {"type":"response.failed","response":{"error":{"code":"context_length_exceeded","type":"invalid_request_error","message":"context_length_exceeded"}}}

`
			body = strings.Replace(body, "failed\n\ndata:", "failed\ndata:", 1)
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
			svc := &OpenAIGatewayService{toolCorrector: NewCodexToolCorrector()}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			var err error
			if passthrough {
				_, err = svc.handleStreamingResponsePassthrough(context.Background(), resp, c, account, time.Now(), "public-model", "private-model")
			} else {
				_, err = svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "public-model", "private-model")
			}
			require.Error(t, err)
			require.Equal(t, http.StatusBadGateway, rec.Code)
			require.NotContains(t, rec.Body.String(), "private-model")
			require.Contains(t, rec.Body.String(), UpstreamModelLeakClientMessage)
		})
	}
}

func TestResponsesStreamJuiceAndErrorPrivacyCompose(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetOpsUpstreamModel(c, "private-model")
	c.Set("ops_model", "public-model")
	SetJuiceResolvedValue(c, JuiceResolvedValue{Value: 99, OK: true})
	body := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Juice: 12.\"}\n\n" +
		"event: response.failed\ndata: {\"type\":\"response.failed\",\"error\":{\"message\":\"private-model error\"}}\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	svc := &OpenAIGatewayService{toolCorrector: NewCodexToolCorrector()}
	_, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, time.Now(), "public-model", "private-model")
	require.Error(t, err)
	require.Contains(t, rec.Body.String(), "Juice: 99")
	require.NotContains(t, rec.Body.String(), "Juice: 12")
	require.NotContains(t, rec.Body.String(), "private-model")
}
