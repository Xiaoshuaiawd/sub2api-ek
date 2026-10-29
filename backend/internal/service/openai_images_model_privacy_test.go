package service

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
	"github.com/tidwall/gjson"
)

func TestImagesClientModelAndErrorPrivacy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			SetGatewayClientModel(c, "public-image")
			SetOpsUpstreamModel(c, "private-image")
			var body string
			if stream {
				body = "event: image_generation.completed\ndata: {\"type\":\"image_generation.completed\",\"model\":\"private-image\",\ndata: \"b64_json\":\"aGVsbG8=\",\"usage\":{\"input_tokens\":7}}\n\n"
			} else {
				body = `{"model":"private-image","data":[{"b64_json":"aGVsbG8="}],"usage":{"input_tokens":7}}`
			}
			resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
			svc := &OpenAIGatewayService{}
			if stream {
				_, _, _, _, err := svc.handleOpenAIImagesStreamingResponse(resp, c, time.Now(), nil)
				require.NoError(t, err)
			} else {
				_, _, _, err := svc.handleOpenAIImagesNonStreamingResponse(context.Background(), resp, c, &Account{}, &OpenAIImagesRequest{Model: "channel-image"})
				require.NoError(t, err)
			}
			require.Contains(t, rec.Body.String(), "public-image")
			require.NotContains(t, rec.Body.String(), "private-image")
			require.Contains(t, rec.Body.String(), "aGVsbG8=")
		})
	}
}

func TestImagesClientErrorPrivacy(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, stream := range []bool{false, true} {
		t.Run(map[bool]string{false: "json", true: "sse"}[stream], func(t *testing.T) {
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
			SetGatewayClientModel(c, "public-image")
			SetOpsUpstreamModel(c, "private-image")
			if stream {
				body := "event: error\ndata: {\"type\":\"error\",\"error\":{\"message\":\"private-image unavailable\"}}\n\n"
				resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
				_, _, _, _, _ = (&OpenAIGatewayService{}).handleOpenAIImagesStreamingResponse(resp, c, time.Now(), nil)
			} else {
				writeOpenAIImagesUpstreamErrorResponse(c, &OpenAIImagesUpstreamError{StatusCode: 400, Message: "private-image unavailable"})
				require.Equal(t, http.StatusBadGateway, rec.Code)
			}
			require.NotContains(t, rec.Body.String(), "private-image")
			require.Contains(t, rec.Body.String(), UpstreamModelLeakClientMessage)
		})
	}
}

func TestImagesWithoutModelDoesNotAddModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	SetGatewayClientModel(c, "public-image")
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(`{"data":[{"b64_json":"aGVsbG8="}]}`))}
	_, _, _, err := (&OpenAIGatewayService{}).handleOpenAIImagesNonStreamingResponse(context.Background(), resp, c, &Account{}, &OpenAIImagesRequest{Model: "public-image"})
	require.NoError(t, err)
	require.False(t, gjson.Get(rec.Body.String(), "model").Exists())
}

func TestImagesClientSSEEventLimitDoesNotEmitPartialFrame(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	SetGatewayClientModel(c, "public-image")
	body := "event: image_generation.completed\ndata: {\"model\":\"private-image\",\"b64_json\":\"" + strings.Repeat("a", 256) + "\"}\n"
	resp := &http.Response{StatusCode: 200, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	svc := &OpenAIGatewayService{cfg: &config.Config{Gateway: config.GatewayConfig{UpstreamResponseReadMaxBytes: 128}}}
	_, _, _, _, err := svc.handleOpenAIImagesStreamingResponse(resp, c, time.Now(), nil)
	require.ErrorIs(t, err, ErrUpstreamResponseBodyTooLarge)
	require.Empty(t, rec.Body.String())
}

func TestImagesSSEPlainTextErrorHidesUpstreamModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	SetGatewayClientModel(c, "public-image")
	SetOpsUpstreamModel(c, "private-image")
	frame := [][]byte{[]byte("event: error\n"), []byte("data: private-image unavailable\n"), []byte("\n")}
	out := string(rewriteOpenAIImagesClientSSEFrame(c, frame))
	require.NotContains(t, out, "private-image")
	require.Contains(t, out, UpstreamModelLeakClientMessage)
}

func TestImagesSSETruncatedFrameHidesUpstreamModel(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/images/generations", nil)
	SetGatewayClientModel(c, "public-image")
	SetOpsUpstreamModel(c, "private-image")
	frame := [][]byte{[]byte("event: image_generation.completed\n"), []byte("data: {\"model\":\"private-image\"")}
	out := string(rewriteOpenAIImagesClientSSEFrame(c, frame))
	require.NotContains(t, out, "private-image")
	require.Contains(t, out, UpstreamModelLeakClientMessage)
}
