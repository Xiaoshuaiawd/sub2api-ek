-- Credentials are encrypted by the application before insertion. No plaintext
-- password or MFA secret is stored in this table.
CREATE TABLE IF NOT EXISTS openai_account_inventory_config (
    singleton BOOLEAN PRIMARY KEY DEFAULT TRUE CHECK (singleton),
    target_count INTEGER NOT NULL DEFAULT 0 CHECK (target_count BETWEEN 0 AND 10000),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
INSERT INTO openai_account_inventory_config (singleton) VALUES (TRUE)
ON CONFLICT (singleton) DO NOTHING;

CREATE TABLE IF NOT EXISTS openai_account_inventory (
    id BIGSERIAL PRIMARY KEY,
    email TEXT NOT NULL UNIQUE CHECK (email = LOWER(email)),
    password_ciphertext TEXT NOT NULL,
    mfa_ciphertext TEXT NOT NULL,
    state TEXT NOT NULL DEFAULT 'pending' CHECK (state IN ('pending', 'activating', 'active', 'failed', 'retired', 'duplicate')),
    account_id BIGINT REFERENCES accounts(id) ON DELETE SET NULL,
    lease_until TIMESTAMPTZ,
    next_attempt_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    error_code TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS idx_openai_account_inventory_state
    ON openai_account_inventory (state, id);
CREATE INDEX IF NOT EXISTS idx_openai_account_inventory_account
    ON openai_account_inventory (account_id) WHERE account_id IS NOT NULL;
