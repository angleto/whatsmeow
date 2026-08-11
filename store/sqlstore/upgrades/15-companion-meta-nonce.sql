-- v15 (compatible with v8+): Add companion meta nonce column to device table (multitenant)
ALTER TABLE whatsmeow_device ADD COLUMN IF NOT EXISTS companion_meta_nonce TEXT NOT NULL DEFAULT '';
