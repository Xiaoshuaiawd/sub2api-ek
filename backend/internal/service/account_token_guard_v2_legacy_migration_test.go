package service

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type legacyReloginTestReader struct {
	*reauthTestAccountReader
	listCalls  int
	additional []Account
}

func (r *legacyReloginTestReader) ListAllWithFilters(context.Context, string, string, string, string, int64, string) ([]Account, error) {
	r.listCalls++
	return append([]Account{*r.account}, r.additional...), nil
}

func newLegacyReloginMigrationFixture(raw string) (*AccountTokenGuardV2Service, *legacyReloginTestReader, *legacyReloginTestRepo, *reauthRuntimeTestSettings) {
	reauth, reader, _, _, _, _ := newReauthTestService("acct-1")
	reader.account.Name, reader.account.Status = "user@example.com", StatusActive
	legacyReader := &legacyReloginTestReader{reauthTestAccountReader: reader}
	guard := &accountTokenGuardV2TestRepo{}
	login := &legacyReloginTestRepo{guard: guard}
	reauth.repo = login
	settings := &reauthRuntimeTestSettings{values: map[string]string{legacyTokenGuardSettingsKey: raw}}
	reauth.settings = settings
	return NewAccountTokenGuardV2Service(guard, settings, legacyReader, accountTokenGuardV2TestProber{}, reauth), legacyReader, login, settings
}

func TestAccountTokenGuardV2LegacyMigrationPreservesExistingConfigurations(t *testing.T) {
	const raw = `{"enabled":true,"relogin_accounts":[{"email":"user@example.com","password":"legacy-password"}]}`
	for _, existing := range []string{"guard", "login"} {
		t.Run(existing, func(t *testing.T) {
			svc, _, repo, settings := newLegacyReloginMigrationFixture(raw)
			if existing == "guard" {
				repo.guard.record = &AccountTokenGuardV2Record{AccountID: 42, Enabled: false, AutoReloginEnabled: false}
			} else {
				repo.config = &OpenAIOAuthReauthStoredConfig{AccountID: 42, PasswordCiphertext: "existing-encrypted", CredentialMode: OpenAIOAuthReauthModePasswordTOTP}
			}
			count, err := svc.MigrateLegacyReloginAccounts(context.Background())
			require.NoError(t, err)
			require.Zero(t, count)
			if existing == "guard" {
				require.Nil(t, repo.config)
				require.False(t, repo.guard.record.Enabled)
			} else {
				require.Nil(t, repo.guard.record)
				require.Equal(t, "existing-encrypted", repo.config.PasswordCiphertext)
			}
			require.Equal(t, raw, settings.values[legacyTokenGuardSettingsKey])
		})
	}
}

func TestAccountTokenGuardV2LegacyMigrationRetainsSwitchesAndGroupScope(t *testing.T) {
	for _, tc := range []struct {
		name, config  string
		groups        []int64
		enabled, auto bool
	}{
		{"defaults paused", "", nil, false, true},
		{"enabled all", `"enabled":true,`, nil, true, true},
		{"enabled inside group", `"enabled":true,"auto_relogin":false,"group_ids":[7],`, []int64{7}, true, false},
		{"outside group stays paused", `"enabled":true,"group_ids":[7],`, []int64{8}, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw := fmt.Sprintf(`{%s"relogin_accounts":[{"email":"user@example.com","password":"legacy-password","mfa_secret":"totp"}]}`, tc.config)
			svc, reader, repo, _ := newLegacyReloginMigrationFixture(raw)
			reader.account.GroupIDs = tc.groups
			count, err := svc.MigrateLegacyReloginAccounts(context.Background())
			require.NoError(t, err)
			require.Equal(t, 1, count)
			require.Equal(t, tc.enabled, repo.guard.record.Enabled)
			require.Equal(t, tc.auto, repo.guard.record.AutoReloginEnabled)
			require.Equal(t, OpenAIOAuthReauthModePasswordTOTP, repo.config.CredentialMode)
			require.NotEmpty(t, repo.config.PasswordCiphertext)
		})
	}
}

func TestAccountTokenGuardV2LegacyMigrationRetriesEncryptionWithoutChangingSource(t *testing.T) {
	const raw = `{"enabled":true,"relogin_accounts":[{"email":"user@example.com","mail_client_id":"client","mail_refresh_token":"refresh-secret"}]}`
	svc, reader, repo, settings := newLegacyReloginMigrationFixture(raw)
	encryptor := &managedReauthTestEncryptor{}
	svc.reauth.encryptor = encryptor
	count, err := svc.MigrateLegacyReloginAccounts(context.Background())
	require.Error(t, err)
	require.Zero(t, count)
	require.Nil(t, repo.config)
	require.Equal(t, raw, settings.values[legacyTokenGuardSettingsKey])
	encryptor.ready = true
	count, err = svc.MigrateLegacyReloginAccounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, count)
	count, err = svc.MigrateLegacyReloginAccounts(context.Background())
	require.NoError(t, err)
	require.Zero(t, count)
	require.Equal(t, 1, reader.listCalls, "successful migration is idempotent in later cycles")
	require.Equal(t, raw, settings.values[legacyTokenGuardSettingsKey])
}

type multiLegacyReloginGuardRepo struct {
	accountTokenGuardV2TestRepo
	rows map[int64]*AccountTokenGuardV2Record
}

func (r *multiLegacyReloginGuardRepo) GetAccount(_ context.Context, id int64) (*AccountTokenGuardV2Record, error) {
	return r.rows[id], nil
}

type multiLegacyReloginLoginRepo struct {
	reauthTestRepo
	rows  map[int64]*OpenAIOAuthReauthStoredConfig
	guard *multiLegacyReloginGuardRepo
}

func (r *multiLegacyReloginLoginRepo) GetConfig(_ context.Context, id int64) (*OpenAIOAuthReauthStoredConfig, error) {
	return r.rows[id], nil
}
func (r *multiLegacyReloginLoginRepo) ImportLegacyReloginConfig(_ context.Context, config *OpenAIOAuthReauthStoredConfig, enabled, auto bool) (bool, error) {
	if r.rows[config.AccountID] != nil || r.guard.rows[config.AccountID] != nil {
		return false, nil
	}
	r.rows[config.AccountID] = config
	r.guard.rows[config.AccountID] = &AccountTokenGuardV2Record{AccountID: config.AccountID, Enabled: enabled, AutoReloginEnabled: auto}
	return true, nil
}

func TestAccountTokenGuardV2LegacyMigrationHandlesAllExactEmailParents(t *testing.T) {
	const raw = `{"enabled":true,"relogin_accounts":[{"email":"user@example.com","mail_client_id":"client","mail_refresh_token":"refresh-secret"}]}`
	svc, reader, _, _ := newLegacyReloginMigrationFixture(raw)
	second := *reader.account
	second.ID = 43
	shadow := second
	shadow.ID, shadow.ParentAccountID = 44, int64Ptr(42)
	unrelated := second
	unrelated.ID, unrelated.Name = 45, "other-user@example.com"
	reader.additional = []Account{second, shadow, unrelated}
	guard := &multiLegacyReloginGuardRepo{rows: make(map[int64]*AccountTokenGuardV2Record)}
	login := &multiLegacyReloginLoginRepo{rows: make(map[int64]*OpenAIOAuthReauthStoredConfig), guard: guard}
	svc.repo, svc.reauth.repo = guard, login
	count, err := svc.MigrateLegacyReloginAccounts(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, count)
	require.Len(t, login.rows, 2)
	require.NotNil(t, login.rows[42])
	require.NotNil(t, login.rows[43])
}

func TestAccountTokenGuardV2LegacyMigrationFailureDoesNotStopExistingCycle(t *testing.T) {
	reauth, reader, _, _, _, _ := newReauthTestService("acct-1")
	reader.account.Name, reader.account.Status = "user@example.com", StatusActive
	guard := &legacyReloginCycleRepo{cycle: make(chan struct{}, 1)}
	settings := &reauthRuntimeTestSettings{values: map[string]string{legacyTokenGuardSettingsKey: `{invalid-private-legacy-data`}}
	svc := NewAccountTokenGuardV2Service(guard, settings, &legacyReloginTestReader{reauthTestAccountReader: reader}, accountTokenGuardV2TestProber{}, reauth)
	svc.Start()
	defer svc.Stop()
	select {
	case <-guard.cycle:
	case <-time.After(2 * time.Second):
		t.Fatal("legacy migration failure blocked the existing V2 cycle")
	}
}

type legacyReloginTestRepo struct {
	reauthTestRepo
	guard *accountTokenGuardV2TestRepo
}

func (r *legacyReloginTestRepo) ImportLegacyReloginConfig(_ context.Context, stored *OpenAIOAuthReauthStoredConfig, enabled, autoRelogin bool) (bool, error) {
	if r.config != nil || r.guard.record != nil {
		return false, nil
	}
	copy := *stored
	r.config = &copy
	r.guard.record = &AccountTokenGuardV2Record{AccountID: stored.AccountID, Enabled: enabled, AutoReloginEnabled: autoRelogin}
	return true, nil
}

type legacyReloginCycleRepo struct {
	accountTokenGuardV2TestRepo
	cycle chan struct{}
}

func (r *legacyReloginCycleRepo) ClaimDue(context.Context, string, time.Duration, int, time.Time) ([]AccountTokenGuardV2Record, error) {
	select {
	case r.cycle <- struct{}{}:
	default:
	}
	return nil, nil
}

func TestAccountTokenGuardV2MigratesLegacyMailboxInExistingCycle(t *testing.T) {
	reauth, reader, _, _, _, _ := newReauthTestService("acct-1")
	reader.account.Name, reader.account.Status = "user@example.com", StatusActive
	legacyReader := &legacyReloginTestReader{reauthTestAccountReader: reader}
	guardRepo := &legacyReloginCycleRepo{cycle: make(chan struct{}, 1)}
	loginRepo := &legacyReloginTestRepo{guard: &guardRepo.accountTokenGuardV2TestRepo}
	reauth.repo = loginRepo
	settings := &reauthRuntimeTestSettings{values: map[string]string{legacyTokenGuardSettingsKey: `{"enabled":true,"auto_relogin":false,"relogin_accounts":[{"email":"user@example.com","mail_client_id":"client","mail_refresh_token":"refresh-secret"}]}`}}
	reauth.settings = settings
	svc := NewAccountTokenGuardV2Service(guardRepo, settings, legacyReader, accountTokenGuardV2TestProber{}, reauth)
	svc.Start()
	defer svc.Stop()
	select {
	case <-guardRepo.cycle:
	case <-time.After(2 * time.Second):
		t.Fatal("V2 cycle did not finish")
	}
	require.NotNil(t, loginRepo.config, "legacy mailbox credentials must migrate inside the existing V2 cycle")
	require.Equal(t, OpenAIOAuthReauthModeMailboxGraph, loginRepo.config.CredentialMode)
	require.NotEmpty(t, loginRepo.config.MailboxCredentialsCiphertext)
	require.True(t, guardRepo.record.Enabled)
	require.False(t, guardRepo.record.AutoReloginEnabled)
}
