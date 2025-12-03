-- v8 (compatible with v8+): Add tables for LID<->JID mapping
CREATE TABLE IF NOT EXISTS whatsmeow_lid_map (
	business_id TEXT NOT NULL,
	lid TEXT NOT NULL,
	pn  TEXT NOT NULL,
	PRIMARY KEY (business_id, lid),
	UNIQUE (business_id, pn)
);
