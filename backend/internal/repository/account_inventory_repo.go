package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

const inventoryOAuthAccountWhere = `platform='openai' AND type='oauth' AND parent_account_id IS NULL AND deleted_at IS NULL
 AND LOWER(COALESCE(credentials->>'auth_mode','')) NOT IN ('personalaccesstoken','personal_access_token','agentidentity')
 AND LOWER(COALESCE(credentials->>'openai_auth_mode','')) NOT IN ('personalaccesstoken','personal_access_token')`

const inventoryUsableAccountWhere = inventoryOAuthAccountWhere + ` AND status='active' AND schedulable=TRUE
 AND (auto_pause_on_expired=FALSE OR expires_at IS NULL OR expires_at>NOW())`

// A 401 account with automatic re-login still in budget holds its slot while
// credential guard tries to recover it. Transient rate limits never affect the
// count because they do not change status or schedulable.
const inventoryRecoverableAccountWhere = inventoryOAuthAccountWhere + ` AND status='error'
 AND error_message ILIKE '%401%' AND EXISTS (
   SELECT 1 FROM account_token_guard_v2_accounts guard
   WHERE guard.account_id=accounts.id AND guard.enabled=TRUE AND guard.auto_relogin_enabled=TRUE AND guard.auto_relogin_exhausted=FALSE
 )`

const inventoryPendingAdmissionWhere = inventoryOAuthAccountWhere + ` AND status='active' AND schedulable=FALSE
 AND extra->'openai_two_fa_admission'->>'phase'='pending'
 AND EXISTS (SELECT 1 FROM openai_account_inventory inventory WHERE inventory.account_id=accounts.id AND inventory.state='active')`

type accountInventoryRepository struct{ db *sql.DB }

func NewAccountInventoryRepository(db *sql.DB) service.AccountInventoryRepository {
	return &accountInventoryRepository{db: db}
}

func (r *accountInventoryRepository) Import(ctx context.Context, entries []service.AccountInventoryEncryptedEntry) (int, error) {
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	inserted := 0
	for start := 0; start < len(entries); start += 200 {
		end := min(start+200, len(entries))
		var query strings.Builder
		query.WriteString(`INSERT INTO openai_account_inventory (email, password_ciphertext, mfa_ciphertext) VALUES `)
		args := make([]any, 0, (end-start)*3)
		for i := start; i < end; i++ {
			if i > start {
				query.WriteByte(',')
			}
			base := len(args)
			fmt.Fprintf(&query, "($%d,$%d,$%d)", base+1, base+2, base+3)
			args = append(args, entries[i].Email, entries[i].PasswordCiphertext, entries[i].MFACiphertext)
		}
		query.WriteString(` ON CONFLICT (email) DO UPDATE SET
 password_ciphertext=EXCLUDED.password_ciphertext, mfa_ciphertext=EXCLUDED.mfa_ciphertext,
 state='pending', account_id=NULL, lease_until=NULL, next_attempt_at=NOW(),
 attempts=0, error_code='', updated_at=NOW()
 WHERE openai_account_inventory.state='failed'`)
		result, execErr := tx.ExecContext(ctx, query.String(), args...)
		if execErr != nil {
			return 0, execErr
		}
		count, execErr := result.RowsAffected()
		if execErr != nil {
			return 0, execErr
		}
		inserted += int(count)
	}
	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

func (r *accountInventoryRepository) Status(ctx context.Context) (service.AccountInventoryStatus, error) {
	var result service.AccountInventoryStatus
	err := r.db.QueryRowContext(ctx, `SELECT config.target_count, config.enabled,
 (SELECT COUNT(*) FROM accounts WHERE `+inventoryUsableAccountWhere+`),
	 (SELECT COUNT(*) FROM accounts WHERE `+inventoryPendingAdmissionWhere+`),
 (SELECT COUNT(*) FROM openai_account_inventory WHERE state='pending'),
 (SELECT COUNT(*) FROM openai_account_inventory WHERE state='activating'),
 (SELECT COUNT(*) FROM openai_account_inventory WHERE state='active'),
 (SELECT COUNT(*) FROM openai_account_inventory WHERE state='failed'),
 (SELECT COUNT(*) FROM openai_account_inventory WHERE state='retired'),
 (SELECT COUNT(*) FROM openai_account_inventory WHERE state='duplicate')
 FROM openai_account_inventory_config config WHERE singleton=TRUE`).Scan(
		&result.TargetCount, &result.Enabled, &result.Available, &result.AdmissionPending, &result.Pending,
		&result.Activating, &result.Active, &result.Failed, &result.Retired, &result.Duplicate)
	return result, err
}

func (r *accountInventoryRepository) SaveConfig(ctx context.Context, target int, enabled bool) error {
	_, err := r.db.ExecContext(ctx, `UPDATE openai_account_inventory_config SET target_count=$1, enabled=$2, updated_at=NOW() WHERE singleton=TRUE`, target, enabled)
	return err
}

func (r *accountInventoryRepository) List(ctx context.Context, offset, limit int) ([]service.AccountInventoryRow, int, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM openai_account_inventory`).Scan(&total); err != nil {
		return nil, 0, err
	}
	rows, err := r.db.QueryContext(ctx, `SELECT id, email, state, account_id, attempts, error_code, created_at
 FROM openai_account_inventory ORDER BY id DESC LIMIT $1 OFFSET $2`, limit, offset)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	result := make([]service.AccountInventoryRow, 0, limit)
	for rows.Next() {
		var row service.AccountInventoryRow
		var accountID sql.NullInt64
		if err := rows.Scan(&row.ID, &row.Email, &row.State, &accountID, &row.Attempts, &row.ErrorCode, &row.CreatedAt); err != nil {
			return nil, 0, err
		}
		if accountID.Valid {
			row.AccountID = &accountID.Int64
		}
		result = append(result, row)
	}
	return result, total, rows.Err()
}

func (r *accountInventoryRepository) Claim(ctx context.Context, limit int) ([]service.AccountInventoryClaim, error) {
	if limit < 1 {
		return nil, nil
	}
	tx, err := r.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	var target int
	var enabled bool
	if err := tx.QueryRowContext(ctx, `SELECT target_count, enabled FROM openai_account_inventory_config WHERE singleton=TRUE FOR UPDATE`).Scan(&target, &enabled); err != nil {
		return nil, err
	}
	if !enabled || target == 0 {
		return nil, tx.Commit()
	}
	// Recover claims abandoned by a crashed process after the login task's
	// 25-minute deadline. The unique email and Codex import dedup prevent a
	// duplicate account if the old process completed just before crashing.
	if _, err := tx.ExecContext(ctx, `UPDATE openai_account_inventory SET state='pending', lease_until=NULL, next_attempt_at=NOW()+INTERVAL '5 minutes', updated_at=NOW()
 WHERE state='activating' AND lease_until < NOW()`); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE openai_account_inventory inventory SET state='retired', password_ciphertext='', mfa_ciphertext='',
 error_code='account_removed', updated_at=NOW() WHERE state='active' AND
 (account_id IS NULL OR EXISTS (SELECT 1 FROM accounts account WHERE account.id=inventory.account_id AND account.deleted_at IS NOT NULL))`); err != nil {
		return nil, err
	}
	if _, err := tx.ExecContext(ctx, `UPDATE openai_account_inventory inventory SET state='pending', account_id=NULL,
 error_code='', next_attempt_at=NOW(), updated_at=NOW() WHERE state='duplicate' AND
 (account_id IS NULL OR EXISTS (SELECT 1 FROM accounts account WHERE account.id=inventory.account_id AND account.deleted_at IS NOT NULL))`); err != nil {
		return nil, err
	}
	var occupied, inFlight int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM accounts WHERE (`+inventoryUsableAccountWhere+`) OR (`+inventoryRecoverableAccountWhere+`) OR (`+inventoryPendingAdmissionWhere+`)`).Scan(&occupied); err != nil {
		return nil, err
	}
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM openai_account_inventory WHERE state='activating' AND lease_until >= NOW()`).Scan(&inFlight); err != nil {
		return nil, err
	}
	missing := min(limit, target-occupied-inFlight)
	if missing <= 0 {
		return nil, tx.Commit()
	}
	rows, err := tx.QueryContext(ctx, `SELECT id, email, password_ciphertext, mfa_ciphertext FROM openai_account_inventory
 WHERE state='pending' AND next_attempt_at <= NOW() ORDER BY id LIMIT $1 FOR UPDATE SKIP LOCKED`, missing)
	if err != nil {
		return nil, err
	}
	claims := make([]service.AccountInventoryClaim, 0, missing)
	for rows.Next() {
		var claim service.AccountInventoryClaim
		if err := rows.Scan(&claim.ID, &claim.Email, &claim.PasswordCiphertext, &claim.MFACiphertext); err != nil {
			rows.Close()
			return nil, err
		}
		claims = append(claims, claim)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return nil, err
	}
	rows.Close()
	for _, claim := range claims {
		if _, err := tx.ExecContext(ctx, `UPDATE openai_account_inventory SET state='activating', attempts=attempts+1,
 lease_until=NOW()+INTERVAL '30 minutes', updated_at=NOW() WHERE id=$1`, claim.ID); err != nil {
			return nil, err
		}
	}
	return claims, tx.Commit()
}

func (r *accountInventoryRepository) CanActivate(ctx context.Context, claimID int64) (bool, error) {
	var allowed bool
	err := r.db.QueryRowContext(ctx, `SELECT config.enabled AND config.target_count > (
	 SELECT COUNT(*) FROM accounts WHERE (`+inventoryUsableAccountWhere+`) OR (`+inventoryRecoverableAccountWhere+`) OR (`+inventoryPendingAdmissionWhere+`))
	 + (SELECT COUNT(*) FROM openai_account_inventory WHERE state='activating' AND lease_until >= NOW() AND id < $1)
	 FROM openai_account_inventory_config config WHERE singleton=TRUE`, claimID).Scan(&allowed)
	return allowed, err
}

func (r *accountInventoryRepository) Complete(ctx context.Context, id int64, state string, accountID int64, errorCode string) error {
	var linked any
	if accountID > 0 {
		linked = accountID
	}
	_, err := r.db.ExecContext(ctx, `UPDATE openai_account_inventory SET state=$2, account_id=$3, error_code=$4,
 lease_until=NULL, next_attempt_at=CASE WHEN $2='pending' THEN NOW()+INTERVAL '5 minutes' ELSE NOW() END,
 updated_at=NOW() WHERE id=$1 AND state='activating'`, id, state, linked, errorCode)
	return err
}

func (r *accountInventoryRepository) ConfirmedBannedAccountIDs(ctx context.Context) ([]int64, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT account.id FROM accounts account
	 WHERE account.platform='openai' AND account.type='oauth' AND account.parent_account_id IS NULL AND account.deleted_at IS NULL AND account.status='error'
 AND LOWER(COALESCE(account.credentials->>'auth_mode','')) NOT IN ('personalaccesstoken','personal_access_token','agentidentity')
 AND LOWER(COALESCE(account.credentials->>'openai_auth_mode','')) NOT IN ('personalaccesstoken','personal_access_token')
 AND ((account.extra->>'login_error_code', account.error_message) IN (
   ('account_deactivated','封号'),
   ('account_deleted','封号（账号已删除）'),
   ('account_retired','封号（账号已删除或停用）'),
   ('dead_account','死号（邮箱失效，收不到验证码且无密码）'),
   ('account_not_found','无此账号（被引导注册，需先注册 OpenAI 账号）'))
 OR EXISTS (SELECT 1 FROM openai_oauth_reauth_tasks task
    WHERE task.account_id=account.id
      AND task.id=(SELECT MAX(latest.id) FROM openai_oauth_reauth_tasks latest WHERE latest.account_id=account.id)
      AND task.status='failed' AND task.finished_at IS NOT NULL AND task.finished_at >= account.updated_at
      AND task.error_message IN ('account_deactivated','account_deleted','account_retired','dead_account','account_not_found')))
 ORDER BY account.id LIMIT 100`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (r *accountInventoryRepository) RetireAccount(ctx context.Context, accountID int64) error {
	_, err := r.db.ExecContext(ctx, `UPDATE openai_account_inventory SET state='retired', password_ciphertext='', mfa_ciphertext='',
 error_code='account_banned', lease_until=NULL, updated_at=NOW() WHERE account_id=$1`, accountID)
	return err
}

var _ service.AccountInventoryRepository = (*accountInventoryRepository)(nil)
