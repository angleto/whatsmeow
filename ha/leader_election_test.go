// Copyright (c) 2025 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package ha

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	waLog "go.mau.fi/whatsmeow/util/log"
)

// TestLockTagParts pins the split against the layout PostgreSQL uses for a
// one-argument advisory lock: classid takes the high 32 bits, objid the low ones.
func TestLockTagParts(t *testing.T) {
	cases := []struct {
		name    string
		lockID  int64
		classID uint32
		objID   uint32
	}{
		{"zero", 0, 0, 0},
		{"fits in objid alone", 42, 0, 42},
		{"boundary", 1 << 32, 1, 0},
		// The value from the production log that exposed the bug.
		{"real key", 3796319524095469536, 883899518, 1335306208},
		{"negative", -1, 0xFFFFFFFF, 0xFFFFFFFF},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			classID, objID := lockTagParts(tc.lockID)
			if classID != tc.classID || objID != tc.objID {
				t.Errorf("lockTagParts(%d) = (%d, %d), want (%d, %d)",
					tc.lockID, classID, objID, tc.classID, tc.objID)
			}
		})
	}
}

// TestGenerateLockIDExceedsOID documents why the single-parameter form could never
// work: essentially every generated key is wider than the oid columns it was being
// compared against.
func TestGenerateLockIDExceedsOID(t *testing.T) {
	for _, id := range []string{"whatswoof:schema-migration", "whatswoof:abc", "whatswoof:1"} {
		if lockID := GenerateLockID(id); lockID <= 0xFFFFFFFF {
			t.Errorf("GenerateLockID(%q) = %d, unexpectedly fits in a uint32", id, lockID)
		}
	}
}

// newTestPool mirrors the single pinned connection production uses for advisory
// locks: they are session-scoped, and VerifyLeadership matches on pg_backend_pid().
func newTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("TEST_DB_URL")
	if dsn == "" {
		t.Skip("TEST_DB_URL not set")
	}
	config, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("failed to parse TEST_DB_URL: %v", err)
	}
	config.MaxConns = 1
	config.MinConns = 1
	pool, err := pgxpool.NewWithConfig(context.Background(), config)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

// TestVerifyLeadershipRoundTrip is the regression test for the oid encoding bug:
// with a key above 2^32 (which is what GenerateLockID yields) VerifyLeadership used
// to return an error on every call, so a lost lock session was never detected.
func TestVerifyLeadershipRoundTrip(t *testing.T) {
	pool := newTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	lockID := GenerateLockID("whatswoof:test:" + t.Name())
	le := NewLeaderElection(pool, lockID, waLog.Noop)

	acquired, err := le.TryAcquire(ctx)
	if err != nil {
		t.Fatalf("TryAcquire: %v", err)
	}
	if !acquired {
		t.Fatal("TryAcquire returned false on a fresh lock")
	}

	valid, err := le.VerifyLeadership(ctx)
	if err != nil {
		t.Fatalf("VerifyLeadership while holding the lock: %v", err)
	}
	if !valid {
		t.Error("VerifyLeadership = false while the lock is held")
	}

	if err := le.Release(ctx); err != nil {
		t.Fatalf("Release: %v", err)
	}

	valid, err = le.VerifyLeadership(ctx)
	if err != nil {
		t.Fatalf("VerifyLeadership after release: %v", err)
	}
	if valid {
		t.Error("VerifyLeadership = true after the lock was released")
	}
}

// TestVerifyLeadershipForeignLock checks the tag match is precise: holding one lock
// must not make a different key look held. Both keys share no 32-bit half.
func TestVerifyLeadershipForeignLock(t *testing.T) {
	pool := newTestPool(t)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	held := NewLeaderElection(pool, GenerateLockID("whatswoof:test:held:"+t.Name()), waLog.Noop)
	other := NewLeaderElection(pool, GenerateLockID("whatswoof:test:other:"+t.Name()), waLog.Noop)

	if acquired, err := held.TryAcquire(ctx); err != nil || !acquired {
		t.Fatalf("TryAcquire: acquired=%v err=%v", acquired, err)
	}
	defer func() {
		if err := held.Release(context.Background()); err != nil {
			t.Errorf("Release: %v", err)
		}
	}()

	valid, err := other.VerifyLeadership(ctx)
	if err != nil {
		t.Fatalf("VerifyLeadership on an unheld key: %v", err)
	}
	if valid {
		t.Error("VerifyLeadership = true for a key this session never acquired")
	}
}
