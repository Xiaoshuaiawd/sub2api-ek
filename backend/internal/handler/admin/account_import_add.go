package admin

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/mail"
	"strings"
	"time"

	"github.com/Wei-Shaw/sub2api/internal/pkg/response"
	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
)

// addAccountLoginTimeout 限制单条账号首次 2FA 登录的等待时间。
const addAccountLoginTimeout = 5 * time.Minute

// AddAccountImportRequest 是「一键 2FA 登录并导入」的入参。
type AddAccountImportRequest struct {
	// AccountInfo 支持两种行格式：
	//   邮箱----GPT密码----2FA
	//   邮箱----邮箱密码----client_id----refresh_token----GPT密码----2FA
	AccountInfo string `json:"account_info"`
	// Name 可选，留空则使用邮箱作为账号名。
	Name string `json:"name"`
	// UpdateExisting 可选：账号已存在时是否用本次登录得到的新令牌覆盖（默认 false，沿用页面默认配置）。
	UpdateExisting *bool `json:"update_existing"`
	// Force 可选：跳过「已封号账号不再重试」的保护，强制重新登录一次。
	Force *bool `json:"force"`
}

type addAccountCredentials struct {
	Email       string
	MailboxPass string
	ClientID    string
	MailboxRT   string
	GPTPassword string
	MFASecret   string
}

func parseAddAccountInfo(raw string) (addAccountCredentials, error) {
	var c addAccountCredentials
	parts := strings.Split(strings.TrimSpace(raw), "----")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	switch len(parts) {
	case 3:
		c.Email, c.GPTPassword, c.MFASecret = parts[0], parts[1], parts[2]
	case 6:
		c.Email, c.MailboxPass, c.ClientID, c.MailboxRT = parts[0], parts[1], parts[2], parts[3]
		c.GPTPassword, c.MFASecret = parts[4], parts[5]
	default:
		return c, errors.New("account_info 需要用 ---- 分隔成 3 段或 6 段")
	}
	addr, err := mail.ParseAddress(c.Email)
	if err != nil || addr.Address != c.Email {
		return c, errors.New("account_info 中的邮箱格式不正确")
	}
	if len(parts) == 3 {
		if c.GPTPassword == "" || c.MFASecret == "" {
			return c, errors.New("3 段格式需要 GPT 密码和 2FA 密钥")
		}
		return c, nil
	}
	// 6 段格式：至少要能用其中一种方式登录。
	if c.ClientID == "" || c.MailboxRT == "" {
		return c, errors.New("6 段格式需要 client_id 和邮箱 refresh_token")
	}
	if c.GPTPassword == "" && c.MFASecret == "" {
		// 没有 GPT 密码 / 2FA：只能走邮箱验证码，凭据已具备，允许导入。
		return c, nil
	}
	return c, nil
}

// ImportAddAccount 用一条「邮箱----GPT密码----2FA」记录完成首次登录并按默认配置入库，
// 返回可直接回填的 8 段结果行（含 GPT access_token / refresh_token）。
func (h *AccountHandler) ImportAddAccount(c *gin.Context) {
	var req AddAccountImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		response.BadRequest(c, "Invalid request: "+err.Error())
		return
	}
	if h.tokenGuard == nil {
		response.Error(c, http.StatusServiceUnavailable, "凭证守护服务不可用，无法完成首次登录")
		return
	}
	cred, err := parseAddAccountInfo(req.AccountInfo)
	if err != nil {
		response.BadRequest(c, err.Error())
		return
	}
	ctx := c.Request.Context()
	email := strings.ToLower(cred.Email)

	job, err := h.tokenGuard.StartTwoFALogin(ctx, service.AccountTokenGuardReloginAccount{
		Email:            email,
		Password:         cred.GPTPassword,
		MFASecret:        cred.MFASecret,
		MailClientID:     cred.ClientID,
		MailRefreshToken: cred.MailboxRT,
		Force:            req.Force != nil && *req.Force,
	})
	if err != nil {
		var skipped *service.TwoFALoginSkippedError
		if errors.As(err, &skipped) {
			response.ErrorWithDetails(c, http.StatusBadRequest,
				err.Error()+"；如需强制重试请传 force=true", skipped.Code,
				map[string]string{"error_code": skipped.Code, "email": email, "skipped": "true"})
			return
		}
		response.BadRequest(c, err.Error())
		return
	}
	defer h.tokenGuard.DeleteTwoFALogin(job.ID)

	result, err := h.waitTwoFALogin(ctx, job.ID)
	if err != nil {
		response.Error(c, http.StatusGatewayTimeout, err.Error())
		return
	}
	if result.Status != "succeeded" || result.Credential == nil {
		code := result.ErrorCode
		response.ErrorWithDetails(c, http.StatusBadRequest,
			"登录失败："+service.TwoFALoginErrorMessage(code), code,
			map[string]string{"error_code": code, "email": email})
		return
	}

	credentialJSON, err := json.Marshal(result.Credential)
	if err != nil {
		response.InternalError(c, "凭据序列化失败")
		return
	}
	importReq := defaultAddAccountImportRequest(string(credentialJSON), req.Name, email, req.UpdateExisting)
	entries, err := parseCodexSessionImportEntries(importReq)
	if err != nil || len(entries) == 0 {
		response.BadRequest(c, "登录返回的凭据无法导入")
		return
	}
	imported, err := h.importCodexSessions(ctx, importReq, entries)
	if err != nil {
		response.ErrorFrom(c, err)
		return
	}
	if imported.Created == 0 && imported.Updated == 0 && imported.Skipped == 0 {
		message := "导入失败"
		if len(imported.Errors) > 0 && imported.Errors[0].Message != "" {
			message = imported.Errors[0].Message
		}
		response.BadRequest(c, message)
		return
	}
	accountID := int64(0)
	action := ""
	for _, item := range imported.Items {
		if item.AccountID > 0 {
			accountID = item.AccountID
			action = item.Action
			break
		}
	}
	accessToken := addAccountText(result.Credential["access_token"])
	refreshToken := addAccountText(result.Credential["refresh_token"])
	line := strings.Join([]string{
		email, cred.MailboxPass, cred.ClientID, cred.MailboxRT,
		cred.GPTPassword, cred.MFASecret, accessToken, refreshToken,
	}, "----")
	response.Success(c, gin.H{
		"email":         email,
		"account_id":    accountID,
		"action":        action,
		"account_info":  line,
		"access_token":  accessToken,
		"refresh_token": refreshToken,
		"plan_type":     addAccountText(result.Credential["plan_type"]),
	})
}

// defaultAddAccountImportRequest 固化网页端的默认导入配置，保证接口调用与页面导入结果一致。
func defaultAddAccountImportRequest(content, name, email string, updateExistingOverride *bool) CodexSessionImportRequest {
	accountName := strings.TrimSpace(name)
	if accountName == "" {
		accountName = email
	}
	concurrency := 1000
	priority := 1
	rateMultiplier := 1.0
	autoPause := true
	// 本次已拿到新登录令牌，默认覆盖已存在账号，避免库里留着旧 refresh_token；
	// 需要「只导新号、跳过已存在」时传 update_existing=false。
	updateExisting := true
	if updateExistingOverride != nil {
		updateExisting = *updateExistingOverride
	}
	return CodexSessionImportRequest{
		InitializeOpenAITwoFA: true,
		Content:               content,
		Name:                  accountName,
		Concurrency:           &concurrency,
		Priority:              &priority,
		RateMultiplier:        &rateMultiplier,
		AutoPauseOnExpired:    &autoPause,
		UpdateExisting:        &updateExisting,
		SkipExisting:          !updateExisting,
		Extra: map[string]any{
			"openai_request_timezone":                      "Asia/Singapore",
			"openai_oauth_responses_websockets_v2_mode":    "ctx_pool",
			"openai_oauth_responses_websockets_v2_enabled": true,
			"openai_oauth_ws_sse_acceleration":             true,
			"openai_long_context_billing_enabled":          true,
			"codex_fingerprint_mode":                       "full",
			"cost_multiplier":                              0.1,
		},
	}
}

func (h *AccountHandler) waitTwoFALogin(ctx context.Context, id string) (*service.OpenAITwoFALoginJob, error) {
	deadline := time.Now().Add(addAccountLoginTimeout)
	for {
		job, ok := h.tokenGuard.TwoFALogin(id)
		if !ok {
			return nil, errors.New("登录任务不存在或已过期")
		}
		if job.Status != "running" {
			return job, nil
		}
		if time.Now().After(deadline) {
			return nil, errors.New("登录超时，请稍后重试")
		}
		select {
		case <-ctx.Done():
			return nil, errors.New("请求已取消")
		case <-time.After(time.Second):
		}
	}
}

func addAccountText(value any) string {
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}
