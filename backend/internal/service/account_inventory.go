package service

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"
)

type AccountInventoryEncryptedEntry struct {
	Email              string
	PasswordCiphertext string
	MFACiphertext      string
}

type AccountInventoryClaim struct {
	ID         int64
	ForceLogin bool
	AccountInventoryEncryptedEntry
}

type AccountInventoryStatus struct {
	TargetCount      int  `json:"target_count"`
	Enabled          bool `json:"enabled"`
	Available        int  `json:"available"`
	AdmissionPending int  `json:"admission_pending"`
	Pending          int  `json:"pending"`
	Activating       int  `json:"activating"`
	Active           int  `json:"active"`
	Failed           int  `json:"failed"`
	Retired          int  `json:"retired"`
	Duplicate        int  `json:"duplicate"`
}

type AccountInventoryRow struct {
	ID        int64     `json:"id"`
	Email     string    `json:"email"`
	State     string    `json:"state"`
	AccountID *int64    `json:"account_id,omitempty"`
	Attempts  int       `json:"attempts"`
	ErrorCode string    `json:"error_code,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

type AccountInventoryImportResult struct {
	Total    int `json:"total"`
	Imported int `json:"imported"`
	Skipped  int `json:"skipped"`
}

type AccountInventoryRepository interface {
	Import(ctx context.Context, entries []AccountInventoryEncryptedEntry) (int, error)
	Status(ctx context.Context) (AccountInventoryStatus, error)
	SaveConfig(ctx context.Context, target int, enabled bool) error
	List(ctx context.Context, offset, limit int) ([]AccountInventoryRow, int, error)
	Claim(ctx context.Context, limit int) ([]AccountInventoryClaim, error)
	CanActivate(ctx context.Context, claimID int64) (bool, error)
	Complete(ctx context.Context, id int64, state string, accountID int64, errorCode string) error
	ConfirmedBannedAccountIDs(ctx context.Context) ([]int64, error)
	RetireAccount(ctx context.Context, accountID int64) error
}

type AccountInventoryActivator interface {
	ActivateInventoryAccount(ctx context.Context, entry AccountTokenGuardReloginAccount, canImport func(context.Context) (bool, error)) (accountID int64, duplicate bool, errorCode string, err error)
}

type AccountInventoryDeleter interface {
	DeleteAccount(ctx context.Context, accountID int64) error
}

type AccountInventoryService struct {
	repo      AccountInventoryRepository
	encryptor OpenAICredentialEncryptor
	activator AccountInventoryActivator
	deleter   AccountInventoryDeleter
	rootCtx   context.Context
	cancel    context.CancelFunc
	wake      chan struct{}
	done      chan struct{}
	once      sync.Once
}

func NewAccountInventoryService(repo AccountInventoryRepository, encryptor OpenAICredentialEncryptor, activator AccountInventoryActivator) *AccountInventoryService {
	ctx, cancel := context.WithCancel(context.Background())
	return &AccountInventoryService{repo: repo, encryptor: encryptor, activator: activator, rootCtx: ctx, cancel: cancel, wake: make(chan struct{}, 1), done: make(chan struct{})}
}

func (s *AccountInventoryService) SetDeleter(deleter AccountInventoryDeleter) { s.deleter = deleter }

func (s *AccountInventoryService) Import(ctx context.Context, raw string) (AccountInventoryImportResult, error) {
	rows, err := ParseOpenAIInventoryLines(raw)
	if err != nil {
		return AccountInventoryImportResult{}, err
	}
	if s == nil || s.repo == nil || s.encryptor == nil {
		return AccountInventoryImportResult{}, errors.New("库存服务不可用")
	}
	status, err := s.encryptor.EncryptionStatus()
	if err != nil || !status.Configured {
		return AccountInventoryImportResult{}, errors.New("请先启用账号凭据加密")
	}
	encrypted := make([]AccountInventoryEncryptedEntry, 0, len(rows))
	for _, row := range rows {
		password, encryptErr := s.encryptor.Encrypt(row.Password)
		if encryptErr != nil {
			return AccountInventoryImportResult{}, errors.New("库存凭据加密失败")
		}
		mfa, encryptErr := s.encryptor.Encrypt(row.MFASecret)
		if encryptErr != nil {
			return AccountInventoryImportResult{}, errors.New("库存凭据加密失败")
		}
		encrypted = append(encrypted, AccountInventoryEncryptedEntry{Email: row.Email, PasswordCiphertext: password, MFACiphertext: mfa})
	}
	inserted, err := s.repo.Import(ctx, encrypted)
	if err != nil {
		return AccountInventoryImportResult{}, errors.New("库存导入失败")
	}
	s.Wake()
	return AccountInventoryImportResult{Total: len(rows), Imported: inserted, Skipped: len(rows) - inserted}, nil
}

func (s *AccountInventoryService) Status(ctx context.Context) (AccountInventoryStatus, error) {
	if s == nil || s.repo == nil {
		return AccountInventoryStatus{}, errors.New("库存服务不可用")
	}
	return s.repo.Status(ctx)
}

func (s *AccountInventoryService) SaveConfig(ctx context.Context, target int, enabled bool) error {
	if target < 0 || target > OpenAIInventoryMaxRows {
		return fmt.Errorf("目标数量必须在 0 到 %d 之间", OpenAIInventoryMaxRows)
	}
	if err := s.repo.SaveConfig(ctx, target, enabled); err != nil {
		return errors.New("库存设置保存失败")
	}
	s.Wake()
	return nil
}

func (s *AccountInventoryService) List(ctx context.Context, page, pageSize int) ([]AccountInventoryRow, int, error) {
	if page < 1 || pageSize < 1 || pageSize > 100 {
		return nil, 0, errors.New("分页参数无效")
	}
	rows, total, err := s.repo.List(ctx, (page-1)*pageSize, pageSize)
	if err != nil {
		return nil, 0, errors.New("库存读取失败")
	}
	for i := range rows {
		rows[i].Email = maskInventoryEmail(rows[i].Email)
	}
	return rows, total, nil
}

func maskInventoryEmail(email string) string {
	local, domain, found := strings.Cut(email, "@")
	if !found || local == "" {
		return "***"
	}
	return string(local[0]) + "***@" + domain
}

func (s *AccountInventoryService) Wake() {
	if s == nil || s.wake == nil {
		return
	}
	select {
	case s.wake <- struct{}{}:
	default:
	}
}

func (s *AccountInventoryService) Start() {
	if s == nil || s.repo == nil || s.activator == nil {
		return
	}
	s.once.Do(func() {
		go func() {
			defer close(s.done)
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for {
				ctx, cancel := context.WithTimeout(s.rootCtx, 7*time.Minute)
				if err := s.RunOnce(ctx); err != nil && s.rootCtx.Err() == nil {
					slog.Warn("openai_inventory_cycle_failed", "error", err)
				}
				cancel()
				select {
				case <-s.rootCtx.Done():
					return
				case <-s.wake:
				case <-ticker.C:
				}
			}
		}()
	})
}

func (s *AccountInventoryService) Stop() {
	if s == nil {
		return
	}
	s.cancel()
	s.once.Do(func() { close(s.done) })
	select {
	case <-s.done:
	case <-time.After(10 * time.Second):
	}
}

func (s *AccountInventoryService) RunOnce(ctx context.Context) error {
	status, err := s.repo.Status(ctx)
	if err != nil || !status.Enabled || status.TargetCount == 0 {
		return err
	}
	keyStatus, err := s.encryptor.EncryptionStatus()
	if err != nil || !keyStatus.Configured {
		return errors.New("inventory credential encryption is unavailable")
	}
	failedBanUpdates := 0
	if s.deleter != nil {
		ids, listErr := s.repo.ConfirmedBannedAccountIDs(ctx)
		if listErr != nil {
			return listErr
		}
		for _, id := range ids {
			if err := s.deleter.DeleteAccount(ctx, id); err != nil {
				failedBanUpdates++
				continue
			}
			if err := s.repo.RetireAccount(ctx, id); err != nil {
				failedBanUpdates++
			}
		}
	}
	claims, err := s.repo.Claim(ctx, 4)
	if err != nil {
		return err
	}
	var wg sync.WaitGroup
	for _, claim := range claims {
		wg.Add(1)
		go func(claim AccountInventoryClaim) {
			defer wg.Done()
			s.activate(ctx, claim)
		}(claim)
	}
	wg.Wait()
	if failedBanUpdates > 0 {
		return fmt.Errorf("%d confirmed banned accounts could not be fully retired", failedBanUpdates)
	}
	return ctx.Err()
}

func (s *AccountInventoryService) activate(ctx context.Context, claim AccountInventoryClaim) {
	canImport := func(checkCtx context.Context) (bool, error) { return s.repo.CanActivate(checkCtx, claim.ID) }
	canActivate, err := canImport(ctx)
	if err != nil || !canActivate {
		s.finishClaim(ctx, claim.ID, 0, "pending", "")
		return
	}
	password, err := s.encryptor.Decrypt(claim.PasswordCiphertext)
	if err != nil {
		s.finishClaim(ctx, claim.ID, 0, "pending", "decryption_failed")
		return
	}
	mfa, err := s.encryptor.Decrypt(claim.MFACiphertext)
	if err != nil {
		s.finishClaim(ctx, claim.ID, 0, "pending", "decryption_failed")
		return
	}
	accountID, duplicate, code, err := s.activator.ActivateInventoryAccount(ctx, AccountTokenGuardReloginAccount{Email: claim.Email, Password: password, MFASecret: mfa, Force: claim.ForceLogin}, canImport)
	if err == nil {
		if duplicate {
			s.finishClaim(ctx, claim.ID, accountID, "duplicate", "")
			return
		}
		s.finishClaim(ctx, claim.ID, accountID, "active", "")
		return
	}
	if inventoryPermanentFailure(code) {
		s.finishClaim(ctx, claim.ID, 0, "failed", code)
	} else {
		s.finishClaim(ctx, claim.ID, 0, "pending", "temporary_failure")
	}
}

func inventoryPermanentFailure(code string) bool {
	switch code {
	case "account_deactivated", "account_deleted", "account_retired", "dead_account", "account_not_found", "password_rejected", "mfa_required":
		return true
	default:
		return false
	}
}

func (s *AccountInventoryService) finishClaim(ctx context.Context, id, accountID int64, state, code string) {
	persistCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer cancel()
	if err := s.repo.Complete(persistCtx, id, state, accountID, code); err != nil {
		slog.Warn("openai_inventory_claim_save_failed", "inventory_id", id, "error", err)
	}
}
