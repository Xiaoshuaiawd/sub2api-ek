package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
)

type LegacyReloginAccountReader interface {
	ListAllWithFilters(context.Context, string, string, string, string, int64, string) ([]Account, error)
}

type legacyReloginConfigImporter interface {
	ImportLegacyReloginConfig(context.Context, *OpenAIOAuthReauthStoredConfig, bool, bool) (bool, error)
}

func (s *AccountTokenGuardV2Service) SetLegacyReloginAccounts(accounts LegacyReloginAccountReader) {
	s.legacyAccounts = accounts
}

// MigrateLegacyReloginAccounts runs within the existing V2 cycle. The source
// settings are deliberately read-only: a concurrent endpoint/header edit must
// never be replaced by a stale migration snapshot.
func (s *AccountTokenGuardV2Service) MigrateLegacyReloginAccounts(ctx context.Context) (int, error) {
	if s == nil || s.settings == nil || s.legacyAccounts == nil || s.reauth == nil || s.repo == nil {
		return 0, nil
	}
	s.legacyMigrationMu.Lock()
	defer s.legacyMigrationMu.Unlock()
	if s.legacyMigrationDone {
		return 0, nil
	}
	raw, err := s.settings.GetValue(ctx, legacyTokenGuardSettingsKey)
	if errors.Is(err, ErrSettingNotFound) || (err == nil && strings.TrimSpace(raw) == "") {
		s.legacyMigrationDone = true
		return 0, nil
	}
	if err != nil {
		return 0, errors.New("legacy re-login settings are unavailable")
	}
	legacy := struct {
		Enabled         bool                       `json:"enabled"`
		AutoRelogin     bool                       `json:"auto_relogin"`
		GroupIDs        []int64                    `json:"group_ids"`
		ReloginAccounts []OpenAIReloginCredentials `json:"relogin_accounts"`
	}{AutoRelogin: true}
	if json.Unmarshal([]byte(raw), &legacy) != nil {
		return 0, errors.New("legacy re-login settings are invalid")
	}
	if len(legacy.ReloginAccounts) == 0 {
		s.legacyMigrationDone = true
		return 0, nil
	}
	if err := s.reauth.ensureDurableEncryption(); err != nil {
		return 0, err
	}
	importer, ok := s.reauth.repo.(legacyReloginConfigImporter)
	if !ok {
		return 0, errors.New("legacy re-login migration storage is unavailable")
	}
	migrated, pending := 0, false
	var firstErr error
	for _, entry := range legacy.ReloginAccounts {
		if err := ctx.Err(); err != nil {
			return migrated, err
		}
		email, err := normalizeReauthEmail(entry.Email)
		if err != nil {
			pending = true
			firstErr = errors.New("some legacy re-login credentials are invalid")
			continue
		}
		accounts, err := s.legacyAccounts.ListAllWithFilters(ctx, PlatformOpenAI, AccountTypeOAuth, "", email, 0, "")
		if err != nil {
			pending = true
			firstErr = errors.New("legacy re-login accounts could not be read")
			continue
		}
		matched := false
		for _, account := range accounts {
			// The old guard matched its credential entries by exact account name.
			if !strings.EqualFold(strings.TrimSpace(account.Name), email) || account.Platform != PlatformOpenAI ||
				account.Type != AccountTypeOAuth || account.IsShadow() || account.IsOpenAIPersonalAccessToken() || account.IsOpenAIAgentIdentity() {
				continue
			}
			matched = true
			guard, guardErr := s.repo.GetAccount(ctx, account.ID)
			config, configErr := s.reauth.repo.GetConfig(ctx, account.ID)
			if guardErr != nil || configErr != nil {
				pending = true
				firstErr = errors.New("existing re-login configuration could not be read")
				continue
			}
			if guard != nil || config != nil {
				continue
			}
			mode := OpenAIOAuthReauthModePasswordTOTP
			if strings.TrimSpace(entry.MailClientID) != "" && strings.TrimSpace(entry.MailRefreshToken) != "" {
				mode = OpenAIOAuthReauthModeMailboxGraph
			}
			stored, prepareErr := s.reauth.prepareCredentialConfig(ctx, account.ID, OpenAIOAuthReauthConfigInput{
				LoginEmail: email, CredentialMode: mode, Engine: OpenAIOAuthReauthEngineSessionStudio,
				ProxySource: OpenAIOAuthReauthProxySourceAccount, Password: entry.Password, TOTPSecret: entry.MFASecret,
				MailClientID: entry.MailClientID, MailRefreshToken: entry.MailRefreshToken,
			})
			if prepareErr != nil {
				pending = true
				firstErr = errors.New("legacy re-login credentials could not be encrypted")
				continue
			}
			enabled := legacy.Enabled && (account.Status == StatusActive || account.Status == StatusError) && legacyReloginGroupAllowed(account, legacy.GroupIDs)
			inserted, importErr := importer.ImportLegacyReloginConfig(ctx, stored, enabled, legacy.AutoRelogin)
			if importErr != nil {
				pending = true
				firstErr = errors.New("legacy re-login credentials could not be saved")
				continue
			}
			if inserted {
				migrated++
			}
		}
		// Keep unmatched entries eligible for a later cycle after an account is
		// restored/imported, instead of silently abandoning its old login method.
		if !matched {
			pending = true
		}
	}
	s.legacyMigrationDone = !pending
	return migrated, firstErr
}

func legacyReloginGroupAllowed(account Account, groups []int64) bool {
	validGroups := 0
	for _, wanted := range groups {
		if wanted <= 0 {
			continue
		}
		validGroups++
		for _, id := range account.GroupIDs {
			if id == wanted {
				return true
			}
		}
		for _, group := range account.Groups {
			if group != nil && group.ID == wanted {
				return true
			}
		}
	}
	return validGroups == 0
}
