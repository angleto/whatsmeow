-- v14 (compatible with v8+): Add NCT salt table for cstoken derivation (multitenant)
CREATE TABLE IF NOT EXISTS whatsmeow_nct_salt (
	business_id TEXT  NOT NULL,
	our_jid     TEXT  NOT NULL,
	salt        bytea NOT NULL,
	PRIMARY KEY (business_id, our_jid),
	FOREIGN KEY (business_id, our_jid) REFERENCES whatsmeow_device(business_id, jid) ON DELETE CASCADE ON UPDATE CASCADE
);
