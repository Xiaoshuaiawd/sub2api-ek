package service

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestJuiceChatPendingTextDoesNotCommitBeforeFailover(t *testing.T) {
	gin.SetMode(gin.TestMode)
	rec := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(rec)
	c.Request = httptest.NewRequest(http.MethodPost, "/v1/chat/completions", nil)
	SetJuiceResolvedValue(c, JuiceResolvedValue{Value: 99, OK: true})
	body := "data: {\"type\":\"response.created\",\"response\":{\"id\":\"resp_1\",\"model\":\"m\",\"status\":\"in_progress\",\"output\":[]}}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"Juice: 1\"}\n\n" +
		"data: {\"type\":\"response.output_text.delta\",\"delta\":\"2\"}\n\n" +
		"data: {\"type\":\"response.failed\",\"response\":{\"id\":\"resp_1\",\"status\":\"failed\",\"error\":{\"message\":\"An error occurred while processing your request.\"}}}\n\n"
	resp := &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader(body))}
	svc := &OpenAIGatewayService{cfg: &config.Config{}}
	_, err := svc.handleChatStreamingResponse(resp, c, &Account{ID: 1, Platform: PlatformOpenAI}, "m", "m", "m", time.Now(), 64)
	var failover *UpstreamFailoverError
	require.ErrorAs(t, err, &failover)
	require.False(t, c.Writer.Written())
	require.Empty(t, rec.Body.String())
}
