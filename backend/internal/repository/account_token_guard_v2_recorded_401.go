package repository

import "context"

// ListRecorded401 includes legacy account-test 401 bodies as candidates; the
// service parses their top-level auth code before queuing anything. Ordering
// by the guard's updated_at rotates rejected candidates behind later rows.
func (r *accountTokenGuardV2Repository) ListRecorded401(ctx context.Context, limit int) ([]int64, error) {
	if limit <= 0 {
		return nil, nil
	}
	rows, err := r.db.QueryContext(ctx, `SELECT account.id FROM accounts account
 JOIN account_token_guard_v2_accounts guard ON guard.account_id=account.id
 JOIN openai_oauth_reauth_configs config ON config.account_id=account.id
 WHERE account.platform='openai' AND account.type='oauth' AND account.parent_account_id IS NULL
   AND account.deleted_at IS NULL AND account.status='error'
   AND (
     account.error_message LIKE 'Token revoked (401):%'
     OR account.error_message LIKE 'Unauthorized (401):%'
     OR account.error_message LIKE 'Authentication failed (401):%'
     OR account.error_message LIKE 'OAuth 401 (no refresh_token):%'
   )
   AND LOWER(COALESCE(account.credentials->>'auth_mode','')) NOT IN ('personalaccesstoken','personal_access_token','agentidentity')
   AND LOWER(COALESCE(account.credentials->>'openai_auth_mode','')) NOT IN ('personalaccesstoken','personal_access_token')
   AND guard.enabled=TRUE AND guard.auto_relogin_enabled=TRUE AND guard.auto_relogin_exhausted=FALSE
   AND (guard.cooldown_until IS NULL OR guard.cooldown_until<=NOW())
   AND (guard.lease_until IS NULL OR guard.lease_until<NOW())
   AND config.credential_mode='password_totp' AND COALESCE(length(config.password_ciphertext),0)>0
   AND NOT EXISTS (SELECT 1 FROM openai_oauth_reauth_tasks task
     WHERE task.account_id=account.id AND task.status IN ('queued','running','callback_processing'))
 ORDER BY guard.updated_at ASC, account.id ASC LIMIT $1`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	ids := make([]int64, 0, limit)
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}
