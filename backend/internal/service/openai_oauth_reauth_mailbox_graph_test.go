package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/reauthruntime"
	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthReauthMailboxGraphForcesRemoteEngine(t *testing.T) {
	for _, global := range []string{"", "local_worker", "session_studio"} {
		require.Equal(t, "session_studio", effectiveReauthEngine("mailbox_graph", "session_studio", global))
	}
}

func TestOpenAIOAuthReauthMailboxGraphEncryptedRoundTripAndDirectCredentials(t *testing.T) {
	for _, fallback := range []bool{false, true} {
		t.Run(map[bool]string{false: "mailbox only", true: "password fallback"}[fallback], func(t *testing.T) {
			svc, reader, repo, updater, _, _ := newReauthTestService("acct-1")
			svc.settings = &reauthRuntimeTestSettings{values: map[string]string{openAIOAuthReauthRuntimeSettingsKey: `{"engine":"local_worker","worker_concurrency":3}`}}
			reader.account.Extra = map[string]any{OpenAIProxyModeKey: OpenAIProxyModeRequest}
			input := OpenAIOAuthReauthConfigInput{
				LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModeMailboxGraph,
				MailClientID: "mail-client", MailRefreshToken: "mail-refresh-secret",
				ProxySource: OpenAIOAuthReauthProxySourceManagedProxy, ProxyID: int64Ptr(99),
			}
			if fallback {
				input.Password, input.TOTPSecret = "password-secret", "totp-secret"
			}
			view, err := svc.SaveCredentialConfig(context.Background(), 42, input)
			require.NoError(t, err)
			require.True(t, view.Configured)
			require.True(t, view.MailboxConfigured)
			require.Equal(t, fallback, view.PasswordConfigured)
			require.Equal(t, OpenAIOAuthReauthEngineSessionStudio, view.Engine)
			require.Equal(t, OpenAIOAuthReauthProxySourceAccount, view.ProxySource)
			require.Nil(t, view.ProxyID)
			plain, err := svc.encryptor.Decrypt(repo.config.MailboxCredentialsCiphertext)
			require.NoError(t, err)
			require.JSONEq(t, `{"mail_client_id":"mail-client","mail_refresh_token":"mail-refresh-secret"}`, plain)
			require.Empty(t, repo.config.OTPURLCiphertext)
			safe, err := json.Marshal(view)
			require.NoError(t, err)
			for _, secret := range []string{"mail-client", "mail-refresh-secret", "password-secret", "totp-secret", "ciphertext"} {
				require.NotContains(t, string(safe), secret)
			}
			_, err = svc.CreateTask(context.Background(), 42)
			require.NoError(t, err)
			// Built-in workers support this remote mode without taking legacy
			// email_otp_url jobs that require a different protocol runtime.
			svc.worker = reauthruntime.New(t.TempDir(), "test", "http://127.0.0.1:8080", "test-token")
			claim, err := svc.ClaimTaskWithEngines(context.Background(), "worker", []string{OpenAIOAuthReauthEngineLocal})
			require.NoError(t, err)
			require.Nil(t, claim)
			claim, err = svc.ClaimTaskWithEngines(context.Background(), "worker", []string{OpenAIOAuthReauthEngineSessionStudio})
			require.NoError(t, err)
			require.NotNil(t, claim)
			require.Equal(t, "mail-client", claim.MailClientID)
			require.Equal(t, "mail-refresh-secret", claim.MailRefreshToken)
			require.Equal(t, "old-refresh", claim.RefreshToken)
			require.Empty(t, claim.ProxyURL)
			require.Equal(t, input.Password, claim.Password)
			require.Equal(t, input.TOTPSecret, claim.TOTPSecret)
			result, err := svc.SubmitCredentials(context.Background(), claim.TaskID, "worker", directReauthCredentials("acct-1", "user-1", "user@example.com"), nil)
			require.NoError(t, err)
			require.Equal(t, OpenAIOAuthReauthStatusSucceeded, result.Status)
			require.True(t, updater.applied)
		})
	}
}

func TestOpenAIOAuthReauthMailboxGraphPreservesAndClearsSecretsByMode(t *testing.T) {
	svc, _, repo, _, _, _ := newReauthTestService("acct-1")
	ctx := context.Background()
	_, err := svc.SaveCredentialConfig(ctx, 42, OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModeMailboxGraph, MailClientID: "client", MailRefreshToken: "refresh", Password: "fallback", TOTPSecret: "totp"})
	require.NoError(t, err)
	bundle := repo.config.MailboxCredentialsCiphertext
	_, err = svc.SaveCredentialConfig(ctx, 42, OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModeMailboxGraph})
	require.NoError(t, err)
	require.Equal(t, bundle, repo.config.MailboxCredentialsCiphertext)
	require.NotEmpty(t, repo.config.PasswordCiphertext)
	_, err = svc.SaveCredentialConfig(ctx, 42, OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModeMailboxGraph, ClearPassword: true, ClearTOTP: true})
	require.NoError(t, err)
	require.Empty(t, repo.config.PasswordCiphertext)
	require.Empty(t, repo.config.TOTPSecretCiphertext)
	_, err = svc.SaveCredentialConfig(ctx, 42, OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModePasswordTOTP, Password: "new-password"})
	require.NoError(t, err)
	require.Empty(t, repo.config.MailboxCredentialsCiphertext)
	_, err = svc.SaveCredentialConfig(ctx, 42, OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModeMailboxGraph})
	require.Error(t, err)
}

func TestOpenAIOAuthReauthMailboxGraphRejectsPartialCredentialsAndWrongIdentity(t *testing.T) {
	svc, _, _, updater, _, _ := newReauthTestService("acct-1")
	ctx := context.Background()
	for _, pair := range [][2]string{{"", ""}, {"client", ""}, {"", "refresh"}} {
		_, err := svc.SaveCredentialConfig(ctx, 42, OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModeMailboxGraph, MailClientID: pair[0], MailRefreshToken: pair[1]})
		require.Error(t, err)
	}
	_, err := svc.SaveCredentialConfig(ctx, 42, OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModeMailboxGraph, MailClientID: "client", MailRefreshToken: "refresh"})
	require.NoError(t, err)
	for _, profile := range []string{"excel", "siwc"} {
		_, err = svc.CreateTaskForProfile(ctx, 42, profile)
		require.Error(t, err)
	}
	_, err = svc.CreateTask(ctx, 42)
	require.NoError(t, err)
	claim, err := svc.ClaimTaskWithEngines(ctx, "worker", []string{OpenAIOAuthReauthEngineSessionStudio})
	require.NoError(t, err)
	require.NotNil(t, claim)
	_, err = svc.SubmitCredentials(ctx, claim.TaskID, "worker", directReauthCredentials("wrong-account", "other-user", "wrong@example.com"), nil)
	require.Error(t, err)
	require.False(t, updater.applied)
}
