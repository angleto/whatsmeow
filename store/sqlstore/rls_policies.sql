-- Row-Level Security (RLS) Policies for Defense-in-Depth Multitenancy
--
-- This file contains PostgreSQL Row-Level Security policies that provide
-- an additional layer of protection against cross-tenant data access.
--
-- IMPORTANT: These policies are OPTIONAL but RECOMMENDED for production deployments.
-- They require PostgreSQL and the application to set the app.current_business_id
-- session variable on every connection it uses.
--
-- The policies below use current_setting('app.current_business_id', true), which
-- returns NULL when the variable was never set. Combined with FORCE ROW LEVEL
-- SECURITY at the end of this file, that means an application which does NOT set the
-- variable sees ZERO rows in every table. Do not apply this file until the
-- application side is wired up.
--
-- Usage:
-- 1. Wire up the session variable in the application. With this library, call
--    sqlstore.EnableTenantRLS(cfg) on the *pgxpool.Config before creating the pool:
--
--        cfg, _ := pgxpool.ParseConfig(dsn)
--        sqlstore.EnableTenantRLS(cfg)
--        pool, _ := pgxpool.NewWithConfig(ctx, cfg)
--
--    Every query issued through a Container/SQLStore then sets
--    app.current_business_id to that container's business ID on the connection it
--    acquires, so one shared pool can serve many tenants safely.
-- 2. Apply this file to your database. It is idempotent: every policy is dropped
--    before being recreated, so it can be re-run after a schema upgrade.
-- 3. The database now enforces tenant isolation at the row level, on top of the
--    business_id predicate that every query already carries.
--
-- CONSTRAINTS
-- * Do NOT apply this behind a transaction-pooling proxy. PgBouncer with
--   pool_mode = transaction (and equivalents) re-multiplexes each statement onto a
--   different server backend, so app.current_business_id is usually absent when the
--   query runs and every read silently returns zero rows with no error. Session
--   pooling is fine.
-- * Run schema migrations before applying this file, or from a role with BYPASSRLS.
--   FORCE ROW LEVEL SECURITY below applies to the table owner too, so a data migration
--   run with the policies active would only see one tenant's rows.
--
-- Covers all 16 tenant tables of schema v16. The schema-version table
-- whatsmeow_version is intentionally NOT covered: it holds no tenant data and the
-- migration runner must reach it before any tenant is known.
--
-- Note: redacted phone numbers live in whatsmeow_contacts.redacted_phone (added by
-- migration 11) and are covered by the whatsmeow_contacts policies. There has never
-- been a whatsmeow_redacted_phones table.

-- Enable RLS on all tenant tables

ALTER TABLE whatsmeow_device ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_identity_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_pre_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_sessions ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_sender_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_app_state_sync_keys ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_app_state_version ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_app_state_mutation_macs ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_contacts ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_chat_settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_message_secrets ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_privacy_tokens ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_lid_map ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_event_buffer ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_retry_buffer ENABLE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_nct_salt ENABLE ROW LEVEL SECURITY;

-- Tenant isolation policies. The USING clause covers SELECT/UPDATE/DELETE (and is
-- reused as the check for UPDATE); the separate INSERT policy covers WITH CHECK.

DROP POLICY IF EXISTS tenant_isolation_device ON whatsmeow_device;
CREATE POLICY tenant_isolation_device ON whatsmeow_device
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_device_insert ON whatsmeow_device;
CREATE POLICY tenant_isolation_device_insert ON whatsmeow_device
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_identity_keys ON whatsmeow_identity_keys;
CREATE POLICY tenant_isolation_identity_keys ON whatsmeow_identity_keys
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_identity_keys_insert ON whatsmeow_identity_keys;
CREATE POLICY tenant_isolation_identity_keys_insert ON whatsmeow_identity_keys
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_pre_keys ON whatsmeow_pre_keys;
CREATE POLICY tenant_isolation_pre_keys ON whatsmeow_pre_keys
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_pre_keys_insert ON whatsmeow_pre_keys;
CREATE POLICY tenant_isolation_pre_keys_insert ON whatsmeow_pre_keys
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_sessions ON whatsmeow_sessions;
CREATE POLICY tenant_isolation_sessions ON whatsmeow_sessions
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_sessions_insert ON whatsmeow_sessions;
CREATE POLICY tenant_isolation_sessions_insert ON whatsmeow_sessions
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_sender_keys ON whatsmeow_sender_keys;
CREATE POLICY tenant_isolation_sender_keys ON whatsmeow_sender_keys
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_sender_keys_insert ON whatsmeow_sender_keys;
CREATE POLICY tenant_isolation_sender_keys_insert ON whatsmeow_sender_keys
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_app_state_sync_keys ON whatsmeow_app_state_sync_keys;
CREATE POLICY tenant_isolation_app_state_sync_keys ON whatsmeow_app_state_sync_keys
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_app_state_sync_keys_insert ON whatsmeow_app_state_sync_keys;
CREATE POLICY tenant_isolation_app_state_sync_keys_insert ON whatsmeow_app_state_sync_keys
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_app_state_version ON whatsmeow_app_state_version;
CREATE POLICY tenant_isolation_app_state_version ON whatsmeow_app_state_version
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_app_state_version_insert ON whatsmeow_app_state_version;
CREATE POLICY tenant_isolation_app_state_version_insert ON whatsmeow_app_state_version
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_app_state_mutation_macs ON whatsmeow_app_state_mutation_macs;
CREATE POLICY tenant_isolation_app_state_mutation_macs ON whatsmeow_app_state_mutation_macs
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_app_state_mutation_macs_insert ON whatsmeow_app_state_mutation_macs;
CREATE POLICY tenant_isolation_app_state_mutation_macs_insert ON whatsmeow_app_state_mutation_macs
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_contacts ON whatsmeow_contacts;
CREATE POLICY tenant_isolation_contacts ON whatsmeow_contacts
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_contacts_insert ON whatsmeow_contacts;
CREATE POLICY tenant_isolation_contacts_insert ON whatsmeow_contacts
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_chat_settings ON whatsmeow_chat_settings;
CREATE POLICY tenant_isolation_chat_settings ON whatsmeow_chat_settings
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_chat_settings_insert ON whatsmeow_chat_settings;
CREATE POLICY tenant_isolation_chat_settings_insert ON whatsmeow_chat_settings
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_message_secrets ON whatsmeow_message_secrets;
CREATE POLICY tenant_isolation_message_secrets ON whatsmeow_message_secrets
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_message_secrets_insert ON whatsmeow_message_secrets;
CREATE POLICY tenant_isolation_message_secrets_insert ON whatsmeow_message_secrets
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_privacy_tokens ON whatsmeow_privacy_tokens;
CREATE POLICY tenant_isolation_privacy_tokens ON whatsmeow_privacy_tokens
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_privacy_tokens_insert ON whatsmeow_privacy_tokens;
CREATE POLICY tenant_isolation_privacy_tokens_insert ON whatsmeow_privacy_tokens
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_lid_map ON whatsmeow_lid_map;
CREATE POLICY tenant_isolation_lid_map ON whatsmeow_lid_map
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_lid_map_insert ON whatsmeow_lid_map;
CREATE POLICY tenant_isolation_lid_map_insert ON whatsmeow_lid_map
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_event_buffer ON whatsmeow_event_buffer;
CREATE POLICY tenant_isolation_event_buffer ON whatsmeow_event_buffer
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_event_buffer_insert ON whatsmeow_event_buffer;
CREATE POLICY tenant_isolation_event_buffer_insert ON whatsmeow_event_buffer
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_retry_buffer ON whatsmeow_retry_buffer;
CREATE POLICY tenant_isolation_retry_buffer ON whatsmeow_retry_buffer
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_retry_buffer_insert ON whatsmeow_retry_buffer;
CREATE POLICY tenant_isolation_retry_buffer_insert ON whatsmeow_retry_buffer
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_nct_salt ON whatsmeow_nct_salt;
CREATE POLICY tenant_isolation_nct_salt ON whatsmeow_nct_salt
    USING (business_id = current_setting('app.current_business_id', true));

DROP POLICY IF EXISTS tenant_isolation_nct_salt_insert ON whatsmeow_nct_salt;
CREATE POLICY tenant_isolation_nct_salt_insert ON whatsmeow_nct_salt
    FOR INSERT
    WITH CHECK (business_id = current_setting('app.current_business_id', true));

-- FORCE applies the policies to the table owner too, which is normally the role the
-- application connects as. Without it, an owner connection bypasses RLS entirely and
-- this file buys you nothing. Use a separate maintenance role for operations that
-- must see every tenant.
ALTER TABLE whatsmeow_device FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_identity_keys FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_pre_keys FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_sessions FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_sender_keys FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_app_state_sync_keys FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_app_state_version FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_app_state_mutation_macs FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_contacts FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_chat_settings FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_message_secrets FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_privacy_tokens FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_lid_map FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_event_buffer FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_retry_buffer FORCE ROW LEVEL SECURITY;
ALTER TABLE whatsmeow_nct_salt FORCE ROW LEVEL SECURITY;

-- To roll back, disable RLS on every table (the policies then become inert):
-- ALTER TABLE whatsmeow_device DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_identity_keys DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_pre_keys DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_sessions DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_sender_keys DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_app_state_sync_keys DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_app_state_version DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_app_state_mutation_macs DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_contacts DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_chat_settings DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_message_secrets DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_privacy_tokens DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_lid_map DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_event_buffer DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_retry_buffer DISABLE ROW LEVEL SECURITY;
-- ALTER TABLE whatsmeow_nct_salt DISABLE ROW LEVEL SECURITY;
--
-- To also drop the policies, run DROP POLICY IF EXISTS for both the
-- tenant_isolation_<table> and tenant_isolation_<table>_insert names above.
