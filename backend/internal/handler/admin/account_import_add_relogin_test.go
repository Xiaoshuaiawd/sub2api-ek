//go:build unit

package admin

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type addAccountReloginSettings struct {
	service.SettingRepository
	raw string
}

func (s *addAccountReloginSettings) GetValue(context.Context, string) (string, error) {
	return s.raw, nil
}

type addAccountGuardWriter struct {
	id    int64
	input service.AccountTokenGuardV2AccountInput
	err   error
}

func (w *addAccountGuardWriter) SaveAccount(_ context.Context, id int64, input service.AccountTokenGuardV2AccountInput) (*service.AccountTokenGuardV2Account, error) {
	w.id, w.input = id, input
	return &service.AccountTokenGuardV2Account{AccountID: id}, w.err
}

func runAddAccountImport(t *testing.T, guard accountTokenGuardV2Writer, info string, encryptionReady ...bool) (*httptest.ResponseRecorder, *codexImportMemoryAdminService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"credential":{"access_token":"access","refresh_token":"refresh","id_token":"id"}}`)
	}))
	t.Cleanup(server.Close)
	raw, err := json.Marshal(map[string]any{"relogin_endpoint": server.URL})
	require.NoError(t, err)
	relogin := service.NewOpenAIReloginService(&addAccountReloginSettings{raw: string(raw)})
	t.Cleanup(relogin.Stop)
	admin := newCodexImportMemoryAdminService(nil)
	ready := len(encryptionReady) == 0 || encryptionReady[0]
	serde := service.NewOpenAIOAuthReauthService(nil, nil, nil, nil, nil, ready, nil, nil)
	h := &AccountHandler{adminService: admin, relogin: relogin, inventoryTokenGuardV2: guard, openaiOAuthReauth: serde}
	recorder := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(recorder)
	body, err := json.Marshal(AddAccountImportRequest{AccountInfo: info})
	require.NoError(t, err)
	ctx.Request = httptest.NewRequest(http.MethodPost, "/import/add", strings.NewReader(string(body)))
	ctx.Request.Header.Set("Content-Type", "application/json")
	h.ImportAddAccount(ctx)
	return recorder, admin
}

func TestAddAccountImportEnrollsEncryptedReloginCredentials(t *testing.T) {
	guard := &addAccountGuardWriter{}
	recorder, admin := runAddAccountImport(t, guard, "USER@example.com----password-canary----totp-canary")
	require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
	require.NotEmpty(t, admin.accounts)
	require.Equal(t, admin.accounts[0].ID, guard.id)
	require.Equal(t, "user@example.com", guard.input.LoginEmail)
	require.Equal(t, "password-canary", guard.input.Password)
	require.Equal(t, "totp-canary", guard.input.TOTPSecret)
	require.Equal(t, service.OpenAIOAuthReauthModePasswordTOTP, guard.input.CredentialMode)
	require.True(t, guard.input.PreserveEnabled)
	require.True(t, guard.input.PreserveAutoRelogin)
	require.Contains(t, recorder.Body.String(), `"auto_relogin_saved":true`)
}

func TestAddAccountImportRequiresGuardForPasswordTOTP(t *testing.T) {
	recorder, admin := runAddAccountImport(t, nil, "user@example.com----password-canary----totp-canary")
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
	require.Empty(t, admin.accounts)
}

func TestAddAccountImportReportsCredentialSaveFailure(t *testing.T) {
	guard := &addAccountGuardWriter{err: errors.New("password-canary totp-canary storage-failure")}
	recorder, _ := runAddAccountImport(t, guard, "user@example.com----password-canary----totp-canary")
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
	require.Positive(t, guard.id)
	require.Contains(t, recorder.Body.String(), `credential_save_failed`)
	require.Contains(t, recorder.Body.String(), `account_id`)
	require.NotContains(t, recorder.Body.String(), "password-canary")
	require.NotContains(t, recorder.Body.String(), "storage-failure")
}

func TestAddAccountImportEnrollsEncryptedMailboxCredentials(t *testing.T) {
	for _, info := range []string{
		"user@example.com----mail-pass----mail-client----mail-refresh--------",
		"user@example.com----mail-pass----mail-client----mail-refresh----password-canary----totp-canary",
	} {
		t.Run(info, func(t *testing.T) {
			guard := &addAccountGuardWriter{}
			recorder, _ := runAddAccountImport(t, guard, info)
			require.Equal(t, http.StatusOK, recorder.Code, recorder.Body.String())
			require.Positive(t, guard.id)
			require.Equal(t, "mailbox_graph", guard.input.CredentialMode)
			require.Equal(t, service.OpenAIOAuthReauthEngineSessionStudio, guard.input.Engine)
			require.Equal(t, "user@example.com", guard.input.LoginEmail)
			require.True(t, guard.input.Enabled)
			require.True(t, guard.input.AutoReloginEnabled)
			if strings.Contains(info, "password-canary") {
				require.Equal(t, "password-canary", guard.input.Password)
				require.Equal(t, "totp-canary", guard.input.TOTPSecret)
			} else {
				require.Empty(t, guard.input.Password)
				require.Empty(t, guard.input.TOTPSecret)
			}
			inputJSON, err := json.Marshal(guard.input)
			require.NoError(t, err)
			require.Contains(t, string(inputJSON), `"mail_client_id":"mail-client"`)
			require.Contains(t, string(inputJSON), `"mail_refresh_token":"mail-refresh"`)
			require.True(t, guard.input.PreserveEnabled)
			require.True(t, guard.input.PreserveAutoRelogin)
			require.Contains(t, recorder.Body.String(), `"auto_relogin_saved":true`)
		})
	}
}

func TestAddAccountImportRequiresGuardForMailbox(t *testing.T) {
	recorder, admin := runAddAccountImport(t, nil, "user@example.com----mail-pass----mail-client----mail-refresh--------")
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
	require.Empty(t, admin.accounts)
}

func TestAddAccountImportChecksMailboxEncryptionBeforeLoggingIn(t *testing.T) {
	guard := &addAccountGuardWriter{}
	recorder, admin := runAddAccountImport(t, guard, "user@example.com----mail-pass----mail-client----mail-refresh--------", false)
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Empty(t, admin.accounts)
	require.Zero(t, guard.id)
}

func TestAddAccountImportChecksEncryptionBeforeLoggingIn(t *testing.T) {
	guard := &addAccountGuardWriter{}
	recorder, admin := runAddAccountImport(t, guard, "user@example.com----password-canary----totp-canary", false)
	require.Equal(t, http.StatusBadRequest, recorder.Code, recorder.Body.String())
	require.Empty(t, admin.accounts)
	require.Zero(t, guard.id)
}

func TestAddAccountImportReportsMailboxCredentialSaveFailure(t *testing.T) {
	guard := &addAccountGuardWriter{err: errors.New("mail-client mail-refresh storage-failure")}
	recorder, _ := runAddAccountImport(t, guard, "user@example.com----mail-pass----mail-client----mail-refresh--------")
	require.Equal(t, http.StatusServiceUnavailable, recorder.Code, recorder.Body.String())
	require.Positive(t, guard.id)
	require.Contains(t, recorder.Body.String(), `credential_save_failed`)
	require.Contains(t, recorder.Body.String(), `account_id`)
	require.NotContains(t, recorder.Body.String(), "mail-refresh")
	require.NotContains(t, recorder.Body.String(), "storage-failure")
}
