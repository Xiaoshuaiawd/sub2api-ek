package service

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/reauthruntime"
	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthReauthEngineSelectionAndLegacyWorker(t *testing.T) {
	ctx := context.Background()
	svc, _, repo, _, _, _ := newReauthTestService("acct-1")
	input := OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModePasswordTOTP, Password: "password-secret", TOTPSecret: "totp-secret"}
	view, err := svc.SaveCredentialConfig(ctx, 42, input)
	require.NoError(t, err)
	require.Equal(t, OpenAIOAuthReauthEngineLocal, view.Engine)
	input.Engine = OpenAIOAuthReauthEngineSessionStudio
	input.Password = ""
	input.TOTPSecret = ""
	view, err = svc.SaveCredentialConfig(ctx, 42, input)
	require.NoError(t, err)
	require.Equal(t, OpenAIOAuthReauthEngineSessionStudio, view.Engine)
	require.True(t, view.PasswordConfigured)
	require.True(t, view.TOTPConfigured)
	// Existing clients that merely pause an account must preserve its engine.
	input.Engine = ""
	view, err = svc.SaveCredentialConfig(ctx, 42, input)
	require.NoError(t, err)
	require.Equal(t, OpenAIOAuthReauthEngineSessionStudio, view.Engine)
	_, err = svc.CreateTask(ctx, 42)
	require.NoError(t, err)
	claim, err := svc.ClaimTask(ctx, "legacy")
	require.NoError(t, err)
	require.Nil(t, claim)
	require.Equal(t, OpenAIOAuthReauthStatusQueued, repo.task.Status)
	claim, err = svc.ClaimTaskWithEngines(ctx, "modern", []string{OpenAIOAuthReauthEngineLocal, OpenAIOAuthReauthEngineSessionStudio})
	require.NoError(t, err)
	require.Nil(t, claim)
	require.Equal(t, OpenAIOAuthReauthStatusFailed, repo.task.Status)
	require.Equal(t, "Session Studio cannot guarantee WARP egress", repo.task.Error)
}

func TestOpenAIOAuthReauthEngineConfigurationUsesGuardSettings(t *testing.T) {
	svc, _, _, _, _, _ := newReauthTestService("acct-1")
	svc.settings = &accountTokenGuardV2TestSettings{raw: "{\"relogin_endpoint\":\"https://login.example/api/relogin\",\"relogin_headers\":{\"X-Service-Key\":\"synthetic-service-secret\"}}"}
	ctx := context.Background()
	view, err := svc.SaveCredentialConfig(ctx, 42, OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModePasswordTOTP, Engine: OpenAIOAuthReauthEngineSessionStudio, Password: "secret"})
	require.NoError(t, err)
	safe, err := json.Marshal(view)
	require.NoError(t, err)
	require.NotContains(t, string(safe), "synthetic-service-secret")
	_, err = svc.CreateTask(ctx, 42)
	require.NoError(t, err)
	claim, err := svc.ClaimTaskWithEngines(ctx, "modern", []string{OpenAIOAuthReauthEngineSessionStudio})
	require.NoError(t, err)
	require.Nil(t, claim)
}

func TestOpenAIOAuthReauthEngineRejectsInvalidModeAndConfiguration(t *testing.T) {
	for _, engine := range []string{"unknown", "session_studio"} {
		svc, _, _, _, _, _ := newReauthTestService("acct-1")
		_, err := svc.SaveCredentialConfig(context.Background(), 42, OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModeEmailOTPURL, Engine: engine, OTPURL: "https://mail.example/latest"})
		require.Error(t, err)
	}
	for _, raw := range []string{
		"{broken",
		"{\"relogin_endpoint\":\"http://login.example/\"}",
		"{\"relogin_endpoint\":\"https://user:pass@login.example/\"}",
		"{\"relogin_headers\":{\"X-OpenAI-Reauth-Worker-Token\":\"secret\"}}",
	} {
		svc, _, _, _, _, _ := newReauthTestService("acct-1")
		svc.settings = &accountTokenGuardV2TestSettings{raw: raw}
		_, err := svc.SaveCredentialConfig(context.Background(), 42, OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModePasswordTOTP, Engine: OpenAIOAuthReauthEngineSessionStudio, Password: "secret"})
		require.Error(t, err)
		require.NotContains(t, err.Error(), "user:pass")
		require.NotContains(t, err.Error(), "{broken")
	}
}

func TestOpenAIOAuthReauthEngineRemoteDoesNotAcquireLocalProxy(t *testing.T) {
	svc, _, _, _, _, _ := newReauthTestService("acct-1")
	acquired := false
	svc.acquireMihomoProxy = func(context.Context, string) (string, func(), error) {
		acquired = true
		return "http://127.0.0.1:9999", func() {}, nil
	}
	ctx := context.Background()
	_, err := svc.SaveCredentialConfig(ctx, 42, OpenAIOAuthReauthConfigInput{LoginEmail: "user@example.com", CredentialMode: OpenAIOAuthReauthModePasswordTOTP, Engine: OpenAIOAuthReauthEngineSessionStudio, ProxySource: OpenAIOAuthReauthProxySourceMihomo, Password: "secret"})
	require.NoError(t, err)
	_, err = svc.CreateTask(ctx, 42)
	require.NoError(t, err)
	claim, err := svc.ClaimTaskWithEngines(ctx, "modern", []string{OpenAIOAuthReauthEngineSessionStudio})
	require.NoError(t, err)
	require.Nil(t, claim)
	require.False(t, acquired)
}

func TestOpenAIOAuthReauthEngineManagedClaimRetainsPasswordScope(t *testing.T) {
	svc, _, _, _, _, _ := newReauthTestService("acct-1")
	ctx := context.Background()
	_, err := svc.SaveConfig(ctx, 42, "user@example.com", "https://mail.example/latest")
	require.NoError(t, err)
	_, err = svc.CreateTask(ctx, 42)
	require.NoError(t, err)
	svc.worker = reauthruntime.New(t.TempDir(), "1.0.0", "http://127.0.0.1:8080", "test")
	claim, err := svc.ClaimTaskWithEngines(ctx, "managed", []string{OpenAIOAuthReauthEngineLocal, OpenAIOAuthReauthEngineSessionStudio})
	require.NoError(t, err)
	require.Nil(t, claim)
}
