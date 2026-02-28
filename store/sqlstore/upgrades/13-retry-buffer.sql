-- v13 (compatible with v8+): Add buffer for outgoing events to accept retry receipts (multitenant)
CREATE TABLE IF NOT EXISTS whatsmeow_retry_buffer (
	business_id TEXT   NOT NULL,
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
CREATE INDEX IF NOT EXISTS idx_retry_buffer_business ON whatsmeow_retry_buffer(business_id);
