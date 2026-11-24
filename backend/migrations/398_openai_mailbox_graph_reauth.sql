-- Keep legacy Graph mailbox OTP logins in the encrypted, durable V2 queue.
ALTER TABLE openai_oauth_reauth_configs
    ADD COLUMN IF NOT EXISTS mailbox_credentials_ciphertext TEXT;

ALTER TABLE openai_oauth_reauth_configs
    DROP CONSTRAINT IF EXISTS openai_oauth_reauth_configs_credential_mode_check;
ALTER TABLE openai_oauth_reauth_configs
    ADD CONSTRAINT openai_oauth_reauth_configs_credential_mode_check CHECK (
        credential_mode IN ('email_otp_url', 'password_totp', 'mailbox_graph')
    );
