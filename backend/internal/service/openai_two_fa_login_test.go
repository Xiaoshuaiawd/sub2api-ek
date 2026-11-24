package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type twoFALoginSettings struct {
	SettingRepository
	mu     sync.Mutex
	raw    string
	setErr error
	writes int
}

func (s *twoFALoginSettings) GetValue(context.Context, string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.raw, nil
}

func (s *twoFALoginSettings) Set(_ context.Context, _, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.setErr != nil {
		return s.setErr
	}
	s.raw = value
	s.writes++
	return nil
}

func newTwoFATestService(t *testing.T, server *httptest.Server) *OpenAIReloginService {
	t.Helper()
	cfg := defaultOpenAIReloginConfig()
	cfg.ReloginEndpoint = server.URL
	cfg.ReloginHeaders["X-Test-Client"] = "{{uuid}}"
	raw, err := json.Marshal(cfg)
	require.NoError(t, err)
	svc := NewOpenAIReloginService(&twoFALoginSettings{raw: string(raw)})
	svc.httpClient = server.Client()
	t.Cleanup(svc.clearTwoFALogins)
	return svc
}

func waitTwoFALogin(t *testing.T, svc *OpenAIReloginService, id string) *OpenAITwoFALoginJob {
	t.Helper()
	var job *OpenAITwoFALoginJob
	require.Eventually(t, func() bool {
		var ok bool
		job, ok = svc.TwoFALogin(id)
		return ok && job.Status != "running"
	}, 3*time.Second, 5*time.Millisecond)
	return job
}

func TestTwoFALoginForOperationsDoesNotSaveLegacyCredentials(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "{\"credential\":{\"access_token\":\"access\",\"refresh_token\":\"refresh\",\"id_token\":\"id\"}}")
	}))
	defer server.Close()
	svc := newTwoFATestService(t, server)
	settings, ok := svc.settings.(*twoFALoginSettings)
	require.True(t, ok)
	before := settings.raw
	entry := OpenAIReloginCredentials{Email: "operations@example.com", Password: "test-password", MFASecret: "test-secret"}
	job, err := svc.StartTwoFALogin(context.Background(), entry)
	require.NoError(t, err)
	result := waitTwoFALogin(t, svc, job.ID)
	require.Equal(t, "succeeded", result.Status)
	settings.mu.Lock()
	require.Zero(t, settings.writes)
	require.Equal(t, before, settings.raw)
	settings.mu.Unlock()
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), entry.Password)
	require.NotContains(t, string(raw), entry.MFASecret)
}

type existingTwoFALoginAccountRepo struct {
	AccountRepository
	account *Account
	code    string
}

func (r *existingTwoFALoginAccountRepo) ListAllWithFilters(context.Context, string, string, string, string, int64, string) ([]Account, error) {
	return []Account{*r.account}, nil
}
func (r *existingTwoFALoginAccountRepo) UpdateExtra(_ context.Context, _ int64, updates map[string]any) error {
	r.code, _ = updates["login_error_code"].(string)
	return nil
}
func (r *existingTwoFALoginAccountRepo) SetError(_ context.Context, _ int64, message string) error {
	r.account.Status = StatusError
	r.account.ErrorMessage = message
	return nil
}
func (r *existingTwoFALoginAccountRepo) SetSchedulable(_ context.Context, _ int64, schedulable bool) error {
	r.account.Schedulable = schedulable
	return nil
}

func TestExistingTwoFALoginBanRecordsSafeCodeForInventoryCleanup(t *testing.T) {
	repo := &existingTwoFALoginAccountRepo{account: &Account{ID: 11, Name: "user@example.com", Status: StatusActive, Schedulable: true}}
	svc := &OpenAIReloginService{accounts: repo}
	id := svc.recordFailedTwoFALoginAccount(context.Background(), AccountTokenGuardReloginAccount{Email: "user@example.com"}, "account_deactivated")
	require.Equal(t, int64(11), id)
	require.Equal(t, "account_deactivated", repo.code)
	require.Equal(t, "封号", repo.account.ErrorMessage)
	require.False(t, repo.account.Schedulable)
}

func TestInventoryLoginFailureDoesNotDisableMatchingHealthyAccount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"error":{"code":"password_rejected"}}`)
	}))
	defer server.Close()
	svc := newTwoFATestService(t, server)
	repo := &existingTwoFALoginAccountRepo{account: &Account{ID: 11, Name: "user@example.com", Status: StatusActive, Schedulable: true}}
	svc.accounts = repo
	job, err := svc.StartTwoFALoginForInventory(context.Background(), AccountTokenGuardReloginAccount{Email: "user@example.com", Password: "wrong", MFASecret: "secret"})
	require.NoError(t, err)
	result := waitTwoFALogin(t, svc, job.ID)
	require.Equal(t, "failed", result.Status)
	require.Zero(t, result.AccountID)
	require.Equal(t, StatusActive, repo.account.Status)
	require.True(t, repo.account.Schedulable)
}

func TestTwoFALoginInitialLoginWithoutExistingAccount(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error("invalid login payload")
		}
		payload["test_client"] = r.Header.Get("X-Test-Client")
		payload["relogin_header"] = r.Header.Get("X-Session-Studio-Relogin")
		requests <- payload
		_, _ = io.WriteString(w, `{"type":"result","payload":{"credential":{"access_token":"access","refresh_token":"refresh","id_token":"id","account_id":"workspace","password":"echo-password","mfa_secret":"echo-secret","model_mapping":{"bad":"bad"}}}}`)
	}))
	defer server.Close()
	svc := newTwoFATestService(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	job, err := svc.StartTwoFALogin(ctx, OpenAIReloginCredentials{Email: " USER@example.com ", Password: "p,a!ss", MFASecret: "TEST-SECRET"})
	require.NoError(t, err)
	cancel() // The HTTP request may finish immediately after acceptance.
	result := waitTwoFALogin(t, svc, job.ID)
	require.Equal(t, "succeeded", result.Status)
	require.Equal(t, "user@example.com", result.Credential["email"])
	require.Equal(t, "workspace", result.Credential["account_id"])
	require.NotContains(t, result.Credential, "password")
	require.NotContains(t, result.Credential, "mfa_secret")
	require.NotContains(t, result.Credential, "model_mapping")
	payload := <-requests
	require.Equal(t, "password_2fa", payload["auth_mode"])
	require.Equal(t, "start", payload["action"])
	require.Equal(t, "p,a!ss", payload["password"])
	require.Equal(t, "1", payload["relogin_header"])
	require.NotEmpty(t, payload["test_client"])
	require.NotEqual(t, "{{uuid}}", payload["test_client"])
	result.Credential["access_token"] = "mutated"
	original, ok := svc.TwoFALogin(job.ID)
	require.True(t, ok)
	require.Equal(t, "access", original.Credential["access_token"])
	svc.DeleteTwoFALogin(job.ID)
	_, ok = svc.TwoFALogin(job.ID)
	require.False(t, ok)
}

func TestTwoFALoginFailureDoesNotExposeProviderData(t *testing.T) {
	for _, body := range []string{
		`{"error":{"code":"password-canary-secret","message":"secret"}}`,
		`{"credential":{"access_token":"access-only-secret"}}`,
	} {
		t.Run("redacted", func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = io.WriteString(w, body) }))
			defer server.Close()
			svc := newTwoFATestService(t, server)
			job, err := svc.StartTwoFALogin(context.Background(), OpenAIReloginCredentials{Email: "a@example.com", Password: "secret", MFASecret: "secret"})
			require.NoError(t, err)
			result := waitTwoFALogin(t, svc, job.ID)
			require.Equal(t, "failed", result.Status)
			raw, err := json.Marshal(result)
			require.NoError(t, err)
			require.NotContains(t, string(raw), "secret")
			require.Nil(t, result.Credential)
			settings, ok := svc.settings.(*twoFALoginSettings)
			require.True(t, ok)
			require.Zero(t, settings.writes)
		})
	}
}

func TestTwoFALoginCancellation(t *testing.T) {
	started, canceled := make(chan struct{}), make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		close(started)
		<-r.Context().Done()
		close(canceled)
	}))
	defer server.Close()
	svc := newTwoFATestService(t, server)
	job, err := svc.StartTwoFALogin(context.Background(), OpenAIReloginCredentials{Email: "a@example.com", Password: "secret", MFASecret: "secret"})
	require.NoError(t, err)
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("login did not start")
	}
	svc.DeleteTwoFALogin(job.ID)
	select {
	case <-canceled:
	case <-time.After(3 * time.Second):
		t.Fatal("upstream login was not canceled")
	}
	_, ok := svc.TwoFALogin(job.ID)
	require.False(t, ok)
}

func TestTwoFALoginRejectsInvalidInputBeforeNetwork(t *testing.T) {
	svc := &OpenAIReloginService{}
	for _, entry := range []OpenAIReloginCredentials{
		{Email: "not-email", Password: "p", MFASecret: "s"},
		{Email: "User <a@example.com>", Password: "p", MFASecret: "s"},
		{Email: "a@example.com", Password: "p"},
		{Email: "a@example.com", Password: strings.Repeat("p", 4097), MFASecret: "s"},
	} {
		_, err := svc.StartTwoFALogin(context.Background(), entry)
		require.Error(t, err)
	}
}

func TestTwoFALoginPreservesMailboxAndRefreshCredentials(t *testing.T) {
	requests := make(chan map[string]any, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		requests <- payload
		_, _ = io.WriteString(w, `{"credential":{"access_token":"access","refresh_token":"refresh","id_token":"id"}}`)
	}))
	defer server.Close()
	svc := newTwoFATestService(t, server)
	entry := OpenAIReloginCredentials{Email: "mailbox@example.com", MailClientID: "mail-client", MailRefreshToken: "mail-refresh", RefreshToken: "old-refresh"}
	job, err := svc.StartTwoFALogin(context.Background(), entry)
	require.NoError(t, err)
	require.Equal(t, "succeeded", waitTwoFALogin(t, svc, job.ID).Status)
	payload := <-requests
	require.Equal(t, entry.MailClientID, payload["mail_client_id"])
	require.Equal(t, entry.MailRefreshToken, payload["mail_refresh_token"])
	require.Equal(t, entry.RefreshToken, payload["refresh_token"])
	settings := svc.settings.(*twoFALoginSettings)
	require.Zero(t, settings.writes)
}

func TestTwoFALoginPreservesSafeErrorDiagnosis(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"error":{"code":"account_deactivated","message":"provider-secret"}}`)
	}))
	defer server.Close()
	svc := newTwoFATestService(t, server)
	repo := &existingTwoFALoginAccountRepo{account: &Account{ID: 11, Name: "a@example.com", Status: StatusActive, Schedulable: true}}
	svc.SetAccounts(repo)
	job, err := svc.StartTwoFALogin(context.Background(), OpenAIReloginCredentials{Email: "a@example.com", Password: "password", MFASecret: "totp"})
	require.NoError(t, err)
	result := waitTwoFALogin(t, svc, job.ID)
	require.Equal(t, "failed", result.Status)
	require.Equal(t, "account_deactivated", result.ErrorCode)
	require.Equal(t, int64(11), result.AccountID)
	require.Equal(t, StatusError, repo.account.Status)
	require.False(t, repo.account.Schedulable)
	raw, err := json.Marshal(result)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "provider-secret")
}

func TestSafeTwoFALoginErrorRedactsMailboxSecrets(t *testing.T) {
	entry := OpenAIReloginCredentials{Email: "a@example.com", Password: "password-canary", MFASecret: "totp-canary", MailClientID: "mail-client-canary", MailRefreshToken: "mail-refresh-canary", RefreshToken: "refresh-canary"}
	reason := safeTwoFALoginError(fmt.Errorf("%s %s %s %s %s %s", entry.Email, entry.Password, entry.MFASecret, entry.MailClientID, entry.MailRefreshToken, entry.RefreshToken), entry)
	for _, secret := range []string{entry.Email, entry.Password, entry.MFASecret, entry.MailClientID, entry.MailRefreshToken, entry.RefreshToken} {
		require.NotContains(t, reason, secret)
	}
}

func TestTwoFALoginSkipsKnownBannedAccountUnlessForced(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{"credential":{"access_token":"access","refresh_token":"refresh","id_token":"id"}}`)
	}))
	defer server.Close()
	svc := newTwoFATestService(t, server)
	svc.SetAccounts(&existingTwoFALoginAccountRepo{account: &Account{ID: 11, Name: "a@example.com", Status: StatusError, ErrorMessage: "封号", Extra: map[string]any{"login_error_code": "account_deactivated"}}})
	entry := OpenAIReloginCredentials{Email: "a@example.com", Password: "password", MFASecret: "totp"}
	job, err := svc.StartTwoFALogin(context.Background(), entry)
	require.Nil(t, job)
	var skipped *TwoFALoginSkippedError
	require.ErrorAs(t, err, &skipped)
	require.Equal(t, "account_deactivated", skipped.Code)
	require.Equal(t, int64(11), skipped.AccountID)
	entry.Force = true
	job, err = svc.StartTwoFALogin(context.Background(), entry)
	require.NoError(t, err)
	require.Equal(t, "succeeded", waitTwoFALogin(t, svc, job.ID).Status)
}
