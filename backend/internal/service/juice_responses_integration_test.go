package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestJuiceResponsesHTTPStreamingAndJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range []struct {
		name            string
		body            string
		stream, enabled bool
		want, unwanted  string
	}{
		{"stream split", "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Juice: 1\"}\n\nevent: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"2\"}\n\nevent: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[{\"type\":\"message\",\"content\":[{\"type\":\"output_text\",\"text\":\"Juice: 12\"}]}],\"usage\":{\"input_tokens\":2,\"output_tokens\":2}}}\n\n", true, true, "Juice: 99", "Juice: 12"},
		{"json", `{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"Juice: 12"}]}],"usage":{"input_tokens":2,"output_tokens":2}}`, false, true, "Juice: 99", "Juice: 12"},
		{"unconfigured", `{"id":"resp_1","output":[{"type":"message","content":[{"type":"output_text","text":"Juice: 12"}]}],"usage":{"input_tokens":2,"output_tokens":2}}`, false, false, "Juice: 12", "Juice: 99"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
			if tc.enabled {
				SetJuiceResolvedValue(c, JuiceResolvedValue{Value: 99, OK: true})
			}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(tc.body))}
			svc := &OpenAIGatewayService{toolCorrector: NewCodexToolCorrector()}
			account := &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}
			var err error
			if tc.stream {
				_, err = svc.handleStreamingResponse(context.Background(), resp, c, account, time.Now(), "m", "m")
			} else {
				_, err = svc.handleNonStreamingResponse(context.Background(), resp, c, account, "m", "m")
			}
			require.NoError(t, err)
			require.Contains(t, rec.Body.String(), tc.want)
			require.NotContains(t, rec.Body.String(), tc.unwanted)
		})
	}
}

func TestJuiceResponsesSSEToJSONRewritesReconstructedOutput(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetJuiceResolvedValue(c, JuiceResolvedValue{Value: 99, OK: true})
	body := "event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Juice: 12\",\"item_id\":\"item1\",\"output_index\":0,\"content_index\":0}\n\n" +
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"output\":[],\"usage\":{\"input_tokens\":2,\"output_tokens\":2}}}\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{"Content-Type": []string{"text/event-stream"}}, Body: io.NopCloser(strings.NewReader(body))}
	svc := &OpenAIGatewayService{toolCorrector: NewCodexToolCorrector()}
	_, err := svc.handleNonStreamingResponse(context.Background(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI, Type: AccountTypeAPIKey}, "m", "m")
	require.NoError(t, err)
	require.Contains(t, rec.Body.String(), "Juice: 99")
	require.NotContains(t, rec.Body.String(), "Juice: 12")
}

func TestJuiceResponsesBeforeOutputFailureKeepsFailoverSafe(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, body := range []string{
		"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\nevent: response.failed\ndata: {\"type\":\"response.failed\",\"error\":{\"message\":\"An error occurred while processing your request.\"}}\n\n",
		"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n",
	} {
		rec := httptest.NewRecorder()
		c, _ := gin.CreateTestContext(rec)
		c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
		SetJuiceResolvedValue(c, JuiceResolvedValue{Value: 99, OK: true})
		resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
		svc := &OpenAIGatewayService{toolCorrector: NewCodexToolCorrector()}
		_, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "m", "m")
		var failover *UpstreamFailoverError
		require.ErrorAs(t, err, &failover)
		require.False(t, c.Writer.Written())
		require.Empty(t, rec.Body.String())
	}
}

func TestJuiceResponsesPendingTextDoesNotCommitBeforeFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetJuiceResolvedValue(c, JuiceResolvedValue{Value: 99, OK: true})
	body := "event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\"}}\n\n" +
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Juice: 1\"}\n\n" +
		"event: response.failed\ndata: {\"type\":\"response.failed\",\"error\":{\"message\":\"An error occurred while processing your request.\"}}\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	svc := &OpenAIGatewayService{toolCorrector: NewCodexToolCorrector()}
	_, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "m", "m")
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
}

func TestJuiceResponsesSafePrefixFlushesBeforeUpstreamEOF(t *testing.T) {
	gin.SetMode(gin.TestMode)
	allowTerminal := make(chan struct{})
	terminalWaiting := make(chan struct{})
	reader := &stagedOpenAISSEReadCloser{
		segments: [][]byte{
			[]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"" + strings.Repeat("a", 300) + "\"}\n\n"),
			[]byte("event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"more text\"}\n\n"),
			[]byte("event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"resp_1\",\"usage\":{\"input_tokens\":2,\"output_tokens\":2}}}\n\n"),
		},
		gates: []<-chan struct{}{nil, nil, allowTerminal}, waiting: []chan struct{}{nil, nil, terminalWaiting},
	}
	recorder := newOpenAIResponseFlushRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/responses", nil)
	SetJuiceResolvedValue(c, JuiceResolvedValue{Value: 99, OK: true})
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: reader}
	svc := &OpenAIGatewayService{toolCorrector: NewCodexToolCorrector()}
	errCh := make(chan error, 1)
	go func() {
		_, err := svc.handleStreamingResponse(context.Background(), resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, time.Now(), "m", "m")
		errCh <- err
	}()
	defer func() {
		close(allowTerminal)
		require.NoError(t, <-errCh)
	}()
	waitOpenAIResponseFlushSignal(t, terminalWaiting)
	waitOpenAIResponseFlushCount(t, recorder, 1)
	body, _ := recorder.snapshot()
	require.Contains(t, body, strings.Repeat("a", 128))
}
