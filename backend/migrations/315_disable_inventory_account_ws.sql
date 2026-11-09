-- Inventory activation now defaults to HTTP without WS acceleration. Bring
-- existing inventory-created accounts into line without touching manual or
-- duplicate-import accounts, and refresh the scheduler's account snapshots.
WITH changed AS (
    UPDATE accounts AS account
    SET extra = COALESCE(account.extra, '{}'::jsonb) ||
        '{"openai_oauth_responses_websockets_v2_mode":"off","openai_oauth_responses_websockets_v2_enabled":false,"openai_oauth_ws_sse_acceleration":false}'::jsonb,
        updated_at = NOW()
    FROM openai_account_inventory AS inventory
    WHERE inventory.account_id = account.id
      AND inventory.state = 'active'
      AND account.deleted_at IS NULL
      AND account.platform = 'openai'
      AND account.type = 'oauth'
      AND (
          account.extra->>'openai_oauth_responses_websockets_v2_mode' IS DISTINCT FROM 'off'
          OR account.extra->'openai_oauth_responses_websockets_v2_enabled' IS DISTINCT FROM 'false'::jsonb
          OR account.extra->'openai_oauth_ws_sse_acceleration' IS DISTINCT FROM 'false'::jsonb
      )
    RETURNING account.id
)
INSERT INTO scheduler_outbox (event_type, account_id)
SELECT 'account_changed', id FROM changed;
