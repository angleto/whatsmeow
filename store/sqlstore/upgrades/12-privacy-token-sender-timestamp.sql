-- v12 (compatible with v8+): Add sender timestamp and prune index for privacy tokens (multitenant)
--
-- This number was once shared with a second file, so databases stamped v12 skipped
-- whichever of the two the runner reached second. On branch v2.4 the fix was to
-- renumber this file to 15; here the fix is whatsmeow_migrations, which records
-- applied migrations by filename, so the number only controls ordering and a database
-- that missed this file gets it on the next start regardless of its recorded version.
ALTER TABLE whatsmeow_privacy_tokens ADD COLUMN IF NOT EXISTS sender_timestamp BIGINT;

CREATE INDEX IF NOT EXISTS idx_whatsmeow_privacy_tokens_our_jid_timestamp
ON whatsmeow_privacy_tokens (business_id, our_jid, timestamp);
