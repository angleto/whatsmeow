-- v4: Add privacy tokens table
CREATE TABLE IF NOT EXISTS whatsmeow_privacy_tokens (
	business_id TEXT NOT NULL,
	our_jid   TEXT,
	their_jid TEXT,
	token     bytea  NOT NULL,
	timestamp BIGINT NOT NULL,
	PRIMARY KEY (business_id, our_jid, their_jid),
	FOREIGN KEY (business_id, our_jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);
