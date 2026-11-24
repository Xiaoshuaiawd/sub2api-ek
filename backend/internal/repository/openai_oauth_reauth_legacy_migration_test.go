package repository

import (
	"context"
	"testing"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/stretchr/testify/require"
)

func TestLegacyReloginMigrationAtomicallyPreservesExistingRows(t *testing.T) {
	for _, tc := range []struct {
		name                  string
		exists                bool
		configRows, guardRows int64
		want                  bool
	}{
		{"already configured", true, 0, 0, false},
		{"concurrent login configuration", false, 0, 0, false},
		{"concurrent guard configuration rolls back login", false, 1, 0, false},
		{"both rows inserted", false, 1, 1, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, mock, err := sqlmock.New()
			require.NoError(t, err)
			defer db.Close()
			mock.ExpectBegin()
			mock.ExpectQuery("(?s)SELECT EXISTS.*openai_oauth_reauth_configs.*OR EXISTS.*account_token_guard_v2_accounts").WithArgs(int64(42)).WillReturnRows(sqlmock.NewRows([]string{"exists"}).AddRow(tc.exists))
			if !tc.exists {
				mock.ExpectExec("(?s)INSERT INTO openai_oauth_reauth_configs.*mailbox_credentials_ciphertext.*ON CONFLICT\\(account_id\\) DO NOTHING").WithArgs(int64(42), "user@example.com", "mailbox_graph", "session_studio", "account", nil, "", "", "", "encrypted-bundle").WillReturnResult(sqlmock.NewResult(0, tc.configRows))
				if tc.configRows > 0 {
					mock.ExpectExec("(?s)INSERT INTO account_token_guard_v2_accounts.*ON CONFLICT\\(account_id\\) DO NOTHING").WithArgs(int64(42), false, true).WillReturnResult(sqlmock.NewResult(0, tc.guardRows))
				}
			}
			if tc.want {
				mock.ExpectCommit()
			} else {
				mock.ExpectRollback()
			}
			inserted, err := (&openAIOAuthReauthRepository{db: db}).ImportLegacyReloginConfig(context.Background(), &service.OpenAIOAuthReauthStoredConfig{
				AccountID: 42, LoginEmail: "user@example.com", CredentialMode: "mailbox_graph", Engine: "session_studio", ProxySource: "account", MailboxCredentialsCiphertext: "encrypted-bundle",
			}, false, true)
			require.NoError(t, err)
			require.Equal(t, tc.want, inserted)
			require.NoError(t, mock.ExpectationsWereMet())
		})
	}
}
