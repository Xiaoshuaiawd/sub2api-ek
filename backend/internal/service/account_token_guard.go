package service

import (
	"encoding/json"
	"strings"
)

// AccountTokenGuardReloginAccount keeps inventory callers source compatible
// while explicit imports use OpenAIReloginService and periodic recovery uses V2.
// No legacy background guard or plaintext credential enrollment remains.
type AccountTokenGuardReloginAccount = OpenAIReloginCredentials

func guardAccountHasAuthFailure(account *Account) bool {
	if account.Platform != PlatformOpenAI || !account.IsOAuth() || account.Status != StatusError {
		return false
	}
	if strings.HasPrefix(account.ErrorMessage, "Token revoked (401):") ||
		strings.HasPrefix(account.ErrorMessage, "Unauthorized (401):") {
		return true
	}
	// The account test path persists the structured upstream body under this
	// prefix. Require an explicit auth code, never a substring in its message.
	raw, ok := strings.CutPrefix(account.ErrorMessage, "Authentication failed (401):")
	if !ok {
		return false
	}
	var result struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	return json.Unmarshal([]byte(raw), &result) == nil && isGuardAuthCode(result.Error.Code)
}

func isGuardAuthCode(code string) bool {
	switch strings.ToLower(strings.TrimSpace(code)) {
	case "auth_failed", "invalid_token", "token_invalid", "requires_relogin", "invalid_grant", "revoked",
		"token_invalidated", "token_revoked", "refresh_token_reused", "refresh_token_invalidated":
		return true
	default:
		return false
	}
}
