// Copyright (c) 2026 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

package sqlstore

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

// businessIDCtxKey tags a context with the business ID of the Container that issued
// the query, so EnableTenantRLS's pool hook knows which tenant a connection is being
// acquired for. Tenant isolation itself does not depend on this: every query carries
// an explicit business_id predicate. This only feeds the optional Postgres row level
// security layer in rls_policies.sql.
type businessIDCtxKey struct{}

func contextWithBusinessID(ctx context.Context, businessID string) context.Context {
	return context.WithValue(ctx, businessIDCtxKey{}, businessID)
}

// businessIDFromContext returns the business ID tagged onto the context by a
// tenantPool, and whether there was one.
func businessIDFromContext(ctx context.Context) (string, bool) {
	businessID, ok := ctx.Value(businessIDCtxKey{}).(string)
	return businessID, ok
}

// tenantPool wraps a pgxpool.Pool and tags the context of every query it issues with
// the business ID it belongs to.
//
// The pool is deliberately NOT embedded. pgxpool.Pool has further context-taking
// methods (BeginTx, SendBatch, CopyFrom, Acquire, AcquireFunc, AcquireAllIdle) which,
// if promoted, would run with an untagged context: EnableTenantRLS would then scope
// that connection to the empty business ID, and under rls_policies.sql the statement
// would see no rows at all. Requiring a new call site to add a wrapper here first
// makes that mistake impossible to compile.
type tenantPool struct {
	pool       *pgxpool.Pool
	businessID string
}

func newTenantPool(pool *pgxpool.Pool, businessID string) *tenantPool {
	if pool == nil {
		return nil
	}
	return &tenantPool{pool: pool, businessID: businessID}
}

func (p *tenantPool) tag(ctx context.Context) context.Context {
	return contextWithBusinessID(ctx, p.businessID)
}

func (p *tenantPool) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	return p.pool.Exec(p.tag(ctx), sql, args...)
}

func (p *tenantPool) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	return p.pool.Query(p.tag(ctx), sql, args...)
}

func (p *tenantPool) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	return p.pool.QueryRow(p.tag(ctx), sql, args...)
}

// Begin acquires the connection with the tagged context, so every statement in the
// returned transaction runs on a connection whose app.current_business_id is already
// set to this tenant.
func (p *tenantPool) Begin(ctx context.Context) (pgx.Tx, error) {
	return p.pool.Begin(p.tag(ctx))
}

func (p *tenantPool) Close() {
	p.pool.Close()
}

// setBusinessIDQuery sets the session variable the policies in rls_policies.sql read.
// SET does not accept bind parameters, hence set_config; the third argument false
// makes it session scoped rather than transaction scoped, so it survives across the
// individual statements a pgx-pooled connection serves.
const setBusinessIDQuery = `SELECT set_config('app.current_business_id', $1, false)`

// rlsTenantConnKey is where the business ID currently set on a connection is
// remembered, in the connection's own CustomData map. Keeping it on the connection
// rather than in a side map means it cannot outlive the connection and needs no
// locking: PrepareConn is the only writer and runs while the acquiring goroutine holds
// the connection exclusively.
const rlsTenantConnKey = "whatsmeow.current_business_id"

// EnableTenantRLS makes a pgxpool set the app.current_business_id session variable
// read by the row level security policies in rls_policies.sql. Call it on the config
// before creating the pool:
//
//	cfg, err := pgxpool.ParseConfig(dsn)
//	sqlstore.EnableTenantRLS(cfg)
//	pool, err := pgxpool.NewWithConfig(ctx, cfg)
//
// This supports the usual multi-tenant topology where a single pool is shared by many
// Containers: the variable is set per acquisition, from the business ID of the
// Container that issued the query, and only when the connection is not already on that
// tenant, so a connection serving one tenant repeatedly costs no extra round trip.
//
// A connection whose variable cannot be set is destroyed and the underlying error is
// returned to the caller, so nobody can end up on a connection scoped to another
// tenant. Queries issued on the raw pool rather than through a Container are treated
// as the empty business ID, which is also what the single-tenant sqlstore.New path
// uses.
//
// REQUIREMENT: the pool must talk to PostgreSQL directly, or through a proxy in
// session pooling mode. app.current_business_id is a session variable set once per
// pool acquisition; a transaction-pooling proxy (PgBouncer pool_mode = transaction or
// statement, and similar) re-multiplexes each statement onto a different server
// backend, so the variable is usually absent when the query runs. The policies then
// match nothing and reads return zero rows with a nil error, which the library reads
// as "no session" or "device not paired". Do not enable row level security behind a
// transaction pooler.
//
// Without this call (the default), no session variable is ever set,
// current_setting(..., true) returns NULL and the policies match nothing: do not apply
// rls_policies.sql unless the pool is configured here. Existing PrepareConn,
// BeforeAcquire and BeforeClose hooks on the config are preserved and run first.
func EnableTenantRLS(cfg *pgxpool.Config) {
	previousPrepareConn := cfg.PrepareConn
	// pgx ignores BeforeAcquire entirely once PrepareConn is set, so fold any existing
	// one into the new hook and clear it rather than leaving it as dead code.
	previousBeforeAcquire := cfg.BeforeAcquire
	cfg.BeforeAcquire = nil

	cfg.PrepareConn = func(ctx context.Context, conn *pgx.Conn) (bool, error) {
		if previousPrepareConn != nil {
			if ok, err := previousPrepareConn(ctx, conn); !ok || err != nil {
				return ok, err
			}
		} else if previousBeforeAcquire != nil && !previousBeforeAcquire(ctx, conn) {
			return false, nil
		}

		businessID, _ := businessIDFromContext(ctx)
		connData := conn.PgConn().CustomData()
		if current, ok := connData[rlsTenantConnKey].(string); ok && current == businessID {
			return true, nil
		}

		// Drop the claim before the round trip: if it fails, the connection must not
		// look like it is scoped to anything.
		delete(connData, rlsTenantConnKey)
		if _, err := conn.Exec(ctx, setBusinessIDQuery, businessID); err != nil {
			// Returning the error rather than a bare false destroys this connection
			// once and surfaces the real cause, instead of making pgxpool retry over
			// MaxConns+1 fresh connections and report an "infinite loop" of its own.
			return false, fmt.Errorf("failed to set app.current_business_id to %q: %w", businessID, err)
		}
		connData[rlsTenantConnKey] = businessID
		return true, nil
	}
}
