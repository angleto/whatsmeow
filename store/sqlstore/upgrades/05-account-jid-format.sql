-- v5: Update account JID format
--
-- The WHERE clause makes this safely re-runnable: on a database whose JIDs are already
-- converted it matches no rows, so the reconciliation path in upgrade.go can replay it
-- without rewriting every row of whatsmeow_device (and cascading that update through
-- every child table's foreign key).
UPDATE whatsmeow_device SET jid=REPLACE(jid, '.0', '') WHERE jid LIKE '%.0%';
