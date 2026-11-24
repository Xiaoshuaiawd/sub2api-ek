package service

import (
	"encoding/json"
	"strings"

	infraerrors "github.com/Wei-Shaw/sub2api/internal/pkg/errors"
)

// The Graph client and mailbox refresh token are one encrypted credential.
// They are never included in administrator configuration/status responses.
type openAIMailboxGraphCredentials struct {
	ClientID     string `json:"mail_client_id"`
	RefreshToken string `json:"mail_refresh_token"`
}

func (s *OpenAIOAuthReauthService) saveMailboxGraphConfig(stored *OpenAIOAuthReauthStoredConfig, input OpenAIOAuthReauthConfigInput) error {
	stored.OTPURLCiphertext = ""
	clientID, refreshToken := strings.TrimSpace(input.MailClientID), strings.TrimSpace(input.MailRefreshToken)
	if clientID != "" || refreshToken != "" {
		if clientID == "" || refreshToken == "" || len(clientID) > 4096 || len(refreshToken) > 16384 {
			return infraerrors.BadRequest("OPENAI_REAUTH_MAILBOX_REQUIRED", "a mailbox client id and refresh token are required")
		}
		bundle, err := json.Marshal(openAIMailboxGraphCredentials{ClientID: clientID, RefreshToken: refreshToken})
		if err != nil {
			return infraerrors.InternalServer("OPENAI_REAUTH_CONFIG_ENCRYPT_FAILED", "failed to prepare mailbox credentials")
		}
		stored.MailboxCredentialsCiphertext, err = s.encryptor.Encrypt(string(bundle))
		if err != nil {
			return infraerrors.InternalServer("OPENAI_REAUTH_CONFIG_ENCRYPT_FAILED", "failed to encrypt mailbox credentials")
		}
	}
	if stored.MailboxCredentialsCiphertext == "" {
		return infraerrors.BadRequest("OPENAI_REAUTH_MAILBOX_REQUIRED", "a mailbox client id and refresh token are required")
	}
	// Six-field imports may also supply a password/TOTP fallback. Preserve it
	// in the existing encrypted columns, without requiring it for mailbox OTP.
	var err error
	if input.Password != "" {
		stored.PasswordCiphertext, err = s.encryptor.Encrypt(input.Password)
		if err != nil {
			return infraerrors.InternalServer("OPENAI_REAUTH_CONFIG_ENCRYPT_FAILED", "failed to encrypt login password")
		}
	}
	if secret := strings.TrimSpace(input.TOTPSecret); secret != "" {
		stored.TOTPSecretCiphertext, err = s.encryptor.Encrypt(secret)
		if err != nil {
			return infraerrors.InternalServer("OPENAI_REAUTH_CONFIG_ENCRYPT_FAILED", "failed to encrypt TOTP secret")
		}
	}
	return nil
}

func (s *OpenAIOAuthReauthService) decryptMailboxGraph(stored *OpenAIOAuthReauthStoredConfig) (openAIMailboxGraphCredentials, error) {
	var bundle openAIMailboxGraphCredentials
	plain, err := s.decryptReauthSecret(stored.MailboxCredentialsCiphertext, "saved mailbox credentials cannot be decrypted")
	if err != nil {
		return bundle, err
	}
	if json.Unmarshal([]byte(plain), &bundle) != nil || strings.TrimSpace(bundle.ClientID) == "" || strings.TrimSpace(bundle.RefreshToken) == "" {
		return bundle, infraerrors.BadRequest("OPENAI_REAUTH_MAILBOX_REQUIRED", "complete mailbox credentials are required")
	}
	return bundle, nil
}

func (s *OpenAIOAuthReauthService) mailboxGraphClaim(stored *OpenAIOAuthReauthStoredConfig, claim *OpenAIOAuthReauthClaim) error {
	bundle, err := s.decryptMailboxGraph(stored)
	if err != nil {
		return err
	}
	claim.MailClientID, claim.MailRefreshToken = bundle.ClientID, bundle.RefreshToken
	claim.Password, err = s.decryptReauthSecret(stored.PasswordCiphertext, "login password cannot be decrypted")
	if err != nil {
		return err
	}
	if stored.TOTPSecretCiphertext != "" {
		binding, err := s.decryptTOTP(stored.TOTPSecretCiphertext)
		if err != nil {
			return err
		}
		claim.TOTPSecret, claim.TOTPFactorID = binding.Secret, binding.FactorID
	}
	return nil
}

func (s *OpenAIOAuthReauthService) mailboxGraphConfigView(stored *OpenAIOAuthReauthStoredConfig, view *OpenAIOAuthReauthConfig) error {
	claim := &OpenAIOAuthReauthClaim{}
	if err := s.mailboxGraphClaim(stored, claim); err != nil {
		return err
	}
	view.MailboxConfigured = true
	view.PasswordConfigured = claim.Password != ""
	view.TOTPConfigured = claim.TOTPSecret != ""
	view.Configured = true
	return nil
}
