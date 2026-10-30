//go:build unit

package service

import (
	"bytes"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

func TestImageParsingClassifiesIntelligenceBeforeChannelValidation(t *testing.T) {
	gin.SetMode(gin.TestMode)
	group := int64(10)
	channel := Channel{ID: 1, Status: StatusActive, GroupIDs: []int64{group}, ModelMapping: map[string]map[string]string{"openai": {"gpt-image-2": "not-an-image-model"}}}
	svc := &OpenAIGatewayService{channelService: newTestChannelService(makeStandardRepo(channel, map[int64]string{group: PlatformOpenAI}))}

	for _, tc := range []struct {
		name, contentType, path string
		body                    []byte
		intelligence            bool
	}{
		{name: "json intelligence", contentType: "application/json", path: "/v1/images/generations", body: []byte(`{"model":"gpt-image-2","prompt":"画一只鹈鹕骑自行车"}`), intelligence: true},
		{name: "json ordinary", contentType: "application/json", path: "/v1/images/generations", body: []byte(`{"model":"gpt-image-2","prompt":"draw a cat"}`)},
		{name: "multipart intelligence", path: "/v1/images/edits", intelligence: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body, contentType := tc.body, tc.contentType
			if tc.name == "multipart intelligence" {
				var buf bytes.Buffer
				writer := multipart.NewWriter(&buf)
				require.NoError(t, writer.WriteField("model", "gpt-image-2"))
				require.NoError(t, writer.WriteField("prompt", "画一只鹈鹕骑自行车"))
				part, err := writer.CreateFormFile("image", "source.png")
				require.NoError(t, err)
				_, err = part.Write([]byte("fixture"))
				require.NoError(t, err)
				require.NoError(t, writer.Close())
				body, contentType = buf.Bytes(), writer.FormDataContentType()
			}
			c, _ := gin.CreateTestContext(httptest.NewRecorder())
			c.Request = httptest.NewRequest(http.MethodPost, tc.path, bytes.NewReader(body))
			c.Request.Header.Set("Content-Type", contentType)
			c.Set("api_key", &APIKey{GroupID: &group})
			_, err := svc.ParseOpenAIImagesRequest(c, body)
			if tc.intelligence {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			require.Equal(t, tc.intelligence, IsIntelligenceRouting(c.Request.Context()))
		})
	}
}
