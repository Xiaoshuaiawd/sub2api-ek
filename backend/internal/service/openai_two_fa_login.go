package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/domain"
)

// Login jobs keep token results in bounded, short-lived memory. Successful logins
// save the supplied password/2FA to the guard without enabling it or creating accounts.
type OpenAITwoFALoginJob struct {
	ID         string         `json:"id"`
	Status     string         `json:"status"`
	Credential map[string]any `json:"credential,omitempty"`
	// ErrorCode 是脱敏后的失败原因短码（如 account_deactivated），供前端展示准确报错。
	ErrorCode string `json:"error_code,omitempty"`
	// AccountID 指向失败后登记到账号列表的错误态账号（0 表示未登记）。
	AccountID int64          `json:"account_id,omitempty"`
	cancel    context.CancelFunc
	expiry    *time.Timer
}

func ValidateOpenAITwoFALogin(entry AccountTokenGuardReloginAccount) error {
	email := strings.TrimSpace(entry.Email)
	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email || len(email) > 320 ||
		len(entry.Password) > 4096 || len(entry.MFASecret) > 4096 {
		return errors.New("请填写有效的邮箱")
	}
	// 两种可用登录方式：密码 + 2FA，或邮箱取件凭据（走邮箱验证码）。
	hasPasswordLogin := strings.TrimSpace(entry.Password) != "" && strings.TrimSpace(entry.MFASecret) != ""
	hasMailbox := strings.TrimSpace(entry.MailClientID) != "" && strings.TrimSpace(entry.MailRefreshToken) != ""
	if !hasPasswordLogin && !hasMailbox {
		return errors.New("请填写邮箱、GPT 密码与 2FA 密钥，或提供邮箱取件凭据（client_id + refresh_token）")
	}
	return nil
}

func (s *AccountTokenGuardService) StartTwoFALogin(ctx context.Context, entry AccountTokenGuardReloginAccount) (*OpenAITwoFALoginJob, error) {
	return s.startTwoFALogin(ctx, entry, true)
}

// Operations imports save their login method through the encrypted per-account
// API after identity deduplication returns the account ID. Do not also enroll
// them in the legacy guard's plaintext settings.
func (s *AccountTokenGuardService) StartTwoFALoginForOperations(ctx context.Context, entry AccountTokenGuardReloginAccount) (*OpenAITwoFALoginJob, error) {
	return s.startTwoFALogin(ctx, entry, false)
}

func (s *AccountTokenGuardService) startTwoFALogin(ctx context.Context, entry AccountTokenGuardReloginAccount, saveToLegacyGuard bool) (*OpenAITwoFALoginJob, error) {
	if err := ValidateOpenAITwoFALogin(entry); err != nil {
		return nil, err
	}
	cfg, err := s.GetConfig(ctx)
	if err != nil {
		return nil, errors.New("无法读取凭证守护配置")
	}
	// Explicit login works while scheduled inspection/automatic relogin is off.
	if err := validateGuardHTTPURL(cfg.ReloginEndpoint, "relogin_endpoint"); err != nil {
		return nil, errors.New("请在凭证守护中配置有效的重登接口")
	}
	entry.Email = strings.ToLower(strings.TrimSpace(entry.Email))
	entry.MFASecret = strings.TrimSpace(entry.MFASecret)
	// 已封号的账号不再重复登录：每次登录都会触发一封验证码邮件，
	// 既浪费又容易被邮箱侧限流。需要强制重试时由调用方设置 Force。
	if !entry.Force {
		if banned := s.findBannedAccountByEmail(ctx, entry.Email); banned != nil {
			code := strings.TrimSpace(guardText(banned.Extra["login_error_code"]))
			if code == "" {
				if strings.Contains(banned.ErrorMessage, "死号") {
					code = "dead_account"
				} else {
					code = "account_deactivated"
				}
			}
			return nil, &TwoFALoginSkippedError{
				AccountID: banned.ID,
				Code:      code,
				Status:    banned.ErrorMessage,
			}
		}
	}
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if len(s.logins) >= 32 {
		return nil, errors.New("登录任务过多，请稍后重试")
	}
	if s.logins == nil {
		s.logins = make(map[string]*OpenAITwoFALoginJob)
	}
	loginCtx, cancel := context.WithTimeout(context.Background(), 25*time.Minute)
	job := &OpenAITwoFALoginJob{ID: guardRandomID(), Status: "running", cancel: cancel}
	s.logins[job.ID] = job
	job.expiry = time.AfterFunc(30*time.Minute, func() { s.DeleteTwoFALogin(job.ID) })
	go func() {
		defer cancel()
		credential, loginErr := s.relogin(loginCtx, cfg, entry)
		s.loginMu.Lock()
		defer s.loginMu.Unlock()
		if s.logins[job.ID] != job {
			return
		}
		if loginErr != nil {
			// Neither provider errors nor request data may be reflected to clients.
			reason := safeTwoFALoginError(loginErr, entry)
			slog.Warn("account_token_guard_two_fa_login_failed",
				"job_id", job.ID,
				"stage", "relogin",
				"reason", reason,
			)
			job.Status = "failed"
			job.ErrorCode = twoFALoginPublicErrorCode(reason)
			// 账号层面的失败（封号 / 密码错误 / 需要验证）同步登记到账号列表的错误态，
			// 便于管理员直接看到哪些号已经不可用。
			job.AccountID = s.recordFailedTwoFALoginAccount(context.Background(), entry, job.ErrorCode)
			return
		}
		// Persist verified login credentials before the client imports tokens and
		// clears its password/MFA input. Never report success on a failed save.
		if saveToLegacyGuard {
			if err := s.saveTwoFALoginAccount(loginCtx, entry); err != nil {
				slog.Warn("account_token_guard_two_fa_login_failed",
					"job_id", job.ID,
					"stage", "save_guard_credentials",
					"reason", safeTwoFALoginError(err, entry),
				)
				job.Status = "failed"
				return
			}
		}
		job.Credential = twoFALoginCredential(credential, entry.Email)
		job.Status = "succeeded"
	}()
	return &OpenAITwoFALoginJob{ID: job.ID, Status: job.Status}, nil
}

// safeTwoFALoginError keeps asynchronous login diagnostics useful without
// allowing an upstream response or an accidentally wrapped secret to become a
// log entry. The relogin path already reduces network and provider failures to
// short error codes; this is a final redaction, length and control-character guard.
func safeTwoFALoginError(err error, entry AccountTokenGuardReloginAccount) string {
	if err == nil {
		return "unknown"
	}
	reason := strings.TrimSpace(err.Error())
	if reason == "" {
		return "unknown"
	}
	for _, secret := range []string{entry.Email, entry.Password, entry.MFASecret} {
		if secret != "" {
			reason = strings.ReplaceAll(reason, secret, "[redacted]")
		}
	}
	reason = strings.Map(func(r rune) rune {
		if r == '\n' || r == '\r' || r == '\t' {
			return ' '
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, reason)
	if len(reason) > 160 {
		return reason[:160] + "..."
	}
	return reason
}

// twoFALoginPublicErrorCodes 是可以安全回传给前端的失败原因短码。
// 上游返回的任意文本（可能夹带凭据或内部信息）一律折叠成 unknown。
var twoFALoginPublicErrorCodes = map[string]bool{
	"phone_verification_required": true,
	"account_deactivated":         true,
	"account_deleted":             true,
	"account_retired":             true,
	"dead_account":                true,
	"account_not_found":           true,
	"requires_relogin":            true,
	"password_rejected":           true,
	"mfa_required":                true,
	"security_check":              true,
	"authorization_denied":        true,
	"rate_limited":                true,
	"timeout":                     true,
	"transport_error":             true,
	"service_error":               true,
	"unknown":                     true,
}

func twoFALoginPublicErrorCode(reason string) string {
	code := strings.ToLower(strings.TrimSpace(reason))
	if twoFALoginPublicErrorCodes[code] {
		return code
	}
	// 重登接口返回非 2xx 时 reason 是「重登接口返回 HTTP 5xx」，对用户即上游服务异常。
	if strings.HasPrefix(reason, "重登接口返回 HTTP") {
		return "service_error"
	}
	return "unknown"
}

// twoFALoginAccountErrorCodes 是需要在账号列表留痕的失败原因（账号/凭据层面的问题）。
// 网络、限流、上游 5xx 等临时异常不登记，避免把可恢复的号刷成错误态。
var twoFALoginAccountErrorCodes = map[string]bool{
	"account_deactivated":         true,
	"account_deleted":             true,
	"account_retired":             true,
	"dead_account":                true,
	"account_not_found":           true,
	"password_rejected":           true,
	"mfa_required":                true,
	"security_check":              true,
	"phone_verification_required": true,
	"authorization_denied":        true,
}

// twoFALoginBannedCodes 是不可恢复的封号类原因：命中后不再重复登录，
// 避免每次重试都触发一封验证码邮件。
var twoFALoginBannedCodes = map[string]bool{
	"account_deactivated": true,
	"account_deleted":     true,
	"account_retired":     true,
	"dead_account":        true,
	"account_not_found":   true,
	// 密码错 / 需要 2FA 但没给密钥：重试同样过不了，直接跳过。
	"password_rejected": true,
	"mfa_required":      true,
}

// TwoFALoginSkippedError 表示账号已被标记为不可用（封号 / 死号），本次登录被跳过。
type TwoFALoginSkippedError struct {
	AccountID int64
	// Code 是账号记录里的失败原因短码，如 account_deactivated / dead_account。
	Code string
	// Status 是账号列表里显示的说明，如「封号」「死号（…）」。
	Status string
}

func (e *TwoFALoginSkippedError) Error() string {
	return fmt.Sprintf("该账号已标记为%s，已跳过登录（账号 #%d）", e.Status, e.AccountID)
}

// findBannedAccountByEmail 找到同名的已封号账号（用于跳过重复登录）。
func (s *AccountTokenGuardService) findBannedAccountByEmail(ctx context.Context, email string) *Account {
	if s.accounts == nil || strings.TrimSpace(email) == "" {
		return nil
	}
	list, err := s.accounts.ListAllWithFilters(ctx, PlatformOpenAI, "", "", email, 0, "")
	if err != nil {
		slog.Warn("account_token_guard_skip_lookup_failed", "email", email, "error", err)
		return nil
	}
	for i := range list {
		account := list[i]
		if !strings.EqualFold(strings.TrimSpace(account.Name), email) {
			continue
		}
		if account.Status != domain.StatusError {
			continue
		}
		code := strings.TrimSpace(guardText(account.Extra["login_error_code"]))
		if twoFALoginBannedCodes[code] ||
			strings.Contains(account.ErrorMessage, "封号") ||
			strings.Contains(account.ErrorMessage, "停用") ||
			strings.Contains(account.ErrorMessage, "死号") {
			return &list[i]
		}
	}
	return nil
}

// twoFALoginAccountErrorMessage 把上游短码转成账号列表里显示的错误说明。
func twoFALoginAccountErrorMessage(code string) string {
	switch code {
	case "account_deactivated":
		return "封号"
	case "account_deleted":
		return "封号（账号已删除）"
	case "account_retired":
		return "封号（账号已删除或停用）"
	case "dead_account":
		return "死号（邮箱失效，收不到验证码且无密码）"
	case "account_not_found":
		return "无此账号（被引导注册，需先注册 OpenAI 账号）"
	case "password_rejected":
		return "密码错误或被拒绝"
	case "mfa_required":
		return "需要 MFA / 验证码"
	case "security_check":
		return "需要安全验证"
	case "phone_verification_required":
		return "需要手机号验证"
	case "authorization_denied":
		return "授权被拒绝"
	case "requires_relogin":
		return "凭据失效，需重新登录"
	case "rate_limited":
		return "上游限流"
	case "timeout":
		return "请求超时"
	case "transport_error":
		return "网络连接异常"
	case "service_error":
		return "上游服务异常"
	case "unknown", "":
		return "未知异常，原因未确认"
	}
	return code
}

// TwoFALoginErrorMessage 把失败短码转成可展示的中文原因，供 API 层复用。
func TwoFALoginErrorMessage(code string) string {
	return twoFALoginAccountErrorMessage(strings.TrimSpace(code))
}

// recordFailedTwoFALoginAccount 把登录失败的邮箱登记为错误态账号：
// 已存在同邮箱账号时只更新错误信息并停止调度，不重复建号。
// 返回账号 ID（0 表示未登记，例如网络类失败或仓储不可用）。
func (s *AccountTokenGuardService) recordFailedTwoFALoginAccount(ctx context.Context, entry AccountTokenGuardReloginAccount, code string) int64 {
	if s.accounts == nil || !twoFALoginAccountErrorCodes[code] {
		return 0
	}
	email := strings.TrimSpace(entry.Email)
	if email == "" {
		return 0
	}
	message := twoFALoginAccountErrorMessage(code)

	existing, err := s.accounts.ListAllWithFilters(ctx, PlatformOpenAI, "", "", email, 0, "")
	if err != nil {
		// 查不到不等于不存在：宁可这次不登记，也不要重复建号。
		slog.Warn("account_token_guard_two_fa_login_lookup_failed", "email", email, "reason", code, "error", err)
		return 0
	}
	for _, account := range existing {
		if !strings.EqualFold(strings.TrimSpace(account.Name), email) {
			continue
		}
		if err := s.accounts.SetError(ctx, account.ID, message); err != nil {
			slog.Warn("account_token_guard_two_fa_login_mark_error_failed", "account_id", account.ID, "reason", code, "error", err)
			return 0
		}
		_ = s.accounts.SetSchedulable(ctx, account.ID, false)
		return account.ID
	}

	account := &Account{
		Name:         truncateGuardText(email, 100),
		Platform:     PlatformOpenAI,
		Type:         AccountTypeOAuth,
		Credentials:  map[string]any{},
		Extra:        map[string]any{"source": "two_fa_login", "login_error_code": code},
		Status:       domain.StatusError,
		ErrorMessage: message,
		Concurrency:  3,
		Priority:     50,
		Schedulable:  false,
	}
	if err := s.accounts.Create(ctx, account); err != nil {
		slog.Warn("account_token_guard_two_fa_login_account_create_failed", "reason", code, "error", err)
		return 0
	}
	return account.ID
}

// Merge into the latest persisted config, not the snapshot from login start:
// logins can take minutes, and other imports/config edits may finish meanwhile.
func (s *AccountTokenGuardService) saveTwoFALoginAccount(ctx context.Context, entry AccountTokenGuardReloginAccount) error {
	s.configMu.Lock()
	defer s.configMu.Unlock()
	cfg, err := s.getConfigLocked(ctx)
	if err != nil {
		return err
	}
	// GetConfig also caches this slice; do not mutate the published snapshot.
	accounts := make([]AccountTokenGuardReloginAccount, 0, len(cfg.ReloginAccounts)+1)
	for _, existing := range cfg.ReloginAccounts {
		if existing.Email != entry.Email {
			accounts = append(accounts, existing)
		}
	}
	cfg.ReloginAccounts = append(accounts, entry)
	_, err = s.saveConfigLocked(ctx, cfg)
	return err
}

// Only token/session fields enter the existing Session importer. Discard any
// echoed password, MFA secret, or unexpected service configuration fields.
func twoFALoginCredential(in map[string]any, email string) map[string]any {
	out := map[string]any{"email": email}
	for _, key := range []string{"access_token", "refresh_token", "id_token", "expires_at", "expired", "account_id", "chatgpt_account_id", "chatgpt_user_id", "user_id", "client_id", "plan_type"} {
		switch value := in[key].(type) {
		case string:
			out[key] = value
		case float64:
			if key == "expires_at" || key == "expired" {
				out[key] = value
			}
		}
	}
	return out
}

func (s *AccountTokenGuardService) TwoFALogin(id string) (*OpenAITwoFALoginJob, bool) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	job, ok := s.logins[id]
	if !ok {
		return nil, false
	}
	snapshot := &OpenAITwoFALoginJob{ID: job.ID, Status: job.Status, ErrorCode: job.ErrorCode, AccountID: job.AccountID}
	if job.Credential != nil {
		snapshot.Credential = make(map[string]any, len(job.Credential))
		for key, value := range job.Credential {
			snapshot.Credential[key] = value
		}
	}
	return snapshot, true
}

func (s *AccountTokenGuardService) DeleteTwoFALogin(id string) {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	if job, ok := s.logins[id]; ok {
		job.cancel()
		job.expiry.Stop()
		job.Credential = nil
		delete(s.logins, id)
	}
}

func (s *AccountTokenGuardService) clearTwoFALogins() {
	s.loginMu.Lock()
	defer s.loginMu.Unlock()
	for id, job := range s.logins {
		job.cancel()
		job.expiry.Stop()
		job.Credential = nil
		delete(s.logins, id)
	}
}
