-- v0 -> v14 (compatible with v8+): Latest schema for multitenant setup
CREATE TABLE IF NOT EXISTS whatsmeow_device (
  	business_id TEXT NOT NULL,
	jid TEXT NOT NULL,
	lid TEXT,

	facebook_uuid uuid,

	registration_id BIGINT NOT NULL CHECK ( registration_id >= 0 AND registration_id < 4294967296 ),

	noise_key    bytea NOT NULL CHECK ( length(noise_key) = 32 ),
	identity_key bytea NOT NULL CHECK ( length(identity_key) = 32 ),

	signed_pre_key     bytea   NOT NULL CHECK ( length(signed_pre_key) = 32 ),
	signed_pre_key_id  INTEGER NOT NULL CHECK ( signed_pre_key_id >= 0 AND signed_pre_key_id < 16777216 ),
	signed_pre_key_sig bytea   NOT NULL CHECK ( length(signed_pre_key_sig) = 64 ),

	adv_key             bytea NOT NULL,
	adv_details         bytea NOT NULL,
	adv_account_sig     bytea NOT NULL CHECK ( length(adv_account_sig) = 64 ),
	adv_account_sig_key bytea NOT NULL CHECK ( length(adv_account_sig_key) = 32 ),
	adv_device_sig      bytea NOT NULL CHECK ( length(adv_device_sig) = 64 ),

	platform      TEXT NOT NULL DEFAULT '',
	business_name TEXT NOT NULL DEFAULT '',
	push_name     TEXT NOT NULL DEFAULT '',

	lid_migration_ts BIGINT NOT NULL DEFAULT 0,

	PRIMARY KEY (business_id, jid)
);

CREATE TABLE IF NOT EXISTS whatsmeow_identity_keys (
    business_id TEXT NOT NULL,
	our_jid  TEXT,
	their_id TEXT,
	identity bytea NOT NULL CHECK ( length(identity) = 32 ),

	PRIMARY KEY (business_id, our_jid, their_id),
	FOREIGN KEY (business_id, our_jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_pre_keys (
	business_id TEXT NOT NULL,
	jid      TEXT,
	key_id   INTEGER          CHECK ( key_id >= 0 AND key_id < 16777216 ),
	key      bytea   NOT NULL CHECK ( length(key) = 32 ),
	uploaded BOOLEAN NOT NULL,

	PRIMARY KEY (business_id, jid, key_id),
	FOREIGN KEY (business_id, jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_sessions (
	business_id TEXT NOT NULL,
	our_jid  TEXT,
	their_id TEXT,
	session  bytea,

	PRIMARY KEY (business_id, our_jid, their_id),
	FOREIGN KEY (business_id, our_jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_sender_keys (
    business_id TEXT NOT NULL,
	our_jid    TEXT,
	chat_id    TEXT,
	sender_id  TEXT,
	sender_key bytea NOT NULL,

	PRIMARY KEY (business_id, our_jid, chat_id, sender_id),
	FOREIGN KEY (business_id, our_jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_app_state_sync_keys (
	business_id TEXT NOT NULL,
	jid         TEXT,
	key_id      bytea,
	key_data    bytea  NOT NULL,
	timestamp   BIGINT NOT NULL,
	fingerprint bytea  NOT NULL,

	PRIMARY KEY (business_id, jid, key_id),
	FOREIGN KEY (business_id, jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_app_state_version (
	business_id TEXT NOT NULL,
	jid     TEXT,
	name    TEXT,
	version BIGINT NOT NULL,
	hash    bytea  NOT NULL CHECK ( length(hash) = 128 ),

	PRIMARY KEY (business_id, jid, name),
	FOREIGN KEY (business_id, jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_app_state_mutation_macs (
	business_id TEXT NOT NULL,
	jid       TEXT,
	name      TEXT,
	version   BIGINT,
	index_mac bytea          CHECK ( length(index_mac) = 32 ),
	value_mac bytea NOT NULL CHECK ( length(value_mac) = 32 ),

	PRIMARY KEY (business_id, jid, name, version, index_mac),
	FOREIGN KEY (business_id, jid, name) REFERENCES whatsmeow_app_state_version(business_id, jid, name) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_contacts (
	business_id TEXT NOT NULL,
	our_jid        TEXT,
	their_jid      TEXT,
	first_name     TEXT,
	full_name      TEXT,
	push_name      TEXT,
	business_name  TEXT,
	redacted_phone TEXT,

	PRIMARY KEY (business_id, our_jid, their_jid),
	FOREIGN KEY (business_id, our_jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_chat_settings (
	business_id TEXT NOT NULL,
	our_jid       TEXT,
	chat_jid      TEXT,
	muted_until   BIGINT  NOT NULL DEFAULT 0,
	pinned        BOOLEAN NOT NULL DEFAULT false,
	archived      BOOLEAN NOT NULL DEFAULT false,

	PRIMARY KEY (business_id, our_jid, chat_jid),
	FOREIGN KEY (business_id, our_jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_message_secrets (
	business_id TEXT NOT NULL,
	our_jid    TEXT,
	chat_jid   TEXT,
	sender_jid TEXT,
	message_id TEXT,
	key        bytea NOT NULL,

	PRIMARY KEY (business_id, our_jid, chat_jid, sender_jid, message_id),
	FOREIGN KEY (business_id, our_jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_privacy_tokens (
	business_id      TEXT NOT NULL,
	our_jid          TEXT,
	their_jid        TEXT,
	token            bytea  NOT NULL,
	timestamp        BIGINT NOT NULL,
	sender_timestamp BIGINT,
	PRIMARY KEY (business_id, our_jid, their_jid),
	FOREIGN KEY (business_id, our_jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_whatsmeow_privacy_tokens_our_jid_timestamp
ON whatsmeow_privacy_tokens (business_id, our_jid, timestamp);

CREATE TABLE IF NOT EXISTS whatsmeow_lid_map (
	business_id TEXT NOT NULL,
	lid TEXT NOT NULL,
	pn  TEXT NOT NULL,
	PRIMARY KEY (business_id, lid),
	UNIQUE (business_id, pn)
);

CREATE TABLE IF NOT EXISTS whatsmeow_event_buffer (
	business_id      TEXT NOT NULL,
	our_jid          TEXT   NOT NULL,
	ciphertext_hash  bytea  NOT NULL CHECK ( length(ciphertext_hash) = 32 ),
	plaintext        bytea,
	server_timestamp BIGINT NOT NULL,
	insert_timestamp BIGINT NOT NULL,
	PRIMARY KEY (business_id, our_jid, ciphertext_hash),
	FOREIGN KEY (business_id, our_jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE TABLE IF NOT EXISTS whatsmeow_retry_buffer (
	business_id TEXT NOT NULL,
	our_jid     TEXT   NOT NULL,
	chat_jid    TEXT   NOT NULL,
	message_id  TEXT   NOT NULL,
	format      TEXT   NOT NULL,
	plaintext   bytea  NOT NULL,
	timestamp   BIGINT NOT NULL,

	PRIMARY KEY (business_id, our_jid, chat_jid, message_id),
	FOREIGN KEY (business_id, our_jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);

CREATE INDEX IF NOT EXISTS whatsmeow_retry_buffer_timestamp_idx ON whatsmeow_retry_buffer (business_id, our_jid, timestamp);

-- Performance indexes for multitenancy
CREATE INDEX IF NOT EXISTS idx_identity_keys_business ON whatsmeow_identity_keys(business_id);
CREATE INDEX IF NOT EXISTS idx_sessions_business ON whatsmeow_sessions(business_id);
CREATE INDEX IF NOT EXISTS idx_pre_keys_business ON whatsmeow_pre_keys(business_id);
CREATE INDEX IF NOT EXISTS idx_sender_keys_business ON whatsmeow_sender_keys(business_id);
CREATE INDEX IF NOT EXISTS idx_app_state_sync_keys_business ON whatsmeow_app_state_sync_keys(business_id);
CREATE INDEX IF NOT EXISTS idx_app_state_version_business ON whatsmeow_app_state_version(business_id);
CREATE INDEX IF NOT EXISTS idx_app_state_mutation_macs_business ON whatsmeow_app_state_mutation_macs(business_id);
CREATE INDEX IF NOT EXISTS idx_contacts_business ON whatsmeow_contacts(business_id);
CREATE INDEX IF NOT EXISTS idx_chat_settings_business ON whatsmeow_chat_settings(business_id);
CREATE INDEX IF NOT EXISTS idx_message_secrets_business ON whatsmeow_message_secrets(business_id);
CREATE INDEX IF NOT EXISTS idx_privacy_tokens_business ON whatsmeow_privacy_tokens(business_id);
CREATE INDEX IF NOT EXISTS idx_lid_map_business ON whatsmeow_lid_map(business_id);
CREATE INDEX IF NOT EXISTS idx_event_buffer_business ON whatsmeow_event_buffer(business_id);
CREATE INDEX IF NOT EXISTS idx_retry_buffer_business ON whatsmeow_retry_buffer(business_id);

-- Composite indexes for common query patterns
CREATE INDEX IF NOT EXISTS idx_sessions_business_jid ON whatsmeow_sessions(business_id, our_jid);
CREATE INDEX IF NOT EXISTS idx_contacts_business_jid ON whatsmeow_contacts(business_id, our_jid);
CREATE INDEX IF NOT EXISTS idx_identity_keys_business_jid ON whatsmeow_identity_keys(business_id, our_jid);
