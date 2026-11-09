package service

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

type recordedOpenAI401Lister interface {
	ListRecorded401(ctx context.Context, limit int) ([]int64, error)
}

type recordedOpenAI401TaskCreator interface {
	CreateRecorded401Relogin(ctx context.Context, id int64, hash, owner string, rules AccountTokenGuardV2Rules, now time.Time, expectedError string) (*OpenAIOAuthReauthTaskRecord, error)
}

// Only known account-level 401 error messages qualify. A request-scoped 401
// that merely appears in provider prose must not create a new login session.
func isRecordedOpenAI401(account *Account) bool {
	if account == nil || account.Platform != PlatformOpenAI || account.Type != AccountTypeOAuth ||
		account.Status != StatusError || account.IsCredentialShadow() ||
		account.IsOpenAIPersonalAccessToken() || account.IsOpenAIAgentIdentity() {
		return false
	}
	message := strings.TrimSpace(account.ErrorMessage)
	if guardAccountHasAuthFailure(account) {
		return true
	}
	return message == "Authentication failed (401): refresh_token missing, cannot recover" ||
		strings.HasPrefix(message, "OAuth 401 (no refresh_token):")
}

// RunRecorded401 uses the durable re-login budget and account lease, but does
// not wait for another models probe: the account already has an upstream 401.
func (s *AccountTokenGuardV2Service) RunRecorded401(ctx context.Context) (int, error) {
	if s == nil || s.repo == nil || s.accounts == nil || s.reauth == nil {
		return 0, nil
	}
	lister, ok := s.repo.(recordedOpenAI401Lister)
	if !ok {
		return 0, nil
	}
	if _, ok := s.repo.(AccountReloginBudgetRepository); !ok {
		return 0, errors.New("automatic re-login budget is unavailable")
	}
	creator, ok := s.repo.(recordedOpenAI401TaskCreator)
	if !ok {
		return 0, errors.New("recorded 401 task creation is unavailable")
	}
	rules, err := s.GetRules(ctx)
	if err != nil {
		return 0, err
	}
	ids, err := lister.ListRecorded401(ctx, 20)
	if err != nil {
		return 0, err
	}
	queued := 0
	var firstErr error
	for _, id := range ids {
		if err := ctx.Err(); err != nil {
			return queued, err
		}
		record, claimErr := s.repo.ClaimAccount(ctx, id, s.leaseOwner, accountTokenGuardV2LeaseDuration)
		if claimErr != nil {
			if firstErr == nil {
				firstErr = claimErr
			}
			continue
		}
		if record == nil {
			continue
		}
		now := s.clockNow()
		completion := AccountTokenGuardV2ProbeCompletion{
			ProbeState: AccountTokenGuardV2ProbeAuth, ProbeDetail: "recorded OpenAI 401",
			FailStreak: max(1, record.FailStreak), LastProbeAt: now,
			NextProbeAt:   now.Add(time.Duration(rules.RetryIntervalSeconds) * time.Second),
			CooldownUntil: record.CooldownUntil,
		}
		account, accountErr := s.accounts.GetAccount(ctx, id)
		if accountErr != nil || !isRecordedOpenAI401(account) || !record.Enabled ||
			!record.AutoReloginEnabled || record.AutoReloginExhausted ||
			(record.CooldownUntil != nil && record.CooldownUntil.After(now)) {
			completion.ProbeState = record.ProbeState
			completion.ProbeDetail = record.ProbeDetail
			completion.FailStreak = record.FailStreak
			completion.NextProbeAt = record.NextProbeAt
			s.completeProbe(ctx, id, completion)
			continue
		}
		task, taskErr := s.reauth.createTaskForProfile(ctx, id, "codex", func(createCtx context.Context, accountID int64, hash string) (*OpenAIOAuthReauthTaskRecord, error) {
			return creator.CreateRecorded401Relogin(createCtx, accountID, hash, s.leaseOwner, rules, s.clockNow(), account.ErrorMessage)
		})
		if taskErr == nil && task != nil {
			completion.LastReauthAt = &now
			until := now.Add(time.Duration(rules.ReloginCooldownSeconds) * time.Second)
			completion.CooldownUntil = &until
			completion.ProbeDetail = "recorded OpenAI 401; automatic re-login queued"
			queued++
		} else if infraerrors.Reason(taskErr) == "RECORDED_401_CHANGED" {
			completion.ProbeState = record.ProbeState
			completion.ProbeDetail = record.ProbeDetail
			completion.FailStreak = record.FailStreak
			completion.NextProbeAt = record.NextProbeAt
		} else if infraerrors.Code(taskErr) == http.StatusConflict {
			completion.BlockedReason = "re-login task active or budget gated"
		} else {
			completion.BlockedReason = "automatic re-login could not be queued"
			until := now.Add(time.Duration(rules.RetryIntervalSeconds) * time.Second)
			completion.CooldownUntil = &until
			if firstErr == nil {
				if taskErr != nil {
					firstErr = taskErr
				} else {
					firstErr = errors.New("automatic re-login task was not created")
				}
			}
		}
		s.completeProbe(ctx, id, completion)
	}
	return queued, firstErr
}
