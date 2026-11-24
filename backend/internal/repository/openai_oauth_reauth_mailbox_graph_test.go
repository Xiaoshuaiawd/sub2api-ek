package repository

import (
	"context"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestOpenAIOAuthReauthMailboxGraphPersistsEncryptedBundle(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := NewOpenAIOAuthReauthRepository(db)
	version := time.Now()
	mock.ExpectExec("(?s)INSERT INTO openai_oauth_reauth_configs.*mailbox_credentials_ciphertext.*updated_at = \\$11").
		WithArgs(int64(42), "user@example.com", "mailbox_graph", "session_studio", "account", nil, "", "", "", "enc-mailbox", version).
		WillReturnResult(sqlmock.NewResult(0, 1))
	err = repo.UpsertConfig(context.Background(), &service.OpenAIOAuthReauthStoredConfig{
		AccountID: 42, LoginEmail: "user@example.com", CredentialMode: service.OpenAIOAuthReauthModeMailboxGraph,
		Engine: service.OpenAIOAuthReauthEngineSessionStudio, ProxySource: service.OpenAIOAuthReauthProxySourceAccount,
		MailboxCredentialsCiphertext: "enc-mailbox", UpdatedAt: version,
	})
	require.NoError(t, err)
	mock.ExpectQuery("(?s)SELECT.*mailbox_credentials_ciphertext, updated_at").WithArgs(int64(42)).
		WillReturnRows(sqlmock.NewRows([]string{"account_id", "email", "mode", "engine", "proxy_source", "proxy_id", "password", "totp", "otp", "mailbox", "updated_at"}).
			AddRow(42, "user@example.com", "mailbox_graph", "session_studio", "account", nil, nil, nil, nil, "enc-mailbox", version))
	stored, err := repo.GetConfig(context.Background(), 42)
	require.NoError(t, err)
	require.Equal(t, "enc-mailbox", stored.MailboxCredentialsCiphertext)
	require.Empty(t, stored.PasswordCiphertext)
	require.Empty(t, stored.OTPURLCiphertext)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestOpenAIOAuthReauthMailboxGraphManagedClaimForcesRemoteBeforeLocking(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	repo := &openAIOAuthReauthRepository{db: db}
	mock.ExpectQuery("(?s)WITH expired_callbacks.*OR.*\\$9 = 'password_totp' AND config.credential_mode = 'mailbox_graph'.*WHEN config.credential_mode = 'mailbox_graph' THEN 'session_studio'.*WHEN \\$11.*FOR UPDATE SKIP LOCKED").
		WithArgs("failed", "failed", "callback_processing", int64(1800), "queued", "running", "starting", "worker", "password_totp", `{"session_studio"}`, "local_worker").
		WillReturnRows(sqlmock.NewRows([]string{"id"}))
	claim, err := repo.ClaimNextTaskForRuntime(context.Background(), "worker", 30*time.Minute, service.OpenAIOAuthReauthModePasswordTOTP, []string{service.OpenAIOAuthReauthEngineSessionStudio}, service.OpenAIOAuthReauthEngineLocal)
	require.NoError(t, err)
	require.Nil(t, claim)
	require.NoError(t, mock.ExpectationsWereMet())
}

func TestRecorded401IncludesEncryptedMailboxGraphCredentials(t *testing.T) {
	db, mock, err := sqlmock.New()
	require.NoError(t, err)
	defer db.Close()
	mock.ExpectQuery("(?s)SELECT account.id.*config.credential_mode='password_totp'.*OR.*config.credential_mode='mailbox_graph'.*length\\(config.mailbox_credentials_ciphertext\\).*NOT EXISTS").
		WithArgs(20).WillReturnRows(sqlmock.NewRows([]string{"id"}).AddRow(42))
	ids, err := (&accountTokenGuardV2Repository{db: db}).ListRecorded401(context.Background(), 20)
	require.NoError(t, err)
	require.Equal(t, []int64{42}, ids)
	require.NoError(t, mock.ExpectationsWereMet())
}
