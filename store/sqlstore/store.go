// Copyright (c) 2025 Tulir Asokan
//
// This Source Code Form is subject to the terms of the Mozilla Public
// License, v. 2.0. If a copy of the MPL was not distributed with this
// file, You can obtain one at http://mozilla.org/MPL/2.0/.

// Package sqlstore contains an SQL-backed implementation of the interfaces in the store package.
package sqlstore

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.mau.fi/util/exsync"

	"go.mau.fi/whatsmeow/store"
	"go.mau.fi/whatsmeow/types"
	"go.mau.fi/whatsmeow/util/keys"
)

// ErrInvalidLength is returned by some database getters if the database returned a byte array with an unexpected length.
// This should be impossible, as the database schema contains CHECK()s for all the relevant columns.
var ErrInvalidLength = errors.New("database returned byte array with illegal length")

type SQLStore struct {
	*Container
	businessId string
	JID        string

	preKeyLock sync.Mutex

	contactCache     map[types.JID]*types.ContactInfo
	contactCacheLock sync.Mutex

	migratedPNSessionsCache *exsync.Set[string]
	dbPool                  *pgxpool.Pool
}

// scannable is an interface for pgx rows and row objects.
type scannable interface {
	Scan(dest ...any) error
}

// NewSQLStore creates a new SQLStore with the given database container and user JID.
// It contains implementations of all the different stores in the store package.
//
// In general, you should use Container.NewDevice or Container.GetDevice instead of this.
func NewSQLStore(c *Container, jid types.JID) *SQLStore {
	return &SQLStore{
		Container:               c,
		businessId:              c.businessId,
		JID:                     jid.String(),
		dbPool:                  c.dbPool,
		contactCache:            make(map[types.JID]*types.ContactInfo),
		migratedPNSessionsCache: exsync.NewSet[string](),
	}
}

var _ store.AllSessionSpecificStores = (*SQLStore)(nil)

const (
	putIdentityQuery = `
		INSERT INTO whatsmeow_identity_keys (business_id, our_jid, their_id, identity) VALUES ($1, $2, $3, $4)
		ON CONFLICT (business_id, our_jid, their_id) DO UPDATE SET identity=excluded.identity
	`
	deleteAllIdentitiesQuery = `DELETE FROM whatsmeow_identity_keys WHERE business_id=$1 AND our_jid=$2 AND their_id LIKE $3`
	deleteIdentityQuery      = `DELETE FROM whatsmeow_identity_keys WHERE business_id=$1 AND our_jid=$2 AND their_id=$3`
	getIdentityQuery         = `SELECT identity FROM whatsmeow_identity_keys WHERE business_id=$1 AND our_jid=$2 AND their_id=$3`
)

func (s *SQLStore) PutIdentity(ctx context.Context, address string, key [32]byte) error {
	_, err := s.dbPool.Exec(ctx, putIdentityQuery, s.businessId, s.JID, address, key[:])
	return err
}

func (s *SQLStore) DeleteAllIdentities(ctx context.Context, phone string) error {
	_, err := s.dbPool.Exec(ctx, deleteAllIdentitiesQuery, s.businessId, s.JID, phone+":%")
	return err
}

func (s *SQLStore) DeleteIdentity(ctx context.Context, address string) error {
	_, err := s.dbPool.Exec(ctx, deleteIdentityQuery, s.businessId, s.JID, address)
	return err
}

func (s *SQLStore) IsTrustedIdentity(ctx context.Context, address string, key [32]byte) (bool, error) {
	var existingIdentity []byte
	err := s.dbPool.QueryRow(ctx, getIdentityQuery, s.businessId, s.JID, address).Scan(&existingIdentity)
	if errors.Is(err, pgx.ErrNoRows) {
		// Trust if not known, it'll be saved automatically later
		return true, nil
	} else if err != nil {
		return false, err
	} else if len(existingIdentity) != 32 {
		return false, ErrInvalidLength
	}
	return *(*[32]byte)(existingIdentity) == key, nil
}

const (
	getSessionQuery    = `SELECT session FROM whatsmeow_sessions WHERE business_id=$1 AND our_jid=$2 AND their_id=$3`
	hasSessionQuery    = `SELECT true FROM whatsmeow_sessions WHERE business_id=$1 AND our_jid=$2 AND their_id=$3`
	putSessionQuery    = `
		INSERT INTO whatsmeow_sessions (business_id, our_jid, their_id, session) VALUES ($1, $2, $3, $4)
		ON CONFLICT (business_id, our_jid, their_id) DO UPDATE SET session=excluded.session
	`
	deleteAllSessionsQuery = `DELETE FROM whatsmeow_sessions WHERE business_id=$1 AND our_jid=$2 AND their_id LIKE $3`
	deleteSessionQuery     = `DELETE FROM whatsmeow_sessions WHERE business_id=$1 AND our_jid=$2 AND their_id=$3`

	migratePNToLIDSessionsQuery = `
		INSERT INTO whatsmeow_sessions (business_id, our_jid, their_id, session)
		SELECT $1, our_jid, replace(their_id, $3, $4), session
		FROM whatsmeow_sessions
		WHERE business_id=$1 AND our_jid=$2 AND their_id LIKE $3 || ':%'
		ON CONFLICT (business_id, our_jid, their_id) DO UPDATE SET session=excluded.session
	`
	deleteAllIdentityKeysQuery      = `DELETE FROM whatsmeow_identity_keys WHERE business_id=$1 AND our_jid=$2 AND their_id LIKE $3`
	migratePNToLIDIdentityKeysQuery = `
		INSERT INTO whatsmeow_identity_keys (business_id, our_jid, their_id, identity)
		SELECT $1, our_jid, replace(their_id, $3, $4), identity
		FROM whatsmeow_identity_keys
		WHERE business_id=$1 AND our_jid=$2 AND their_id LIKE $3 || ':%'
		ON CONFLICT (business_id, our_jid, their_id) DO UPDATE SET identity=excluded.identity
	`
	deleteAllSenderKeysQuery      = `DELETE FROM whatsmeow_sender_keys WHERE business_id=$1 AND our_jid=$2 AND sender_id LIKE $3`
	migratePNToLIDSenderKeysQuery = `
		INSERT INTO whatsmeow_sender_keys (business_id, our_jid, chat_id, sender_id, sender_key)
		SELECT $1, our_jid, chat_id, replace(sender_id, $3, $4), sender_key
		FROM whatsmeow_sender_keys
		WHERE business_id=$1 AND our_jid=$2 AND sender_id LIKE $3 || ':%'
		ON CONFLICT (business_id, our_jid, chat_id, sender_id) DO UPDATE SET sender_key=excluded.sender_key
	`
)

func (s *SQLStore) GetSession(ctx context.Context, address string) (session []byte, err error) {
	err = s.dbPool.QueryRow(ctx, getSessionQuery, s.businessId, s.JID, address).Scan(&session)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}

func (s *SQLStore) HasSession(ctx context.Context, address string) (has bool, err error) {
	err = s.dbPool.QueryRow(ctx, hasSessionQuery, s.businessId, s.JID, address).Scan(&has)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}

func (s *SQLStore) GetManySessions(ctx context.Context, addresses []string) (map[string][]byte, error) {
	if len(addresses) == 0 {
		return nil, nil
	}

	placeholders := make([]string, len(addresses))
	params := make([]any, len(addresses)+2)
	params[0] = s.businessId
	params[1] = s.JID
	for i, addr := range addresses {
		placeholders[i] = fmt.Sprintf("$%d", i+3)
		params[i+2] = addr
	}

	query := fmt.Sprintf(
		"SELECT their_id, session FROM whatsmeow_sessions WHERE business_id=$1 AND our_jid=$2 AND their_id IN (%s)",
		strings.Join(placeholders, ","),
	)

	rows, err := s.dbPool.Query(ctx, query, params...)
	if err != nil {
		return nil, fmt.Errorf("error querying sessions: %w", err)
	}
	defer rows.Close()

	result := make(map[string][]byte, len(addresses))
	for _, addr := range addresses {
		result[addr] = nil
	}
	for rows.Next() {
		var addr string
		var session []byte
		if err := rows.Scan(&addr, &session); err != nil {
			return nil, fmt.Errorf("error scanning session: %w", err)
		}
		result[addr] = session
	}
	if err = rows.Err(); err != nil {
		return nil, fmt.Errorf("error iterating sessions: %w", err)
	}

	return result, nil
}

func (s *SQLStore) PutManySessions(ctx context.Context, sessions map[string][]byte) error {
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for addr, sess := range sessions {
		if _, err = tx.Exec(ctx, putSessionQuery, s.businessId, s.JID, addr, sess); err != nil {
			return fmt.Errorf("error inserting session for %s: %w", addr, err)
		}
	}

	return tx.Commit(ctx)
}

func (s *SQLStore) PutSession(ctx context.Context, address string, session []byte) error {
	_, err := s.dbPool.Exec(ctx, putSessionQuery, s.businessId, s.JID, address, session)
	return err
}

func (s *SQLStore) DeleteAllSessions(ctx context.Context, phone string) error {
	return s.deleteAllSessions(ctx, phone)
}

func (s *SQLStore) deleteAllSessions(ctx context.Context, phone string) error {
	_, err := s.dbPool.Exec(ctx, deleteAllSessionsQuery, s.businessId, s.JID, phone+":%")
	return err
}

func (s *SQLStore) deleteAllSenderKeys(ctx context.Context, phone string) error {
	_, err := s.dbPool.Exec(ctx, deleteAllSenderKeysQuery, s.businessId, s.JID, phone+":%")
	return err
}

func (s *SQLStore) deleteAllIdentityKeys(ctx context.Context, phone string) error {
	_, err := s.dbPool.Exec(ctx, deleteAllIdentityKeysQuery, s.businessId, s.JID, phone+":%")
	return err
}

func (s *SQLStore) DeleteSession(ctx context.Context, address string) error {
	_, err := s.dbPool.Exec(ctx, deleteSessionQuery, s.businessId, s.JID, address)
	return err
}

func (s *SQLStore) MigratePNToLID(ctx context.Context, pn, lid types.JID) error {
	pnSignal := pn.SignalAddressUser()
	if !s.migratedPNSessionsCache.Add(pnSignal) {
		return nil
	}
	var sessionsUpdated, identityKeysUpdated, senderKeysUpdated int64
	lidSignal := lid.SignalAddressUser()

	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	// Sessions migration
	res, err := tx.Exec(ctx, migratePNToLIDSessionsQuery, s.businessId, s.JID, pnSignal, lidSignal)
	if err != nil {
		return fmt.Errorf("failed to migrate sessions: %w", err)
	}
	sessionsUpdated = res.RowsAffected()

	if _, err = tx.Exec(ctx, deleteAllSessionsQuery, s.businessId, s.JID, pnSignal+":%"); err != nil {
		return fmt.Errorf("failed to delete extra sessions: %w", err)
	}

	// Identity keys migration
	res, err = tx.Exec(ctx, migratePNToLIDIdentityKeysQuery, s.businessId, s.JID, pnSignal, lidSignal)
	if err != nil {
		return fmt.Errorf("failed to migrate identity keys: %w", err)
	}
	identityKeysUpdated = res.RowsAffected()

	if _, err = tx.Exec(ctx, deleteAllIdentityKeysQuery, s.businessId, s.JID, pnSignal+":%"); err != nil {
		return fmt.Errorf("failed to delete extra identity keys: %w", err)
	}

	// Sender keys migration
	res, err = tx.Exec(ctx, migratePNToLIDSenderKeysQuery, s.businessId, s.JID, pnSignal, lidSignal)
	if err != nil {
		return fmt.Errorf("failed to migrate sender keys: %w", err)
	}
	senderKeysUpdated = res.RowsAffected()

	if _, err = tx.Exec(ctx, deleteAllSenderKeysQuery, s.businessId, s.JID, pnSignal+":%"); err != nil {
		return fmt.Errorf("failed to delete extra sender keys: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	if sessionsUpdated > 0 || senderKeysUpdated > 0 || identityKeysUpdated > 0 {
		s.log.Infof("Migrated %d sessions, %d identity keys and %d sender keys from %s to %s", sessionsUpdated, identityKeysUpdated, senderKeysUpdated, pnSignal, lidSignal)
	} else {
		s.log.Debugf("No sessions or sender keys found to migrate from %s to %s", pnSignal, lidSignal)
	}
	return nil
}

const (
	getLastPreKeyIDQuery        = `SELECT MAX(key_id) FROM whatsmeow_pre_keys WHERE business_id=$1 AND jid=$2`
	insertPreKeyQuery           = `INSERT INTO whatsmeow_pre_keys (business_id, jid, key_id, key, uploaded) VALUES ($1, $2, $3, $4, $5)`
	getUnuploadedPreKeysQuery   = `SELECT key_id, key FROM whatsmeow_pre_keys WHERE business_id=$1 AND jid=$2 AND uploaded=false ORDER BY key_id LIMIT $3`
	getPreKeyQuery              = `SELECT key_id, key FROM whatsmeow_pre_keys WHERE business_id=$1 AND jid=$2 AND key_id=$3`
	deletePreKeyQuery           = `DELETE FROM whatsmeow_pre_keys WHERE business_id=$1 AND jid=$2 AND key_id=$3`
	markPreKeysAsUploadedQuery  = `UPDATE whatsmeow_pre_keys SET uploaded=true WHERE business_id=$1 AND jid=$2 AND key_id<=$3`
	getUploadedPreKeyCountQuery = `SELECT COUNT(*) FROM whatsmeow_pre_keys WHERE business_id=$1 AND jid=$2 AND uploaded=true`
)

func (s *SQLStore) genOnePreKey(ctx context.Context, id uint32, markUploaded bool) (*keys.PreKey, error) {
	key := keys.NewPreKey(id)
	_, err := s.dbPool.Exec(ctx, insertPreKeyQuery, s.businessId, s.JID, key.KeyID, key.Priv[:], markUploaded)
	return key, err
}

func (s *SQLStore) getNextPreKeyID(ctx context.Context) (uint32, error) {
	var lastKeyID sql.NullInt32
	err := s.dbPool.QueryRow(ctx, getLastPreKeyIDQuery, s.businessId, s.JID).Scan(&lastKeyID)
	if err != nil {
		return 0, fmt.Errorf("failed to query next prekey ID: %w", err)
	}
	return uint32(lastKeyID.Int32) + 1, nil
}

func (s *SQLStore) GenOnePreKey(ctx context.Context) (*keys.PreKey, error) {
	s.preKeyLock.Lock()
	defer s.preKeyLock.Unlock()
	nextKeyID, err := s.getNextPreKeyID(ctx)
	if err != nil {
		return nil, err
	}
	return s.genOnePreKey(ctx, nextKeyID, true)
}

func (s *SQLStore) GetOrGenPreKeys(ctx context.Context, count uint32) ([]*keys.PreKey, error) {
	s.preKeyLock.Lock()
	defer s.preKeyLock.Unlock()

	res, err := s.dbPool.Query(ctx, getUnuploadedPreKeysQuery, s.businessId, s.JID, count)
	if err != nil {
		return nil, fmt.Errorf("failed to query existing prekeys: %w", err)
	}
	defer res.Close()
	newKeys := make([]*keys.PreKey, count)
	var existingCount uint32
	for res.Next() {
		var key *keys.PreKey
		key, err = scanPreKey(res)
		if err != nil {
			return nil, err
		} else if key != nil {
			newKeys[existingCount] = key
			existingCount++
		}
	}

	if existingCount < uint32(len(newKeys)) {
		var nextKeyID uint32
		nextKeyID, err = s.getNextPreKeyID(ctx)
		if err != nil {
			return nil, err
		}
		for i := existingCount; i < count; i++ {
			newKeys[i], err = s.genOnePreKey(ctx, nextKeyID, false)
			if err != nil {
				return nil, fmt.Errorf("failed to generate prekey: %w", err)
			}
			nextKeyID++
		}
	}

	return newKeys, nil
}

func scanPreKey(row scannable) (*keys.PreKey, error) {
	var priv []byte
	var id uint32
	err := row.Scan(&id, &priv)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	} else if len(priv) != 32 {
		return nil, ErrInvalidLength
	}
	return &keys.PreKey{
		KeyPair: *keys.NewKeyPairFromPrivateKey(*(*[32]byte)(priv)),
		KeyID:   id,
	}, nil
}

func (s *SQLStore) GetPreKey(ctx context.Context, id uint32) (*keys.PreKey, error) {
	return scanPreKey(s.dbPool.QueryRow(ctx, getPreKeyQuery, s.businessId, s.JID, id))
}

func (s *SQLStore) RemovePreKey(ctx context.Context, id uint32) error {
	_, err := s.dbPool.Exec(ctx, deletePreKeyQuery, s.businessId, s.JID, id)
	return err
}

func (s *SQLStore) MarkPreKeysAsUploaded(ctx context.Context, upToID uint32) error {
	_, err := s.dbPool.Exec(ctx, markPreKeysAsUploadedQuery, s.businessId, s.JID, upToID)
	return err
}

func (s *SQLStore) UploadedPreKeyCount(ctx context.Context) (count int, err error) {
	err = s.dbPool.QueryRow(ctx, getUploadedPreKeyCountQuery, s.businessId, s.JID).Scan(&count)
	return
}

const (
	getSenderKeyQuery = `SELECT sender_key FROM whatsmeow_sender_keys WHERE business_id=$1 AND our_jid=$2 AND chat_id=$3 AND sender_id=$4`
	putSenderKeyQuery = `
		INSERT INTO whatsmeow_sender_keys (business_id, our_jid, chat_id, sender_id, sender_key) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (business_id, our_jid, chat_id, sender_id) DO UPDATE SET sender_key=excluded.sender_key
	`
)

func (s *SQLStore) PutSenderKey(ctx context.Context, group, user string, session []byte) error {
	_, err := s.dbPool.Exec(ctx, putSenderKeyQuery, s.businessId, s.JID, group, user, session)
	return err
}

func (s *SQLStore) GetSenderKey(ctx context.Context, group, user string) (key []byte, err error) {
	err = s.dbPool.QueryRow(ctx, getSenderKeyQuery, s.businessId, s.JID, group, user).Scan(&key)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}

const (
	putAppStateSyncKeyQuery = `
		INSERT INTO whatsmeow_app_state_sync_keys (business_id, jid, key_id, key_data, timestamp, fingerprint) VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (business_id, jid, key_id) DO UPDATE
			SET key_data=excluded.key_data, timestamp=excluded.timestamp, fingerprint=excluded.fingerprint
			WHERE excluded.timestamp > whatsmeow_app_state_sync_keys.timestamp
	`
	getAllAppStateSyncKeysQuery     = `SELECT key_data, timestamp, fingerprint FROM whatsmeow_app_state_sync_keys WHERE business_id=$1 AND jid=$2 ORDER BY timestamp DESC`
	getAppStateSyncKeyQuery         = `SELECT key_data, timestamp, fingerprint FROM whatsmeow_app_state_sync_keys WHERE business_id=$1 AND jid=$2 AND key_id=$3`
	getLatestAppStateSyncKeyIDQuery = `SELECT key_id FROM whatsmeow_app_state_sync_keys WHERE business_id=$1 AND jid=$2 ORDER BY timestamp DESC LIMIT 1`
)

func (s *SQLStore) PutAppStateSyncKey(ctx context.Context, id []byte, key store.AppStateSyncKey) error {
	_, err := s.dbPool.Exec(ctx, putAppStateSyncKeyQuery, s.businessId, s.JID, id, key.Data, key.Timestamp, key.Fingerprint)
	return err
}

func (s *SQLStore) GetAllAppStateSyncKeys(ctx context.Context) ([]*store.AppStateSyncKey, error) {
	rows, err := s.dbPool.Query(ctx, getAllAppStateSyncKeysQuery, s.businessId, s.JID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*store.AppStateSyncKey
	for rows.Next() {
		var item store.AppStateSyncKey
		err = rows.Scan(&item.Data, &item.Timestamp, &item.Fingerprint)
		if err != nil {
			return nil, err
		}
		if len(item.Data) > 0 {
			out = append(out, &item)
		}
	}
	return out, rows.Err()
}

func (s *SQLStore) GetAppStateSyncKey(ctx context.Context, id []byte) (*store.AppStateSyncKey, error) {
	var key store.AppStateSyncKey
	err := s.dbPool.QueryRow(ctx, getAppStateSyncKeyQuery, s.businessId, s.JID, id).Scan(&key.Data, &key.Timestamp, &key.Fingerprint)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return &key, err
}

func (s *SQLStore) GetLatestAppStateSyncKeyID(ctx context.Context) ([]byte, error) {
	var keyID []byte
	err := s.dbPool.QueryRow(ctx, getLatestAppStateSyncKeyIDQuery, s.businessId, s.JID).Scan(&keyID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	return keyID, err
}

const (
	putAppStateVersionQuery = `
		INSERT INTO whatsmeow_app_state_version (business_id, jid, name, version, hash) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (business_id, jid, name) DO UPDATE SET version=excluded.version, hash=excluded.hash
	`
	getAppStateVersionQuery      = `SELECT version, hash FROM whatsmeow_app_state_version WHERE business_id=$1 AND jid=$2 AND name=$3`
	deleteAppStateVersionQuery   = `DELETE FROM whatsmeow_app_state_version WHERE business_id=$1 AND jid=$2 AND name=$3`
	putAppStateMutationMACsQuery = `INSERT INTO whatsmeow_app_state_mutation_macs (business_id, jid, name, version, index_mac, value_mac) VALUES `
	getAppStateMutationMACQuery  = `SELECT value_mac FROM whatsmeow_app_state_mutation_macs WHERE business_id=$1 AND jid=$2 AND name=$3 AND index_mac=$4 ORDER BY version DESC LIMIT 1`
)

func (s *SQLStore) PutAppStateVersion(ctx context.Context, name string, version uint64, hash [128]byte) error {
	_, err := s.dbPool.Exec(ctx, putAppStateVersionQuery, s.businessId, s.JID, name, version, hash[:])
	return err
}

func (s *SQLStore) GetAppStateVersion(ctx context.Context, name string) (version uint64, hash [128]byte, err error) {
	var uncheckedHash []byte
	err = s.dbPool.QueryRow(ctx, getAppStateVersionQuery, s.businessId, s.JID, name).Scan(&version, &uncheckedHash)
	if errors.Is(err, pgx.ErrNoRows) {
		// version will be 0 and hash will be an empty array, which is the correct initial state
		err = nil
	} else if err != nil {
		// There's an error, just return it
	} else if len(uncheckedHash) != 128 {
		// This shouldn't happen
		err = ErrInvalidLength
	} else if version == 0 {
		err = fmt.Errorf("invalid saved app state version 0 for name %s (hash %x)", name, uncheckedHash)
	} else {
		// No errors, convert hash slice to array
		hash = *(*[128]byte)(uncheckedHash)
	}
	return
}

func (s *SQLStore) DeleteAppStateVersion(ctx context.Context, name string) error {
	_, err := s.dbPool.Exec(ctx, deleteAppStateVersionQuery, s.businessId, s.JID, name)
	return err
}

func (s *SQLStore) putAppStateMutationMACs(tx pgx.Tx, ctx context.Context, name string, version uint64, mutations []store.AppStateMutationMAC) error {
	values := make([]any, 4+len(mutations)*2)
	queryParts := make([]string, len(mutations))
	values[0] = s.businessId
	values[1] = s.JID
	values[2] = name
	values[3] = version
	placeholderSyntax := "($1, $2, $3, $4, $%d, $%d)"
	for i, mutation := range mutations {
		baseIndex := 4 + i*2
		values[baseIndex] = mutation.IndexMAC
		values[baseIndex+1] = mutation.ValueMAC
		queryParts[i] = fmt.Sprintf(placeholderSyntax, baseIndex+1, baseIndex+2)
	}
	_, err := tx.Exec(ctx, putAppStateMutationMACsQuery+strings.Join(queryParts, ","), values...)
	return err
}

const mutationBatchSize = 400

func (s *SQLStore) PutAppStateMutationMACs(ctx context.Context, name string, version uint64, mutations []store.AppStateMutationMAC) error {
	if len(mutations) == 0 {
		return nil
	}
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for slice := range slices.Chunk(mutations, mutationBatchSize) {
		if err = s.putAppStateMutationMACs(tx, ctx, name, version, slice); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *SQLStore) DeleteAppStateMutationMACs(ctx context.Context, name string, indexMACs [][]byte) error {
	if len(indexMACs) == 0 {
		return nil
	}
	args := make([]any, 3+len(indexMACs))
	args[0] = s.businessId
	args[1] = s.JID
	args[2] = name
	ph := make([]string, len(indexMACs))
	for i := range indexMACs {
		ph[i] = fmt.Sprintf("$%d", i+4)
		args[3+i] = indexMACs[i]
	}
	query := fmt.Sprintf(
		"DELETE FROM whatsmeow_app_state_mutation_macs WHERE business_id=$1 AND jid=$2 AND name=$3 AND index_mac IN (%s)",
		strings.Join(ph, ","),
	)
	_, err := s.dbPool.Exec(ctx, query, args...)
	return err
}

func (s *SQLStore) GetAppStateMutationMAC(ctx context.Context, name string, indexMAC []byte) (valueMAC []byte, err error) {
	err = s.dbPool.QueryRow(ctx, getAppStateMutationMACQuery, s.businessId, s.JID, name, indexMAC).Scan(&valueMAC)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}

const (
	putContactNameQuery = `
		INSERT INTO whatsmeow_contacts (business_id, our_jid, their_jid, first_name, full_name) VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (business_id, our_jid, their_jid) DO UPDATE SET first_name=excluded.first_name, full_name=excluded.full_name
	`
	putManyContactNamesQuery = `
		INSERT INTO whatsmeow_contacts (business_id, our_jid, their_jid, first_name, full_name)
		VALUES (%s)
		ON CONFLICT (business_id, our_jid, their_jid) DO UPDATE SET first_name=excluded.first_name, full_name=excluded.full_name
	`
	putRedactedPhoneQuery = `
		INSERT INTO whatsmeow_contacts (business_id, our_jid, their_jid, redacted_phone)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (business_id, our_jid, their_jid) DO UPDATE SET redacted_phone=excluded.redacted_phone
	`
	putPushNameQuery = `
		INSERT INTO whatsmeow_contacts (business_id, our_jid, their_jid, push_name) VALUES ($1, $2, $3, $4)
		ON CONFLICT (business_id, our_jid, their_jid) DO UPDATE SET push_name=excluded.push_name
	`
	putBusinessNameQuery = `
		INSERT INTO whatsmeow_contacts (business_id, our_jid, their_jid, business_name) VALUES ($1, $2, $3, $4)
		ON CONFLICT (business_id, our_jid, their_jid) DO UPDATE SET business_name=excluded.business_name
	`
	getContactQuery = `
		SELECT first_name, full_name, push_name, business_name, redacted_phone FROM whatsmeow_contacts WHERE business_id=$1 AND our_jid=$2 AND their_jid=$3
	`
	getAllContactsQuery = `
		SELECT their_jid, first_name, full_name, push_name, business_name, redacted_phone FROM whatsmeow_contacts WHERE business_id=$1 AND our_jid=$2
	`
)

func (s *SQLStore) PutPushName(ctx context.Context, user types.JID, pushName string) (bool, string, error) {
	s.contactCacheLock.Lock()
	defer s.contactCacheLock.Unlock()

	cached, err := s.getContact(ctx, user)
	if err != nil {
		return false, "", err
	}
	if cached.PushName != pushName {
		_, err = s.dbPool.Exec(ctx, putPushNameQuery, s.businessId, s.JID, user, pushName)
		if err != nil {
			return false, "", err
		}
		previousName := cached.PushName
		cached.PushName = pushName
		cached.Found = true
		return true, previousName, nil
	}
	return false, "", nil
}

func (s *SQLStore) PutBusinessName(ctx context.Context, user types.JID, businessName string) (bool, string, error) {
	s.contactCacheLock.Lock()
	defer s.contactCacheLock.Unlock()

	cached, err := s.getContact(ctx, user)
	if err != nil {
		return false, "", err
	}
	if cached.BusinessName != businessName {
		_, err = s.dbPool.Exec(ctx, putBusinessNameQuery, s.businessId, s.JID, user, businessName)
		if err != nil {
			return false, "", err
		}
		previousName := cached.BusinessName
		cached.BusinessName = businessName
		cached.Found = true
		return true, previousName, nil
	}
	return false, "", nil
}

func (s *SQLStore) PutContactName(ctx context.Context, user types.JID, firstName, fullName string) error {
	s.contactCacheLock.Lock()
	defer s.contactCacheLock.Unlock()

	cached, err := s.getContact(ctx, user)
	if err != nil {
		return err
	}
	if cached.FirstName != firstName || cached.FullName != fullName {
		_, err = s.dbPool.Exec(ctx, putContactNameQuery, s.businessId, s.JID, user, firstName, fullName)
		if err != nil {
			return err
		}
		cached.FirstName = firstName
		cached.FullName = fullName
		cached.Found = true
	}
	return nil
}

const contactBatchSize = 300

func (s *SQLStore) putContactNamesBatch(tx pgx.Tx, ctx context.Context, contacts []store.ContactEntry) error {
	values := make([]any, 2, 2+len(contacts)*3)
	queryParts := make([]string, 0, len(contacts))
	values[0] = s.businessId
	values[1] = s.JID
	placeholderSyntax := "($1, $2, $%d, $%d, $%d)"
	i := 0
	handledContacts := make(map[types.JID]struct{}, len(contacts))
	for _, contact := range contacts {
		if contact.JID.IsEmpty() {
			s.log.Warnf("Empty contact info in mass insert: %+v", contact)
			continue
		}
		if _, alreadyHandled := handledContacts[contact.JID]; alreadyHandled {
			s.log.Warnf("Duplicate contact info for %s in mass insert", contact.JID)
			continue
		}
		handledContacts[contact.JID] = struct{}{}
		baseIndex := i*3 + 2
		values = append(values, contact.JID.String(), contact.FirstName, contact.FullName)
		queryParts = append(queryParts, fmt.Sprintf(placeholderSyntax, baseIndex+1, baseIndex+2, baseIndex+3))
		i++
	}
	if len(queryParts) == 0 {
		return nil
	}
	_, err := tx.Exec(ctx, fmt.Sprintf(putManyContactNamesQuery, strings.Join(queryParts, ",")), values...)
	return err
}

func (s *SQLStore) PutAllContactNames(ctx context.Context, contacts []store.ContactEntry) error {
	if len(contacts) == 0 {
		return nil
	}
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to start transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for slice := range slices.Chunk(contacts, contactBatchSize) {
		if err = s.putContactNamesBatch(tx, ctx, slice); err != nil {
			return err
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.contactCacheLock.Lock()
	// Just clear the cache, fetching pushnames and business names would be too much effort
	s.contactCache = make(map[types.JID]*types.ContactInfo)
	s.contactCacheLock.Unlock()
	return nil
}

func (s *SQLStore) PutManyRedactedPhones(ctx context.Context, entries []store.RedactedPhoneEntry) error {
	if len(entries) == 0 {
		return nil
	}

	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, entry := range entries {
		if _, err = tx.Exec(ctx, putRedactedPhoneQuery,
			s.businessId,
			s.JID,
			entry.JID.String(),
			entry.RedactedPhone,
		); err != nil {
			return fmt.Errorf("failed to insert redacted phone for %s: %w", entry.JID, err)
		}
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("failed to commit transaction: %w", err)
	}

	s.contactCacheLock.Lock()
	for _, entry := range entries {
		if cached, ok := s.contactCache[entry.JID]; ok && cached.RedactedPhone == entry.RedactedPhone {
			continue
		}
		delete(s.contactCache, entry.JID)
	}
	s.contactCacheLock.Unlock()

	return nil
}

func (s *SQLStore) getContact(ctx context.Context, user types.JID) (*types.ContactInfo, error) {
	cached, ok := s.contactCache[user]
	if ok {
		return cached, nil
	}

	var first, full, push, business, redactedPhone sql.NullString
	err := s.dbPool.QueryRow(ctx, getContactQuery, s.businessId, s.JID, user).Scan(&first, &full, &push, &business, &redactedPhone)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return nil, err
	}
	info := &types.ContactInfo{
		Found:         err == nil,
		FirstName:     first.String,
		FullName:      full.String,
		PushName:      push.String,
		BusinessName:  business.String,
		RedactedPhone: redactedPhone.String,
	}
	s.contactCache[user] = info
	return info, nil
}

func (s *SQLStore) GetContact(ctx context.Context, user types.JID) (types.ContactInfo, error) {
	s.contactCacheLock.Lock()
	info, err := s.getContact(ctx, user)
	s.contactCacheLock.Unlock()
	if err != nil {
		return types.ContactInfo{}, err
	}
	return *info, nil
}

func (s *SQLStore) GetAllContacts(ctx context.Context) (map[types.JID]types.ContactInfo, error) {
	s.contactCacheLock.Lock()
	defer s.contactCacheLock.Unlock()

	rows, err := s.dbPool.Query(ctx, getAllContactsQuery, s.businessId, s.JID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	output := make(map[types.JID]types.ContactInfo, len(s.contactCache))
	for rows.Next() {
		var jid types.JID
		var first, full, push, business, redactedPhone sql.NullString
		err = rows.Scan(&jid, &first, &full, &push, &business, &redactedPhone)
		if err != nil {
			return nil, fmt.Errorf("error scanning row: %w", err)
		}
		info := types.ContactInfo{
			Found:         true,
			FirstName:     first.String,
			FullName:      full.String,
			PushName:      push.String,
			BusinessName:  business.String,
			RedactedPhone: redactedPhone.String,
		}
		output[jid] = info
		s.contactCache[jid] = &info
	}
	return output, rows.Err()
}

const (
	putChatSettingQuery = `
		INSERT INTO whatsmeow_chat_settings (business_id, our_jid, chat_jid, %[1]s) VALUES ($1, $2, $3, $4)
		ON CONFLICT (business_id, our_jid, chat_jid) DO UPDATE SET %[1]s=excluded.%[1]s
	`
	getChatSettingsQuery = `
		SELECT muted_until, pinned, archived FROM whatsmeow_chat_settings WHERE business_id=$1 AND our_jid=$2 AND chat_jid=$3
	`
)

func (s *SQLStore) PutMutedUntil(ctx context.Context, chat types.JID, mutedUntil time.Time) error {
	var val int64
	if mutedUntil == store.MutedForever {
		val = -1
	} else if !mutedUntil.IsZero() {
		val = mutedUntil.Unix()
	}
	_, err := s.dbPool.Exec(ctx, fmt.Sprintf(putChatSettingQuery, "muted_until"), s.businessId, s.JID, chat, val)
	return err
}

func (s *SQLStore) PutPinned(ctx context.Context, chat types.JID, pinned bool) error {
	_, err := s.dbPool.Exec(ctx, fmt.Sprintf(putChatSettingQuery, "pinned"), s.businessId, s.JID, chat, pinned)
	return err
}

func (s *SQLStore) PutArchived(ctx context.Context, chat types.JID, archived bool) error {
	_, err := s.dbPool.Exec(ctx, fmt.Sprintf(putChatSettingQuery, "archived"), s.businessId, s.JID, chat, archived)
	return err
}

func (s *SQLStore) GetChatSettings(ctx context.Context, chat types.JID) (settings types.LocalChatSettings, err error) {
	var mutedUntil int64
	err = s.dbPool.QueryRow(ctx, getChatSettingsQuery, s.businessId, s.JID, chat).Scan(&mutedUntil, &settings.Pinned, &settings.Archived)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	} else if err != nil {
		return
	} else {
		settings.Found = true
	}
	if mutedUntil < 0 {
		settings.MutedUntil = store.MutedForever
	} else if mutedUntil > 0 {
		settings.MutedUntil = time.Unix(mutedUntil, 0)
	}
	return
}

const (
	putMsgSecret = `
		INSERT INTO whatsmeow_message_secrets (business_id, our_jid, chat_jid, sender_jid, message_id, key)
		VALUES ($1, $2, $3, $4, $5, $6)
		ON CONFLICT (business_id, our_jid, chat_jid, sender_jid, message_id) DO NOTHING
	`
	getMsgSecret = `
		SELECT key, sender_jid
		FROM whatsmeow_message_secrets
		WHERE business_id=$1 AND our_jid=$2 AND (chat_jid=$3 OR chat_jid=(
			CASE
				WHEN $3 LIKE '%@lid'
					THEN (SELECT pn || '@s.whatsapp.net' FROM whatsmeow_lid_map WHERE business_id=$1 AND lid=replace($3, '@lid', ''))
				WHEN $3 LIKE '%@s.whatsapp.net'
					THEN (SELECT lid || '@lid' FROM whatsmeow_lid_map WHERE business_id=$1 AND pn=replace($3, '@s.whatsapp.net', ''))
			END
		)) AND message_id=$5 AND (sender_jid=$4 OR sender_jid=(
			CASE
				WHEN $4 LIKE '%@lid'
					THEN (SELECT pn || '@s.whatsapp.net' FROM whatsmeow_lid_map WHERE business_id=$1 AND lid=replace($4, '@lid', ''))
				WHEN $4 LIKE '%@s.whatsapp.net'
					THEN (SELECT lid || '@lid' FROM whatsmeow_lid_map WHERE business_id=$1 AND pn=replace($4, '@s.whatsapp.net', ''))
			END
		))
	`
)

func (s *SQLStore) PutMessageSecrets(ctx context.Context, inserts []store.MessageSecretInsert) (err error) {
	if len(inserts) == 0 {
		return nil
	}
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()

	for _, insert := range inserts {
		if _, err = tx.Exec(ctx, putMsgSecret, s.businessId, s.JID, insert.Chat.ToNonAD(), insert.Sender.ToNonAD(), insert.ID, insert.Secret); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func (s *SQLStore) PutMessageSecret(ctx context.Context, chat, sender types.JID, id types.MessageID, secret []byte) (err error) {
	_, err = s.dbPool.Exec(ctx, putMsgSecret, s.businessId, s.JID, chat.ToNonAD(), sender.ToNonAD(), id, secret)
	return
}

func (s *SQLStore) GetMessageSecret(ctx context.Context, chat, sender types.JID, id types.MessageID) (secret []byte, realSender types.JID, err error) {
	err = s.dbPool.QueryRow(ctx, getMsgSecret, s.businessId, s.JID, chat.ToNonAD(), sender.ToNonAD(), id).Scan(&secret, &realSender)
	if errors.Is(err, pgx.ErrNoRows) {
		err = nil
	}
	return
}

const (
	putPrivacyTokens = `
		INSERT INTO whatsmeow_privacy_tokens (business_id, our_jid, their_jid, token, timestamp)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (business_id, our_jid, their_jid) DO UPDATE SET token=EXCLUDED.token, timestamp=EXCLUDED.timestamp
	`
	getPrivacyToken = `
		SELECT token, timestamp FROM whatsmeow_privacy_tokens WHERE business_id=$1 AND our_jid=$2 AND (their_jid=$3 OR their_jid=(
			CASE
				WHEN $3 LIKE '%@lid'
					THEN (SELECT pn || '@s.whatsapp.net' FROM whatsmeow_lid_map WHERE business_id=$1 AND lid=replace($3, '@lid', ''))
				WHEN $3 LIKE '%@s.whatsapp.net'
					THEN (SELECT lid || '@lid' FROM whatsmeow_lid_map WHERE business_id=$1 AND pn=replace($3, '@s.whatsapp.net', ''))
				ELSE $3
			END
		))
		ORDER BY timestamp DESC LIMIT 1
	`
)

func (s *SQLStore) PutPrivacyTokens(ctx context.Context, tokens ...store.PrivacyToken) error {
	if len(tokens) == 0 {
		return nil
	}
	args := make([]any, 2+len(tokens)*3)
	placeholders := make([]string, len(tokens))
	args[0] = s.businessId
	args[1] = s.JID
	for i, token := range tokens {
		args[i*3+2] = token.User.ToNonAD().String()
		args[i*3+3] = token.Token
		args[i*3+4] = token.Timestamp.Unix()
		placeholders[i] = fmt.Sprintf("($1, $2, $%d, $%d, $%d)", i*3+3, i*3+4, i*3+5)
	}
	query := strings.ReplaceAll(putPrivacyTokens, "($1, $2, $3, $4, $5)", strings.Join(placeholders, ","))
	_, err := s.dbPool.Exec(ctx, query, args...)
	return err
}

func (s *SQLStore) GetPrivacyToken(ctx context.Context, user types.JID) (*store.PrivacyToken, error) {
	var token store.PrivacyToken
	token.User = user.ToNonAD()
	var ts int64
	err := s.dbPool.QueryRow(ctx, getPrivacyToken, s.businessId, s.JID, token.User).Scan(&token.Token, &ts)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	} else {
		token.Timestamp = time.Unix(ts, 0)
		return &token, nil
	}
}

const (
	getBufferedEventQuery = `
		SELECT plaintext, server_timestamp, insert_timestamp FROM whatsmeow_event_buffer WHERE business_id=$1 AND our_jid=$2 AND ciphertext_hash=$3
	`
	putBufferedEventQuery = `
		INSERT INTO whatsmeow_event_buffer (business_id, our_jid, ciphertext_hash, plaintext, server_timestamp, insert_timestamp)
		VALUES ($1, $2, $3, $4, $5, $6)
	`
	clearBufferedEventPlaintextQuery = `
		UPDATE whatsmeow_event_buffer SET plaintext = NULL WHERE business_id=$1 AND our_jid=$2 AND ciphertext_hash=$3
	`
	deleteOldBufferedHashesQuery = `
		DELETE FROM whatsmeow_event_buffer WHERE business_id=$1 AND insert_timestamp < $2
	`
)

func (s *SQLStore) GetBufferedEvent(ctx context.Context, ciphertextHash [32]byte) (*store.BufferedEvent, error) {
	var insertTimeMS, serverTimeSeconds int64
	var buf store.BufferedEvent
	err := s.dbPool.QueryRow(ctx, getBufferedEventQuery, s.businessId, s.JID, ciphertextHash[:]).Scan(&buf.Plaintext, &serverTimeSeconds, &insertTimeMS)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	buf.ServerTime = time.Unix(serverTimeSeconds, 0)
	buf.InsertTime = time.UnixMilli(insertTimeMS)
	return &buf, nil
}

func (s *SQLStore) PutBufferedEvent(ctx context.Context, ciphertextHash [32]byte, plaintext []byte, serverTimestamp time.Time) error {
	_, err := s.dbPool.Exec(ctx, putBufferedEventQuery, s.businessId, s.JID, ciphertextHash[:], plaintext, serverTimestamp.Unix(), time.Now().UnixMilli())
	return err
}

func (s *SQLStore) DoDecryptionTxn(ctx context.Context, fn func(context.Context) error) error {
	tx, err := s.dbPool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("failed to begin transaction: %w", err)
	}
	defer func() { _ = tx.Rollback(ctx) }()
	if err = fn(ctx); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s *SQLStore) ClearBufferedEventPlaintext(ctx context.Context, ciphertextHash [32]byte) error {
	_, err := s.dbPool.Exec(ctx, clearBufferedEventPlaintextQuery, s.businessId, s.JID, ciphertextHash[:])
	return err
}

func (s *SQLStore) DeleteOldBufferedHashes(ctx context.Context) error {
	// The WhatsApp servers only buffer events for 14 days,
	// so we can safely delete anything older than that.
	_, err := s.dbPool.Exec(ctx, deleteOldBufferedHashesQuery, s.businessId, time.Now().Add(-14*24*time.Hour).UnixMilli())
	return err
}

const (
	getOutgoingEventQuery = `
		SELECT format, plaintext FROM whatsmeow_retry_buffer
		WHERE business_id=$1 AND our_jid=$2 AND (chat_jid=$3 OR chat_jid=$4) AND message_id=$5
	`
	addOutgoingEventQuery = `
		INSERT INTO whatsmeow_retry_buffer (business_id, our_jid, chat_jid, message_id, format, plaintext, timestamp)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (business_id, our_jid, chat_jid, message_id) DO UPDATE
			SET format=excluded.format, plaintext=excluded.plaintext, timestamp=excluded.timestamp
	`
	deleteOldOutgoingEventsQuery = `
		DELETE FROM whatsmeow_retry_buffer WHERE business_id=$1 AND our_jid=$2 AND timestamp < $3
	`
)

func (s *SQLStore) GetOutgoingEvent(ctx context.Context, chatJID, altChatJID types.JID, id types.MessageID) (format string, result []byte, err error) {
	err = s.dbPool.QueryRow(ctx, getOutgoingEventQuery, s.businessId, s.JID, chatJID, altChatJID, id).Scan(&format, &result)
	return
}

func (s *SQLStore) AddOutgoingEvent(ctx context.Context, chatJID types.JID, id types.MessageID, format string, plaintext []byte) error {
	_, err := s.dbPool.Exec(ctx, addOutgoingEventQuery, s.businessId, s.JID, chatJID, id, format, plaintext, time.Now().UnixMilli())
	return err
}

func (s *SQLStore) DeleteOldOutgoingEvents(ctx context.Context) error {
	_, err := s.dbPool.Exec(ctx, deleteOldOutgoingEventsQuery, s.businessId, s.JID, time.Now().Add(-7*24*time.Hour).UnixMilli())
	return err
}
