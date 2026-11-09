package admin

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/service"
)

// ActivateInventoryAccount reuses the same 2FA login, Codex token import,
// identity deduplication, and encrypted credential guard as the manual import.
func (h *AccountHandler) ActivateInventoryAccount(ctx context.Context, entry service.AccountTokenGuardReloginAccount, canImport func(context.Context) (bool, error)) (int64, bool, string, error) {
	if h == nil || h.tokenGuard == nil || h.inventoryTokenGuardV2 == nil {
		return 0, false, "service_unavailable", errors.New("inventory activation unavailable")
	}
	job, err := h.tokenGuard.StartTwoFALoginForOperations(ctx, entry)
	if err != nil {
		var skipped *service.TwoFALoginSkippedError
		if errors.As(err, &skipped) {
			return 0, false, skipped.Code, err
		}
		return 0, false, "service_unavailable", err
	}
	defer h.tokenGuard.DeleteTwoFALogin(job.ID)
	result, err := h.waitTwoFALogin(ctx, job.ID)
	if err != nil {
		return 0, false, "timeout", err
	}
	if result.Status != "succeeded" || result.Credential == nil {
		return 0, false, result.ErrorCode, errors.New("inventory login failed")
	}
	allowed, err := canImport(ctx)
	if err != nil || !allowed {
		return 0, false, "capacity_full", errors.New("inventory capacity changed during login")
	}
	credentialJSON, err := json.Marshal(result.Credential)
	if err != nil {
		return 0, false, "import_failed", err
	}
	updateExisting := false
	req := defaultAddAccountImportRequest(string(credentialJSON), entry.Email, entry.Email, &updateExisting)
	entries, err := parseCodexSessionImportEntries(req)
	if err != nil || len(entries) != 1 {
		return 0, false, "import_failed", errors.New("inventory credential is invalid")
	}
	imported, err := h.importCodexSessions(ctx, req, entries)
	if err != nil {
		return 0, false, "import_failed", err
	}
	for _, item := range imported.Items {
		if item.AccountID <= 0 {
			continue
		}
		if item.Action != "created" && item.Action != "skipped" {
			continue
		}
		if item.Action == "skipped" {
			return item.AccountID, true, "", nil
		}
		_, err := h.inventoryTokenGuardV2.SaveAccount(ctx, item.AccountID, service.AccountTokenGuardV2AccountInput{
			LoginEmail: entry.Email, CredentialMode: service.OpenAIOAuthReauthModePasswordTOTP,
			ProxySource: service.OpenAIOAuthReauthProxySourceAccount,
			Password:    entry.Password, TOTPSecret: entry.MFASecret,
			Enabled: true, AutoReloginEnabled: true,
		})
		if err != nil {
			if item.Action == "created" {
				cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
				defer cancel()
				_ = h.adminService.DeleteAccount(cleanupCtx, item.AccountID)
			}
			return 0, false, "credential_save_failed", err
		}
		return item.AccountID, false, "", nil
	}
	return 0, false, "import_failed", errors.New("inventory import did not create an account")
}
