-- v12 (compatible with v8+): Add sender timestamp and prune index for privacy tokens (multitenant)
ALTER TABLE whatsmeow_privacy_tokens ADD COLUMN IF NOT EXISTS sender_timestamp BIGINT;

CREATE INDEX IF NOT EXISTS idx_whatsmeow_privacy_tokens_our_jid_timestamp
ON whatsmeow_privacy_tokens (business_id, our_jid, timestamp);
