package service

import (
	"context"
	"encoding/json"
)

// ClassifyIntelligenceRequest checks only text from the latest user message.
// Images remain untouched and are forwarded to the chosen upstream account.
func ClassifyIntelligenceRequest(_ context.Context, body []byte, rules IntelligenceRules) ([]IntelligenceRuleMatch, error) {
	var request map[string]any
	if json.Unmarshal(body, &request) != nil {
		return nil, nil // normal request validation reports malformed JSON
	}
	return rules.MatchAll(intelligenceLatestUserText(request)), nil
}

func (s *OpenAIGatewayService) IntelligenceRuleSettings() *SettingService {
	if s == nil {
		return nil
	}
	return s.settingService
}
