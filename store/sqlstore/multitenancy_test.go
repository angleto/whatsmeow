// Copyright (c) 2025 Security Testing
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package sqlstore_test

import (
	"bytes"
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"go.mau.fi/whatsmeow/proto/waAdv"
	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/store/sqlstore"
	"go.mau.fi/whatsmeow/types"
	waLog "go.mau.fi/whatsmeow/util/log"
)

// newTestDevice returns a device that can actually be persisted. PutDevice writes the
// ADV account columns unconditionally and the schema CHECKs their lengths, so a device
// straight out of NewDevice (which has no Account yet, as before pairing) cannot be
// stored.
func newTestDevice(container *sqlstore.Container, jid types.JID) *store.Device {
	device := container.NewDevice()
	device.ID = &jid
	device.Account = &waAdv.ADVSignedDeviceIdentity{
		Details:             []byte("test-adv-details"),
		AccountSignatureKey: bytes.Repeat([]byte{0x01}, 32),
		AccountSignature:    bytes.Repeat([]byte{0x02}, 64),
		DeviceSignature:     bytes.Repeat([]byte{0x03}, 64),
	}
	return device
}

// TestCrossTenantIsolation tests that data from different tenants is properly isolated
func TestCrossTenantIsolation(t *testing.T) {
	// Skip this test if no PostgreSQL connection is available
	dbURL := getTestDatabaseURL()
	if dbURL == "" {
		t.Skip("Skipping cross-tenant isolation test: no database URL provided (set TEST_DB_URL)")
	}

	ctx := context.Background()

	// Create database connection
	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbPool.Close()

	applySchema(t, dbPool)
	clearTenants(t, dbPool, "tenant1", "tenant2")

	// Create two containers with different business IDs
	log := waLog.Noop
	container1 := sqlstore.NewContainer(dbPool, "tenant1", log)
	container2 := sqlstore.NewContainer(dbPool, "tenant2", log)

	// Create a device for tenant1
	jid1, _ := types.ParseJID("user1@whatsapp.net")
	device1 := newTestDevice(container1, jid1)
	err = container1.PutDevice(ctx, device1)
	if err != nil {
		t.Fatalf("Failed to create device for tenant1: %v", err)
	}
	defer container1.DeleteDevice(ctx, device1)

	// Create a device with the SAME JID for tenant2
	device2 := newTestDevice(container2, jid1) // Same JID as tenant1
	err = container2.PutDevice(ctx, device2)
	if err != nil {
		t.Fatalf("Failed to create device for tenant2: %v", err)
	}
	defer container2.DeleteDevice(ctx, device2)

	// Test 1: Verify both devices exist and have different data
	t.Run("SameJIDDifferentTenants", func(t *testing.T) {
		retrieved1, err := container1.GetDevice(ctx, jid1)
		if err != nil {
			t.Fatalf("Failed to get device from tenant1: %v", err)
		}
		if retrieved1 == nil {
			t.Fatal("Device not found in tenant1")
		}

		retrieved2, err := container2.GetDevice(ctx, jid1)
		if err != nil {
			t.Fatalf("Failed to get device from tenant2: %v", err)
		}
		if retrieved2 == nil {
			t.Fatal("Device not found in tenant2")
		}

		// Each tenant must read back the device IT wrote. NoiseKey.Pub is a
		// *[32]byte, so it must be dereferenced: comparing the pointers would
		// compare two freshly allocated arrays and could never fail.
		if *retrieved1.NoiseKey.Pub != *device1.NoiseKey.Pub {
			t.Error("Tenant1 read back a device that is not its own - tenant isolation broken!")
		}
		if *retrieved2.NoiseKey.Pub != *device2.NoiseKey.Pub {
			t.Error("Tenant2 read back a device that is not its own - tenant isolation broken!")
		}
	})

	// Test 2: Verify tenant1 cannot see tenant2's devices
	t.Run("CannotAccessOtherTenantDevice", func(t *testing.T) {
		// Get all devices from tenant1
		devices1, err := container1.GetAllDevices(ctx)
		if err != nil {
			t.Fatalf("Failed to get all devices from tenant1: %v", err)
		}

		// Should only see 1 device
		if len(devices1) != 1 {
			t.Errorf("Tenant1 sees %d devices, expected 1", len(devices1))
		} else if *devices1[0].NoiseKey.Pub != *device1.NoiseKey.Pub {
			t.Error("Tenant1's only device is not the one it wrote - tenant isolation broken!")
		}

		// Get all devices from tenant2
		devices2, err := container2.GetAllDevices(ctx)
		if err != nil {
			t.Fatalf("Failed to get all devices from tenant2: %v", err)
		}

		// Should only see 1 device
		if len(devices2) != 1 {
			t.Errorf("Tenant2 sees %d devices, expected 1", len(devices2))
		} else if *devices2[0].NoiseKey.Pub != *device2.NoiseKey.Pub {
			t.Error("Tenant2's only device is not the one it wrote - tenant isolation broken!")
		}
	})

	// Test 3: Test session isolation
	t.Run("SessionIsolation", func(t *testing.T) {
		store1 := sqlstore.NewSQLStore(container1, jid1)
		store2 := sqlstore.NewSQLStore(container2, jid1)

		// Put a session in tenant1
		sessionData1 := []byte("tenant1-session-data")
		err := store1.PutSession(ctx, "contact@whatsapp.net", sessionData1)
		if err != nil {
			t.Fatalf("Failed to put session in tenant1: %v", err)
		}

		// Put a different session with same address in tenant2
		sessionData2 := []byte("tenant2-session-data")
		err = store2.PutSession(ctx, "contact@whatsapp.net", sessionData2)
		if err != nil {
			t.Fatalf("Failed to put session in tenant2: %v", err)
		}

		// Verify tenant1 gets its own session
		retrieved1, err := store1.GetSession(ctx, "contact@whatsapp.net")
		if err != nil {
			t.Fatalf("Failed to get session from tenant1: %v", err)
		}
		if string(retrieved1) != string(sessionData1) {
			t.Errorf("Tenant1 session data mismatch: got %s, want %s", retrieved1, sessionData1)
		}

		// Verify tenant2 gets its own session
		retrieved2, err := store2.GetSession(ctx, "contact@whatsapp.net")
		if err != nil {
			t.Fatalf("Failed to get session from tenant2: %v", err)
		}
		if string(retrieved2) != string(sessionData2) {
			t.Errorf("Tenant2 session data mismatch: got %s, want %s", retrieved2, sessionData2)
		}
	})

	// Test 4: Test contact isolation
	t.Run("ContactIsolation", func(t *testing.T) {
		store1 := sqlstore.NewSQLStore(container1, jid1)
		store2 := sqlstore.NewSQLStore(container2, jid1)

		contactJID, _ := types.ParseJID("contact@whatsapp.net")

		// Put contact in tenant1 (arguments are firstName, fullName)
		err := store1.PutContactName(ctx, contactJID, "T1", "Tenant1 Contact")
		if err != nil {
			t.Fatalf("Failed to put contact in tenant1: %v", err)
		}

		// Put different contact with same JID in tenant2
		err = store2.PutContactName(ctx, contactJID, "T2", "Tenant2 Contact")
		if err != nil {
			t.Fatalf("Failed to put contact in tenant2: %v", err)
		}

		// Read back through FRESH stores: SQLStore keeps a write-through contact
		// cache, so reusing store1/store2 would assert on a Go map rather than on
		// what Postgres actually holds.
		reader1 := sqlstore.NewSQLStore(container1, jid1)
		reader2 := sqlstore.NewSQLStore(container2, jid1)

		contact1, err := reader1.GetContact(ctx, contactJID)
		if err != nil {
			t.Fatalf("Failed to get contact from tenant1: %v", err)
		}
		if contact1.FirstName != "T1" || contact1.FullName != "Tenant1 Contact" {
			t.Errorf("Tenant1 contact mismatch: got %q/%q, want T1/Tenant1 Contact", contact1.FirstName, contact1.FullName)
		}

		contact2, err := reader2.GetContact(ctx, contactJID)
		if err != nil {
			t.Fatalf("Failed to get contact from tenant2: %v", err)
		}
		if contact2.FirstName != "T2" || contact2.FullName != "Tenant2 Contact" {
			t.Errorf("Tenant2 contact mismatch: got %q/%q, want T2/Tenant2 Contact", contact2.FirstName, contact2.FullName)
		}
	})

	// Test 5: Test identity key isolation
	t.Run("IdentityKeyIsolation", func(t *testing.T) {
		store1 := sqlstore.NewSQLStore(container1, jid1)
		store2 := sqlstore.NewSQLStore(container2, jid1)

		var key1, key2 [32]byte
		for i := range key1 {
			key1[i] = byte(i)
			key2[i] = byte(i + 100)
		}

		// Put identity key in tenant1
		err := store1.PutIdentity(ctx, "contact@whatsapp.net:1", key1)
		if err != nil {
			t.Fatalf("Failed to put identity in tenant1: %v", err)
		}

		// Put different identity key in tenant2
		err = store2.PutIdentity(ctx, "contact@whatsapp.net:1", key2)
		if err != nil {
			t.Fatalf("Failed to put identity in tenant2: %v", err)
		}

		// Verify tenant1's key is trusted for tenant1
		trusted1, err := store1.IsTrustedIdentity(ctx, "contact@whatsapp.net:1", key1)
		if err != nil {
			t.Fatalf("Failed to check identity in tenant1: %v", err)
		}
		if !trusted1 {
			t.Error("Tenant1's own key is not trusted")
		}

		// Verify tenant2's key is NOT trusted for tenant1 (different key)
		trusted1wrong, err := store1.IsTrustedIdentity(ctx, "contact@whatsapp.net:1", key2)
		if err != nil {
			t.Fatalf("Failed to check wrong identity in tenant1: %v", err)
		}
		if trusted1wrong {
			t.Error("Tenant2's key is incorrectly trusted in tenant1 - SECURITY BREACH!")
		}
	})
}

// TestDeleteCascade tests that deleting a device cascades to all related data
func TestDeleteCascade(t *testing.T) {
	dbURL := getTestDatabaseURL()
	if dbURL == "" {
		t.Skip("Skipping delete cascade test: no database URL provided")
	}

	ctx := context.Background()

	dbPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	defer dbPool.Close()

	applySchema(t, dbPool)
	clearTenants(t, dbPool, "test-tenant")

	container := sqlstore.NewContainer(dbPool, "test-tenant", waLog.Noop)
	jid, _ := types.ParseJID("test@whatsapp.net")
	device := newTestDevice(container, jid)

	err = container.PutDevice(ctx, device)
	if err != nil {
		t.Fatalf("Failed to create device: %v", err)
	}

	// Create associated data
	store := sqlstore.NewSQLStore(container, jid)

	// Add a session
	err = store.PutSession(ctx, "contact@whatsapp.net", []byte("test-session"))
	if err != nil {
		t.Fatalf("Failed to put session: %v", err)
	}

	// Add a contact (arguments are firstName, fullName)
	contactJID, _ := types.ParseJID("contact@whatsapp.net")
	err = store.PutContactName(ctx, contactJID, "TC", "Test Contact")
	if err != nil {
		t.Fatalf("Failed to put contact: %v", err)
	}

	// Sanity check: the rows the cascade is supposed to remove must exist first,
	// otherwise the assertions below would pass on an empty database.
	for table, col := range map[string]string{"whatsmeow_sessions": "our_jid", "whatsmeow_contacts": "our_jid"} {
		var n int
		q := "SELECT count(*) FROM " + table + " WHERE business_id=$1 AND " + col + "=$2"
		if err = dbPool.QueryRow(ctx, q, "test-tenant", jid.String()).Scan(&n); err != nil {
			t.Fatalf("Failed to count rows in %s: %v", table, err)
		}
		if n == 0 {
			t.Fatalf("Expected a row in %s before deleting the device, found none", table)
		}
	}

	// Now delete the device
	err = container.DeleteDevice(ctx, device)
	if err != nil {
		t.Fatalf("Failed to delete device: %v", err)
	}

	// Verify device is gone
	retrieved, err := container.GetDevice(ctx, jid)
	if err != nil {
		t.Fatalf("Error checking deleted device: %v", err)
	}
	if retrieved != nil {
		t.Error("Device still exists after deletion")
	}

	// A plain DELETE FROM whatsmeow_device succeeds even if the foreign keys were
	// missing entirely, so the cascade has to be checked explicitly rather than
	// inferred from the delete succeeding. whatsmeow_lid_map is per-business rather
	// than per-device by design, so it is not included here.
	deviceScoped := map[string]string{
		"whatsmeow_sessions":                "our_jid",
		"whatsmeow_contacts":                "our_jid",
		"whatsmeow_identity_keys":           "our_jid",
		"whatsmeow_sender_keys":             "our_jid",
		"whatsmeow_chat_settings":           "our_jid",
		"whatsmeow_message_secrets":         "our_jid",
		"whatsmeow_privacy_tokens":          "our_jid",
		"whatsmeow_nct_salt":                "our_jid",
		"whatsmeow_event_buffer":            "our_jid",
		"whatsmeow_retry_buffer":            "our_jid",
		"whatsmeow_pre_keys":                "jid",
		"whatsmeow_app_state_sync_keys":     "jid",
		"whatsmeow_app_state_version":       "jid",
		"whatsmeow_app_state_mutation_macs": "jid",
	}
	for table, col := range deviceScoped {
		var n int
		q := "SELECT count(*) FROM " + table + " WHERE business_id=$1 AND " + col + "=$2"
		if err = dbPool.QueryRow(ctx, q, "test-tenant", jid.String()).Scan(&n); err != nil {
			t.Errorf("Failed to count leftover rows in %s: %v", table, err)
			continue
		}
		if n != 0 {
			t.Errorf("%s still holds %d row(s) for the deleted device - ON DELETE CASCADE did not fire", table, n)
		}
	}
}

// getTestDatabaseURL returns the PostgreSQL DSN the multitenancy tests run against.
// The tests skip when it is unset, so they are a no-op in environments without a
// database. Example:
//
//	TEST_DB_URL=postgres://user:pass@localhost:5432/whatsmeow_test go test ./store/sqlstore/
func getTestDatabaseURL() string {
	return os.Getenv("TEST_DB_URL")
}

// rlsTables lists every tenant table covered by rls_policies.sql.
var rlsTables = []string{
	"whatsmeow_device",
	"whatsmeow_identity_keys",
	"whatsmeow_pre_keys",
	"whatsmeow_sessions",
	"whatsmeow_sender_keys",
	"whatsmeow_app_state_sync_keys",
	"whatsmeow_app_state_version",
	"whatsmeow_app_state_mutation_macs",
	"whatsmeow_contacts",
	"whatsmeow_chat_settings",
	"whatsmeow_message_secrets",
	"whatsmeow_privacy_tokens",
	"whatsmeow_lid_map",
	"whatsmeow_event_buffer",
	"whatsmeow_retry_buffer",
	"whatsmeow_nct_salt",
}

// applySchema brings the test database up to the latest schema, so the tests can run
// against an empty database.
func applySchema(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	instance := &sqlstore.ClientInstance{DbPool: pool, Log: waLog.Noop}
	if err := instance.Upgrade(); err != nil {
		t.Fatalf("Failed to apply schema: %v", err)
	}
}

// clearTenants removes every device of the given business IDs, and with it (through
// ON DELETE CASCADE) all their associated rows. Several assertions below are absolute
// counts, so a run that was interrupted after PutDevice would otherwise fail every
// later run for a reason unrelated to tenant isolation. Must be called before row level
// security is turned on, since it goes through a plain pool.
func clearTenants(t *testing.T, pool *pgxpool.Pool, businessIDs ...string) {
	t.Helper()
	for _, businessID := range businessIDs {
		if _, err := pool.Exec(context.Background(), "DELETE FROM whatsmeow_device WHERE business_id=$1", businessID); err != nil {
			t.Fatalf("Failed to clear tenant %q: %v", businessID, err)
		}
	}
}

// tearDownRLS undoes everything rls_policies.sql does, so the shared test database is
// left exactly as TestRowLevelSecurity found it. It must undo NO FORCE and drop the
// policies too, not just DISABLE: leaving FORCE and the policies in place would turn
// the database into one that a single stray ENABLE makes deny every query. It reports
// failures with Errorf rather than Fatalf so one failing statement does not abandon the
// remaining tables.
func tearDownRLS(t *testing.T, pool *pgxpool.Pool) {
	t.Helper()
	ctx := context.Background()
	for _, table := range rlsTables {
		suffix := strings.TrimPrefix(table, "whatsmeow_")
		for _, stmt := range []string{
			"ALTER TABLE " + table + " DISABLE ROW LEVEL SECURITY",
			"ALTER TABLE " + table + " NO FORCE ROW LEVEL SECURITY",
			"DROP POLICY IF EXISTS tenant_isolation_" + suffix + " ON " + table,
			"DROP POLICY IF EXISTS tenant_isolation_" + suffix + "_insert ON " + table,
		} {
			if _, err := pool.Exec(ctx, stmt); err != nil {
				t.Errorf("RLS teardown %q failed: %v", stmt, err)
			}
		}
	}
}

// TestRowLevelSecurity checks the optional defense-in-depth layer: with
// EnableTenantRLS the policies in rls_policies.sql let each tenant through on a shared
// pool, and without it they deny everything.
func TestRowLevelSecurity(t *testing.T) {
	dbURL := getTestDatabaseURL()
	if dbURL == "" {
		t.Skip("Skipping row level security test: no database URL provided (set TEST_DB_URL)")
	}

	ctx := context.Background()

	adminPool, err := pgxpool.New(ctx, dbURL)
	if err != nil {
		t.Fatalf("Failed to connect to database: %v", err)
	}
	// Registered as a Cleanup rather than a defer, and before the teardown below, so
	// it runs after it: Cleanup functions run after the test's defers, and in reverse
	// registration order.
	t.Cleanup(adminPool.Close)

	// Superusers and BYPASSRLS roles ignore policies entirely, which would make this
	// test assert nothing.
	var bypasses bool
	err = adminPool.QueryRow(ctx, "SELECT rolsuper OR rolbypassrls FROM pg_roles WHERE rolname = current_user").Scan(&bypasses)
	if err != nil {
		t.Fatalf("Failed to check current role: %v", err)
	}
	if bypasses {
		t.Skip("Skipping row level security test: current role bypasses RLS (superuser or BYPASSRLS)")
	}

	applySchema(t, adminPool)
	// Start from a known state in case an earlier run was killed, and register the
	// teardown before anything can fail so it always runs.
	tearDownRLS(t, adminPool)
	t.Cleanup(func() { tearDownRLS(t, adminPool) })
	clearTenants(t, adminPool, "rls-tenant1", "rls-tenant2")

	policies, err := os.ReadFile("rls_policies.sql")
	if err != nil {
		t.Fatalf("Failed to read rls_policies.sql: %v", err)
	}
	if _, err = adminPool.Exec(ctx, string(policies)); err != nil {
		t.Fatalf("Failed to apply RLS policies: %v", err)
	}

	cfg, err := pgxpool.ParseConfig(dbURL)
	if err != nil {
		t.Fatalf("Failed to parse DSN: %v", err)
	}
	sqlstore.EnableTenantRLS(cfg)
	rlsPool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("Failed to create RLS-aware pool: %v", err)
	}
	defer rlsPool.Close()

	// One shared pool, two tenants: exactly the coordinator's topology.
	container1 := sqlstore.NewContainer(rlsPool, "rls-tenant1", waLog.Noop)
	container2 := sqlstore.NewContainer(rlsPool, "rls-tenant2", waLog.Noop)

	jid, _ := types.ParseJID("rlsuser@whatsapp.net")
	device1 := newTestDevice(container1, jid)
	if err = container1.PutDevice(ctx, device1); err != nil {
		t.Fatalf("PutDevice for tenant1 failed with RLS enabled: %v", err)
	}
	defer container1.DeleteDevice(ctx, device1)

	device2 := newTestDevice(container2, jid)
	if err = container2.PutDevice(ctx, device2); err != nil {
		t.Fatalf("PutDevice for tenant2 failed with RLS enabled: %v", err)
	}
	defer container2.DeleteDevice(ctx, device2)

	t.Run("EachTenantSeesOnlyItsOwnRows", func(t *testing.T) {
		devices1, err := container1.GetAllDevices(ctx)
		if err != nil {
			t.Fatalf("Failed to get devices for tenant1: %v", err)
		}
		if len(devices1) != 1 {
			t.Errorf("Tenant1 sees %d devices, expected 1", len(devices1))
		} else if *devices1[0].NoiseKey.Pub != *device1.NoiseKey.Pub {
			t.Error("Tenant1 got another tenant's device through the shared RLS pool")
		}
		devices2, err := container2.GetAllDevices(ctx)
		if err != nil {
			t.Fatalf("Failed to get devices for tenant2: %v", err)
		}
		if len(devices2) != 1 {
			t.Errorf("Tenant2 sees %d devices, expected 1", len(devices2))
		} else if *devices2[0].NoiseKey.Pub != *device2.NoiseKey.Pub {
			t.Error("Tenant2 got another tenant's device through the shared RLS pool")
		}
	})

	t.Run("PoolWithoutHookIsDenied", func(t *testing.T) {
		plainPool, err := pgxpool.New(ctx, dbURL)
		if err != nil {
			t.Fatalf("Failed to create plain pool: %v", err)
		}
		defer plainPool.Close()

		plain := sqlstore.NewContainer(plainPool, "rls-tenant1", waLog.Noop)
		devices, err := plain.GetAllDevices(ctx)
		if err != nil {
			t.Fatalf("Unexpected error reading through a plain pool: %v", err)
		}
		if len(devices) != 0 {
			t.Errorf("Pool without EnableTenantRLS saw %d devices, expected 0", len(devices))
		}

		otherJID, _ := types.ParseJID("rlsdenied@whatsapp.net")
		unscoped := newTestDevice(plain, otherJID)
		if err = plain.PutDevice(ctx, unscoped); err == nil {
			// Clean up through the RLS-aware container: a DELETE on the plain pool
			// matches zero rows, which would strand this device under rls-tenant1
			// and break every later run of the sibling subtest.
			if delErr := container1.DeleteDevice(ctx, unscoped); delErr != nil {
				t.Errorf("Failed to remove the device inserted through the unscoped pool: %v", delErr)
			}
			t.Error("PutDevice succeeded through a pool without EnableTenantRLS; the INSERT policy is not enforcing")
		}
	})
}
