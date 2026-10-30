package service

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestIntelligenceImageExtractionLatestUser(t *testing.T) {
	for _, tc := range []struct {
		name, body string
		images     []string
		wantErr    bool
	}{
		{"chat data url", `{"messages":[{"role":"user","content":[{"type":"text","text":"请看图"},{"type":"image_url","image_url":{"url":"data:image/png;base64,AQ=="}}]}]}`, []string{"data:image/png;base64,AQ=="}, false},
		{"ignore history image", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/old.png"}}]},{"role":"assistant","content":"ok"},{"role":"user","content":"hello"}]}`, nil, false},
		{"assistant image ignored", `{"messages":[{"role":"user","content":"hello"},{"role":"assistant","content":[{"type":"image_url","image_url":{"url":"https://example.com/out.png"}}]}]}`, nil, false},
		{"responses image", `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/test.png"}]}]}`, []string{"https://example.com/test.png"}, false},
		{"responses top level mixed blocks", `{"input":[{"type":"input_text","text":"请看图"},{"type":"input_image","image_url":"https://example.com/test.png"},{"type":"input_text","text":"这是什么"}]}`, []string{"https://example.com/test.png"}, false},
		{"websocket latest image", `{"type":"response.create","response":{"input":[{"role":"user","content":"old"},{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/test.png"}]}]}}`, []string{"https://example.com/test.png"}, false},
		{"system one latest image", `{"questions":{"q":{}},"state":{"context":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/old.png"}}]},{"role":"assistant","content":"ok"},{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://example.com/new.png"}}]}]}}`, []string{"https://example.com/new.png"}, false},
		{"system one structured input", `{"questions":{"q":{}},"state":{"context":"please inspect","attachment":{"type":"input_image","image_url":"https://example.com/new.png"}}}`, []string{"https://example.com/new.png"}, false},
		{"anthropic base64", `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"base64","media_type":"image/png","data":"AQ=="}}]}]}`, []string{"data:image/png;base64,AQ=="}, false},
		{"anthropic url", `{"messages":[{"role":"user","content":[{"type":"image","source":{"type":"url","url":"https://example.com/test.png"}}]}]}`, []string{"https://example.com/test.png"}, false},
		{"gemini inline data", `{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"image/png","data":"AQ=="}}]}]}`, []string{"data:image/png;base64,AQ=="}, false},
		{"gemini audio is not an image", `{"contents":[{"role":"user","parts":[{"inlineData":{"mimeType":"audio/wav","data":"AQ=="}}]}]}`, nil, false},
		{"gemini unsupported file", `{"contents":[{"role":"user","parts":[{"fileData":{"fileUri":"gs://example/file"}}]}]}`, nil, true},
		{"image file id unsupported", `{"input":[{"role":"user","content":[{"type":"input_image","file_id":"file-123"}]}]}`, nil, true},
		{"invalid base64", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"data:image/png;base64,%%%"}}]}]}`, nil, true},
		{"image url with credentials", `{"messages":[{"role":"user","content":[{"type":"image_url","image_url":{"url":"https://user:password@example.com/a.png"}}]}]}`, nil, true},
		{"too many images", `{"input":[{"role":"user","content":[{"type":"input_image","image_url":"https://example.com/a"},{"type":"input_image","image_url":"https://example.com/b"},{"type":"input_image","image_url":"https://example.com/c"},{"type":"input_image","image_url":"https://example.com/d"},{"type":"input_image","image_url":"https://example.com/e"}]}]}`, nil, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			images, err := ExtractIntelligenceLatestImages([]byte(tc.body))
			if tc.wantErr {
				require.Error(t, err)
				return
			}
			require.NoError(t, err)
			require.Equal(t, tc.images, images)
		})
	}
}
