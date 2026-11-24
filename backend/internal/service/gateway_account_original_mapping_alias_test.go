//go:build unit

package service

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGenericAccountOriginalMappingPreservesShortOAuthSource(t *testing.T) {
	const source = "claude-sonnet-4-5"
	const channel = "claude-haiku-4-5-20251001"
	const target = "claude-opus-4-6"
	groupID := int64(7)
	ctx := WithAccountModelMapping(context.Background(), source, channel)
	newAccount := func() Account {
		return Account{ID: 1, Platform: PlatformAnthropic, Type: AccountTypeOAuth,
			Credentials: map[string]any{"model_mapping": map[string]any{source: target}},
			GroupIDs:    []int64{groupID}, AccountGroups: []AccountGroup{{GroupID: groupID, AllowedModels: []string{source}}},
		}
	}
	svc := &GatewayService{}
	account := newAccount()
	require.True(t, svc.isModelSupportedByAccountInGroup(ctx, &account, &groupID, channel), "an explicit raw A rule must be checked before provider normalization replaces its short source")
	account = newAccount()
	account.AccountGroups[0].AllowedModels = []string{"unrelated"}
	require.False(t, svc.isModelSupportedByAccountInGroup(ctx, &account, &groupID, channel), "priority does not bypass account binding scope")
	account = newAccount()
	account.Credentials[OAuthInitialModelSelectionKey] = []string{source}
	require.False(t, svc.isModelSupportedByAccountInGroup(ctx, &account, &groupID, channel), "the configured target must remain inside initial scope")
	account = newAccount()
	account.Credentials[OAuthInitialModelSelectionKey] = []string{source, target}
	require.True(t, svc.isModelSupportedByAccountInGroup(ctx, &account, &groupID, channel))
	account = newAccount()
	account.Credentials["model_mapping"] = map[string]any{channel: channel}
	account.AccountGroups[0].AllowedModels = []string{channel}
	require.True(t, svc.isModelSupportedByAccountInGroup(ctx, &account, &groupID, channel), "raw source miss keeps the existing channel/provider fallback")
}
