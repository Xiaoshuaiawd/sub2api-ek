package repository

import (
	"context"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// Both rows are insert-only in one transaction. A normal admin save or another
// instance winning either unique key keeps its configuration and switches.
func (r *openAIOAuthReauthRepository) ImportLegacyReloginConfig(ctx context.Context, config *service.OpenAIOAuthReauthStoredConfig, enabled, autoRelogin bool) (bool, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	var exists bool
	if err := tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM openai_oauth_reauth_configs WHERE account_id=$1)
 OR EXISTS(SELECT 1 FROM account_token_guard_v2_accounts WHERE account_id=$1)`, config.AccountID).Scan(&exists); err != nil {
		return false, err
	}
	if exists {
		return false, nil
	}
	result, err := tx.ExecContext(ctx, `INSERT INTO openai_oauth_reauth_configs
 (account_id,login_email,credential_mode,engine,proxy_source,proxy_id,password_ciphertext,totp_secret_ciphertext,otp_url_ciphertext,mailbox_credentials_ciphertext)
 VALUES($1,$2,$3,$4,$5,$6,NULLIF($7,''),NULLIF($8,''),NULLIF($9,''),NULLIF($10,''))
 ON CONFLICT(account_id) DO NOTHING`, config.AccountID, config.LoginEmail, config.CredentialMode, config.Engine, config.ProxySource, config.ProxyID,
		config.PasswordCiphertext, config.TOTPSecretCiphertext, config.OTPURLCiphertext, config.MailboxCredentialsCiphertext)
	if err != nil {
		return false, err
	}
	count, err := result.RowsAffected()
	if err != nil || count == 0 {
		return false, err
	}
	result, err = tx.ExecContext(ctx, `INSERT INTO account_token_guard_v2_accounts(account_id,enabled,auto_relogin_enabled)
 VALUES($1,$2,$3) ON CONFLICT(account_id) DO NOTHING`, config.AccountID, enabled, autoRelogin)
	if err != nil {
		return false, err
	}
	count, err = result.RowsAffected()
	if err != nil || count == 0 {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}
