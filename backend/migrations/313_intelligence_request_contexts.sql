-- Intelligence test request bodies are kept separate from public usage data.
CREATE TABLE IF NOT EXISTS intelligence_request_contexts (
    id UUID PRIMARY KEY,
    api_key_id BIGINT NOT NULL REFERENCES api_keys(id) ON DELETE CASCADE,
    user_id BIGINT NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    usage_request_id VARCHAR(256),
    matched_rules JSONB NOT NULL,
    request_body BYTEA NOT NULL,
    content_type TEXT NOT NULL DEFAULT '',
    content_encoding TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX IF NOT EXISTS intelligence_request_contexts_usage_idx
    ON intelligence_request_contexts (api_key_id, usage_request_id);
CREATE INDEX IF NOT EXISTS intelligence_request_contexts_created_idx
    ON intelligence_request_contexts (created_at DESC, id DESC);
