//go:build unit

package service

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type recorded401GuardRepo struct {
	*accountTokenGuardV2TestRepo
	ids []int64
}

func (r *recorded401GuardRepo) ListRecorded401(context.Context, int) ([]int64, error) {
	return r.ids, nil
}

func (r *recorded401GuardRepo) CreateRecorded401Relogin(ctx context.Context, id int64, hash, owner string, rules AccountTokenGuardV2Rules, now time.Time, expectedError string) (*OpenAIOAuthReauthTaskRecord, error) {
	if expectedError == "" {
		return nil, errors.New("missing recorded error")
	}
	return r.CreateAutomaticRelogin(ctx, id, hash, owner, rules, now)
}

func TestRecorded401OnlyMatchesOpenAIOAuthParentWithSavedError(t *testing.T) {
	base := Account{Platform: PlatformOpenAI, Type: AccountTypeOAuth, Status: StatusError, ErrorMessage: "Token revoked (401): credential revoked"}
	require.True(t, isRecordedOpenAI401(&base))
	base.ErrorMessage = "Unauthorized (401): account authentication failed"
	require.True(t, isRecordedOpenAI401(&base))
	base.ErrorMessage = "Access forbidden (403): quoted 401 in provider text"
	require.False(t, isRecordedOpenAI401(&base))
	base.ErrorMessage = `Authentication failed (401): {"error":{"code":"provider_unauthorized","message":"token_revoked"}}`
	require.False(t, isRecordedOpenAI401(&base))
	base.ErrorMessage = `Authentication failed (401): token_revoked`
	require.False(t, isRecordedOpenAI401(&base))
	base.ErrorMessage = `Authentication failed (401): {"error":{"code":"token_revoked","message":"private"}}`
	require.True(t, isRecordedOpenAI401(&base))
	base.Status = StatusActive
	base.ErrorMessage = "Token revoked (401): credential revoked"
	require.False(t, isRecordedOpenAI401(&base))
	base.Status, base.Platform = StatusError, PlatformGemini
	require.False(t, isRecordedOpenAI401(&base))
}

func TestRecorded401QueuesAutomaticReloginWithoutSecondProbe(t *testing.T) {
	reauth, reader, reauthRepo, _, _, _ := newReauthTestService("acct-1")
	savePasswordReauthConfig(t, reauth)
	reader.account.Status = StatusError
	reader.account.ErrorMessage = "Token revoked (401): credential revoked"
	repo := &recorded401GuardRepo{
		accountTokenGuardV2TestRepo: &accountTokenGuardV2TestRepo{record: &AccountTokenGuardV2Record{
			AccountID: 42, Enabled: true, AutoReloginEnabled: true,
			NextProbeAt: time.Now().Add(time.Hour), FailStreak: 0,
		}},
		ids: []int64{42},
	}
	repo.createTask = reauthRepo.CreateTask
	svc := NewAccountTokenGuardV2Service(repo, &accountTokenGuardV2TestSettings{}, reader, nil, reauth)
	queued, err := svc.RunRecorded401(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, queued)
	require.NotNil(t, reauthRepo.task)
	require.Equal(t, OpenAIOAuthReauthStatusQueued, reauthRepo.task.Status)
	require.Equal(t, AccountTokenGuardV2ProbeAuth, repo.completion.ProbeState)
	require.Equal(t, 1, repo.completion.FailStreak)
	require.NotNil(t, repo.completion.CooldownUntil)
	again, err := svc.RunRecorded401(context.Background())
	require.NoError(t, err)
	require.Zero(t, again, "cooldown must prevent a duplicate task")
	require.Equal(t, 1, reauthRepo.createCalls)
}

func TestRecorded401LegacyRequestScopedCandidateDoesNotQueue(t *testing.T) {
	reauth, reader, reauthRepo, _, _, _ := newReauthTestService("acct-1")
	reader.account.Status = StatusError
	reader.account.ErrorMessage = `Authentication failed (401): {"error":{"code":"provider_unauthorized","message":"token_revoked"}}`
	repo := &recorded401GuardRepo{
		accountTokenGuardV2TestRepo: &accountTokenGuardV2TestRepo{record: &AccountTokenGuardV2Record{
			AccountID: 42, Enabled: true, AutoReloginEnabled: true,
			ProbeState: AccountTokenGuardV2ProbePending, NextProbeAt: time.Now().Add(time.Hour),
		}},
		ids: []int64{42},
	}
	repo.createTask = reauthRepo.CreateTask
	svc := NewAccountTokenGuardV2Service(repo, &accountTokenGuardV2TestSettings{}, reader, nil, reauth)
	queued, err := svc.RunRecorded401(context.Background())
	require.NoError(t, err)
	require.Zero(t, queued)
	require.Nil(t, reauthRepo.task)
	require.Equal(t, AccountTokenGuardV2ProbePending, repo.completion.ProbeState)
}
