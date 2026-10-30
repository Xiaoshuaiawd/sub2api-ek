package service

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/url"
	"strings"
)

const intelligenceImageMaxBytes = 8 << 20
const intelligenceImageMaxCount = 4

var ErrIntelligenceImageUnsupported = errors.New("image cannot be processed for intelligence routing")

// ExtractIntelligenceLatestImages returns image references from only the latest
// user message. It never downloads image URLs or examines earlier messages.
func ExtractIntelligenceLatestImages(body []byte) ([]string, error) {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return nil, nil // normal request validation reports malformed JSON
	}
	var input any
	_, systemOne := request["questions"].(map[string]any)
	if systemOne {
		if state, exists := request["state"]; exists {
			input = latestIntelligenceSystemOneImages(state)
		}
	}
	if request["type"] == "response.create" {
		if response, ok := request["response"].(map[string]any); ok {
			input = response["input"]
		}
	}
	if input == nil {
		for _, key := range []string{"messages", "contents", "input", "prompt"} {
			if value, ok := request[key]; ok {
				input = value
				break
			}
		}
	}
	latest := latestIntelligenceImageInput(input)
	if systemOne {
		latest = input
	}
	var images []string
	var walk func(any) error
	appendURL := func(raw string) error {
		raw = strings.TrimSpace(raw)
		if raw == "" || len(images) >= intelligenceImageMaxCount {
			return ErrIntelligenceImageUnsupported
		}
		if strings.HasPrefix(strings.ToLower(raw), "data:") {
			header, payload, ok := strings.Cut(raw, ",")
			if !ok || !supportedIntelligenceImageData(header, payload) {
				return ErrIntelligenceImageUnsupported
			}
		} else {
			parsed, err := url.Parse(raw)
			if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") || parsed.Host == "" || parsed.User != nil {
				return ErrIntelligenceImageUnsupported
			}
		}
		images = append(images, raw)
		return nil
	}
	walk = func(value any) error {
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				if err := walk(item); err != nil {
					return err
				}
			}
		case map[string]any:
			if role, ok := v["role"].(string); ok && role != "user" {
				return nil
			}
			typ, _ := v["type"].(string)
			switch typ {
			case "image_url", "input_image":
				raw, _ := v["image_url"].(string)
				if ref, ok := v["image_url"].(map[string]any); ok {
					raw, _ = ref["url"].(string)
				}
				return appendURL(raw)
			case "image":
				if source, ok := v["source"].(map[string]any); ok {
					switch source["type"] {
					case "base64":
						mime, _ := source["media_type"].(string)
						data, _ := source["data"].(string)
						return appendURL("data:" + mime + ";base64," + data)
					case "url":
						raw, _ := source["url"].(string)
						return appendURL(raw)
					}
				}
				return ErrIntelligenceImageUnsupported
			}
			for _, key := range []string{"inlineData", "inline_data"} {
				if inline, ok := v[key].(map[string]any); ok {
					mime, _ := inline["mimeType"].(string)
					if mime == "" {
						mime, _ = inline["mime_type"].(string)
					}
					if mime != "" && !strings.HasPrefix(strings.ToLower(mime), "image/") {
						return nil
					}
					data, _ := inline["data"].(string)
					return appendURL("data:" + mime + ";base64," + data)
				}
			}
			for _, key := range []string{"fileData", "file_data"} {
				if file, ok := v[key].(map[string]any); ok {
					mime, _ := file["mimeType"].(string)
					if mime == "" {
						mime, _ = file["mime_type"].(string)
					}
					if mime != "" && !strings.HasPrefix(strings.ToLower(mime), "image/") {
						return nil
					}
					raw, _ := file["fileUri"].(string)
					if raw == "" {
						raw, _ = file["file_uri"].(string)
					}
					return appendURL(raw)
				}
			}
			keys := []string{"content", "parts", "context", "messages", "input"}
			if systemOne {
				keys = keys[:0]
				for key := range v {
					keys = append(keys, key)
				}
			}
			for _, key := range keys {
				if err := walk(v[key]); err != nil {
					return err
				}
			}
		}
		return nil
	}
	if err := walk(latest); err != nil {
		return nil, err
	}
	return images, nil
}

func latestIntelligenceSystemOneImages(state any) any {
	if conversation, ok := intelligenceSystemOneImageConversation(state); ok {
		return conversation
	}
	return state
}

func intelligenceSystemOneImageConversation(state any) (any, bool) {
	switch value := state.(type) {
	case map[string]any:
		if _, ok := value["role"]; ok {
			return latestIntelligenceImageInput(value), true
		}
		for _, key := range []string{"messages", "contents", "input", "context"} {
			if nested, ok := value[key]; ok {
				if selected, found := intelligenceSystemOneImageConversation(nested); found {
					return selected, true
				}
			}
		}
	case []any:
		for _, item := range value {
			if m, ok := item.(map[string]any); ok {
				if _, hasRole := m["role"]; hasRole {
					return latestIntelligenceImageInput(value), true
				}
			}
		}
	}
	return nil, false
}

func latestIntelligenceImageInput(value any) any {
	switch v := value.(type) {
	case string:
		return nil
	case map[string]any:
		role, _ := v["role"].(string)
		if role != "" && role != "user" {
			return nil
		}
		typ, _ := v["type"].(string)
		if typ != "" && typ != "message" && typ != "text" && typ != "input_text" && typ != "input_image" && typ != "image_url" {
			return nil
		}
		return v
	case []any:
		for i := len(v) - 1; i >= 0; i-- {
			switch item := v[i].(type) {
			case string:
				return nil
			case map[string]any:
				role, _ := item["role"].(string)
				if role != "" && role != "user" {
					continue
				}
				typ, _ := item["type"].(string)
				if typ != "" && typ != "message" && typ != "text" && typ != "input_text" && typ != "input_image" && typ != "image_url" {
					continue
				}
				if role == "" && (typ == "input_text" || typ == "text" || typ == "input_image" || typ == "image_url") {
					start := i
					for start > 0 {
						prev, ok := v[start-1].(map[string]any)
						if !ok || prev["role"] != nil {
							break
						}
						pt, _ := prev["type"].(string)
						if pt != "input_text" && pt != "text" && pt != "input_image" && pt != "image_url" {
							break
						}
						start--
					}
					return v[start : i+1]
				}
				return item
			}
		}
	}
	return nil
}

func supportedIntelligenceImageData(header, payload string) bool {
	header = strings.ToLower(header)
	if header != "data:image/png;base64" && header != "data:image/jpeg;base64" && header != "data:image/webp;base64" && header != "data:image/gif;base64" {
		return false
	}
	if payload == "" || len(payload) > base64.StdEncoding.EncodedLen(intelligenceImageMaxBytes) {
		return false
	}
	decoded, err := base64.StdEncoding.DecodeString(payload)
	return err == nil && len(decoded) > 0 && len(decoded) <= intelligenceImageMaxBytes
}
