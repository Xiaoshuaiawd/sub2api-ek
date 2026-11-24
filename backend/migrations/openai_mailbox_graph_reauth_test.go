package migrations

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenAIMailboxGraphReauthMigrationPreservesExistingLoginModes(t *testing.T) {
	content, err := FS.ReadFile("398_openai_mailbox_graph_reauth.sql")
	require.NoError(t, err)
	sql := strings.Join(strings.Fields(string(content)), " ")
	require.Contains(t, sql, "ADD COLUMN IF NOT EXISTS mailbox_credentials_ciphertext TEXT")
	require.Contains(t, sql, "credential_mode IN ('email_otp_url', 'password_totp', 'mailbox_graph')")
	require.NotContains(t, sql, "UPDATE openai_oauth_reauth_configs")
	require.NotContains(t, sql, "DROP COLUMN")
}
