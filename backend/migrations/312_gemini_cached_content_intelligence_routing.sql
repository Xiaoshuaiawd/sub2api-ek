ALTER TABLE gemini_cached_contents
    ADD COLUMN IF NOT EXISTS intelligence_routing BOOLEAN NOT NULL DEFAULT FALSE;
