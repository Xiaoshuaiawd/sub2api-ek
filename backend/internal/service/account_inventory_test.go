//go:build unit

package service

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

type inventoryRepoStub struct {
	AccountInventoryRepository
	entries []AccountInventoryEncryptedEntry
}

func (r *inventoryRepoStub) Import(_ context.Context, entries []AccountInventoryEncryptedEntry) (int, error) {
	r.entries = entries
	return len(entries), nil
}

type inventoryEncryptorStub struct{ OpenAICredentialEncryptor }

func (inventoryEncryptorStub) EncryptionStatus() (CredentialEncryptionStatus, error) {
	return CredentialEncryptionStatus{Configured: true}, nil
}
func (inventoryEncryptorStub) Encrypt(value string) (string, error) {
	return "cipher:" + value, nil
}

func TestAccountInventoryImportEncryptsBothSecrets(t *testing.T) {
	repo := &inventoryRepoStub{}
	svc := NewAccountInventoryService(repo, inventoryEncryptorStub{}, nil)
	result, err := svc.Import(context.Background(), "USER@example.com----password----MFASECRET")
	require.NoError(t, err)
	require.Equal(t, 1, result.Imported)
	require.Equal(t, "user@example.com", repo.entries[0].Email)
	require.Equal(t, "cipher:password", repo.entries[0].PasswordCiphertext)
	require.Equal(t, "cipher:MFASECRET", repo.entries[0].MFACiphertext)
}

type inventoryWorkerRepo struct {
	AccountInventoryRepository
	status      AccountInventoryStatus
	claims      []AccountInventoryClaim
	bannedIDs   []int64
	completed   []string
	retired     []int64
	canActivate bool
}

func (r *inventoryWorkerRepo) Status(context.Context) (AccountInventoryStatus, error) {
	return r.status, nil
}
func (r *inventoryWorkerRepo) Claim(context.Context, int) ([]AccountInventoryClaim, error) {
	return r.claims, nil
}
func (r *inventoryWorkerRepo) CanActivate(context.Context, int64) (bool, error) {
	return r.canActivate, nil
}
func (r *inventoryWorkerRepo) Complete(_ context.Context, _ int64, state string, _ int64, code string) error {
	r.completed = append(r.completed, state+":"+code)
	return nil
}
func (r *inventoryWorkerRepo) ConfirmedBannedAccountIDs(context.Context) ([]int64, error) {
	return r.bannedIDs, nil
}
func (r *inventoryWorkerRepo) RetireAccount(_ context.Context, id int64) error {
	r.retired = append(r.retired, id)
	return nil
}

type inventoryActivatorStub struct {
	entry     AccountTokenGuardReloginAccount
	code      string
	duplicate bool
}

func (a *inventoryActivatorStub) ActivateInventoryAccount(ctx context.Context, entry AccountTokenGuardReloginAccount, canImport func(context.Context) (bool, error)) (int64, bool, string, error) {
	a.entry = entry
	allowed, err := canImport(ctx)
	if err != nil || !allowed {
		return 0, false, "capacity_full", errors.New("capacity changed")
	}
	if a.code != "" {
		return 0, false, a.code, errors.New("safe fixture error")
	}
	return 19, a.duplicate, "", nil
}

type inventoryDeleterStub struct {
	deleted []int64
	err     error
}

func (d *inventoryDeleterStub) DeleteAccount(_ context.Context, id int64) error {
	d.deleted = append(d.deleted, id)
	return d.err
}

type decryptingInventoryEncryptor struct{ inventoryEncryptorStub }

func (decryptingInventoryEncryptor) Decrypt(ciphertext string) (string, error) {
	return strings.TrimPrefix(ciphertext, "cipher:"), nil
}

func TestAccountInventoryRunOnceDeletesConfirmedBanAndActivatesReplacement(t *testing.T) {
	repo := &inventoryWorkerRepo{
		status: AccountInventoryStatus{TargetCount: 1, Enabled: true},
		claims: []AccountInventoryClaim{{ID: 7, AccountInventoryEncryptedEntry: AccountInventoryEncryptedEntry{
			Email: "new@example.com", PasswordCiphertext: "cipher:password", MFACiphertext: "cipher:mfa",
		}}},
		bannedIDs: []int64{10}, canActivate: true,
	}
	activator := &inventoryActivatorStub{}
	deleter := &inventoryDeleterStub{}
	svc := NewAccountInventoryService(repo, decryptingInventoryEncryptor{}, activator)
	svc.SetDeleter(deleter)
	require.NoError(t, svc.RunOnce(context.Background()))
	require.Equal(t, []int64{10}, repo.retired)
	require.Equal(t, []int64{10}, deleter.deleted)
	require.Equal(t, "password", activator.entry.Password)
	require.Equal(t, []string{"active:"}, repo.completed)
}

func TestAccountInventoryDoesNotTreat401AsBan(t *testing.T) {
	require.False(t, inventoryPermanentFailure("requires_relogin"))
	require.False(t, inventoryPermanentFailure("rate_limited"))
	require.True(t, inventoryPermanentFailure("account_deactivated"))
}

func TestAccountInventoryKeepsStockCredentialsIfBanDeleteFails(t *testing.T) {
	repo := &inventoryWorkerRepo{status: AccountInventoryStatus{TargetCount: 1, Enabled: true}, bannedIDs: []int64{10}}
	deleter := &inventoryDeleterStub{err: errors.New("delete failed")}
	svc := NewAccountInventoryService(repo, decryptingInventoryEncryptor{}, &inventoryActivatorStub{})
	svc.SetDeleter(deleter)
	require.Error(t, svc.RunOnce(context.Background()))
	require.Empty(t, repo.retired)
}

func TestAccountInventoryDisabledDoesNotDeleteOrActivate(t *testing.T) {
	repo := &inventoryWorkerRepo{status: AccountInventoryStatus{TargetCount: 0, Enabled: false}, bannedIDs: []int64{10}}
	deleter := &inventoryDeleterStub{}
	svc := NewAccountInventoryService(repo, decryptingInventoryEncryptor{}, &inventoryActivatorStub{})
	svc.SetDeleter(deleter)
	require.NoError(t, svc.RunOnce(context.Background()))
	require.Empty(t, deleter.deleted)
	require.Empty(t, repo.completed)
}

func TestAccountInventoryActivationFailureClassification(t *testing.T) {
	for _, tc := range []struct {
		code, state string
	}{
		{"requires_relogin", "pending:temporary_failure"},
		{"rate_limited", "pending:temporary_failure"},
		{"account_deactivated", "failed:account_deactivated"},
	} {
		t.Run(tc.code, func(t *testing.T) {
			repo := &inventoryWorkerRepo{
				status: AccountInventoryStatus{TargetCount: 1, Enabled: true}, canActivate: true,
				claims: []AccountInventoryClaim{{ID: 7, AccountInventoryEncryptedEntry: AccountInventoryEncryptedEntry{
					Email: "user@example.com", PasswordCiphertext: "cipher:password", MFACiphertext: "cipher:mfa",
				}}},
			}
			svc := NewAccountInventoryService(repo, decryptingInventoryEncryptor{}, &inventoryActivatorStub{code: tc.code})
			require.NoError(t, svc.RunOnce(context.Background()))
			require.Equal(t, []string{tc.state}, repo.completed)
		})
	}
}

func TestAccountInventoryExistingIdentityDoesNotConsumeNewSlot(t *testing.T) {
	repo := &inventoryWorkerRepo{
		status: AccountInventoryStatus{TargetCount: 1, Enabled: true}, canActivate: true,
		claims: []AccountInventoryClaim{{ID: 7, AccountInventoryEncryptedEntry: AccountInventoryEncryptedEntry{
			Email: "user@example.com", PasswordCiphertext: "cipher:password", MFACiphertext: "cipher:mfa",
		}}},
	}
	svc := NewAccountInventoryService(repo, decryptingInventoryEncryptor{}, &inventoryActivatorStub{duplicate: true})
	require.NoError(t, svc.RunOnce(context.Background()))
	require.Equal(t, []string{"duplicate:"}, repo.completed)
}

func TestAccountInventoryCorrectedFailedRowCanBypassOldLoginSkip(t *testing.T) {
	repo := &inventoryWorkerRepo{
		status: AccountInventoryStatus{TargetCount: 1, Enabled: true}, canActivate: true,
		claims: []AccountInventoryClaim{{ID: 7, ForceLogin: true, AccountInventoryEncryptedEntry: AccountInventoryEncryptedEntry{
			Email: "user@example.com", PasswordCiphertext: "cipher:new-password", MFACiphertext: "cipher:new-mfa",
		}}},
	}
	activator := &inventoryActivatorStub{}
	svc := NewAccountInventoryService(repo, decryptingInventoryEncryptor{}, activator)
	require.NoError(t, svc.RunOnce(context.Background()))
	require.True(t, activator.entry.Force)
}
