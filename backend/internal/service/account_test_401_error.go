package service

import "encoding/json"

// Only a top-level structured auth code is evidence that a 401 invalidated
// account credentials. Request-scoped provider codes retain their old error
// text and are not eligible for automatic re-login.
func openAIAccountTest401Error(body []byte) string {
	var envelope struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if json.Unmarshal(body, &envelope) == nil && isGuardAuthCode(envelope.Error.Code) {
		return "Unauthorized (401): account authentication failed"
	}
	return "Authentication failed (401): " + string(body)
}
