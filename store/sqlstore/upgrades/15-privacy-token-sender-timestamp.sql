-- v15 (compatible with v8+): Add sender timestamp and prune index for privacy tokens (multitenant)
-- NOTE: renumbered from a duplicate "12"; DBs already at v12 skipped it, so this must be > 14.
ALTER TABLE whatsmeow_privacy_tokens ADD COLUMN IF NOT EXISTS sender_timestamp BIGINT;

CREATE INDEX IF NOT EXISTS idx_whatsmeow_privacy_tokens_our_jid_timestamp
ON whatsmeow_privacy_tokens (business_id, our_jid, timestamp);
