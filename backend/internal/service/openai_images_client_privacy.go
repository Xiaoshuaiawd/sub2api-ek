package service

import (
	"bytes"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// rewriteOpenAIImagesClientPayload only changes protocol metadata and errors;
// the upstream payload remains available for billing and account diagnostics.
func rewriteOpenAIImagesClientPayload(c *gin.Context, payload []byte, eventType string) []byte {
	if !gjson.ValidBytes(payload) {
		return payload
	}
	out := payload
	if model := gjson.GetBytes(out, "model"); model.Exists() && model.Type == gjson.String {
		if clientModel := gatewayClientResponseModel(c, ""); clientModel != "" {
			if rewritten, err := sjson.SetBytes(out, "model", clientModel); err == nil {
				out = rewritten
			}
		}
	}
	return SanitizeUpstreamErrorEvent(c, out, eventType)
}

// A data field can span multiple physical lines; preserve all other SSE fields
// and only reframe data when a model/error was actually rewritten.
func rewriteOpenAIImagesClientSSEFrame(c *gin.Context, lines [][]byte) []byte {
	var dataLines []string
	firstData := -1
	eventType := ""
	for i, raw := range lines {
		line := strings.TrimRight(string(raw), "\r\n")
		if event, ok := extractOpenAISSEEventLine(line); ok {
			eventType = strings.TrimSpace(event)
		}
		if data, ok := extractOpenAISSEDataLine(line); ok {
			if firstData < 0 {
				firstData = i
			}
			dataLines = append(dataLines, data)
		}
	}
	if firstData < 0 {
		raw := bytes.Join(lines, nil)
		trimmed := bytes.TrimSpace(raw)
		if gjson.ValidBytes(trimmed) {
			updated := rewriteOpenAIImagesClientPayload(c, trimmed, eventType)
			if !bytes.Equal(updated, trimmed) {
				start := bytes.Index(raw, trimmed)
				out := append([]byte(nil), raw[:start]...)
				out = append(out, updated...)
				return append(out, raw[start+len(trimmed):]...)
			}
		}
		return raw
	}
	joined := []byte(strings.Join(dataLines, "\n"))
	if !gjson.ValidBytes(joined) {
		// A malformed or plain-text event can still contain the upstream model.
		// Do not forward a partial frame that cannot be safely rewritten.
		for _, data := range dataLines {
			if !gjson.Valid(data) && UpstreamErrorMessageLeaksModelForRequest(c, data) {
				return []byte("event: error\ndata: {\"type\":\"error\",\"error\":{\"type\":\"upstream_error\",\"message\":\"Upstream service temporarily unavailable\"}}\n\n")
			}
		}
		// Some providers emit independent JSON documents as separate data lines.
		var out bytes.Buffer
		for _, raw := range lines {
			line := strings.TrimRight(string(raw), "\r\n")
			if data, ok := extractOpenAISSEDataLine(line); ok && gjson.Valid(data) {
				updated := rewriteOpenAIImagesClientPayload(c, []byte(data), eventType)
				if !bytes.Equal(updated, []byte(data)) {
					out.WriteString("data: ")
					out.Write(updated)
					out.WriteByte('\n')
					continue
				}
			}
			out.Write(raw)
		}
		return out.Bytes()
	}
	updated := rewriteOpenAIImagesClientPayload(c, joined, eventType)
	if bytes.Equal(updated, joined) {
		return bytes.Join(lines, nil)
	}
	var out bytes.Buffer
	for i, raw := range lines {
		line := strings.TrimRight(string(raw), "\r\n")
		if _, ok := extractOpenAISSEDataLine(line); ok {
			if i == firstData {
				out.WriteString("data: ")
				out.Write(updated)
				out.WriteByte('\n')
			}
			continue
		}
		out.Write(raw)
	}
	return out.Bytes()
}
