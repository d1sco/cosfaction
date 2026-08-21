// Package postgres provides a PostgreSQL implementation of the faction.Store
// interface for use with the cosfaction disposition engine.
//
// The caller is responsible for establishing and managing the pgxpool.Pool.
// This adapter performs no connection management of its own.
//
// Usage:
//
//	pool, err := pgxpool.New(ctx, os.Getenv("DATABASE_URL"))
//	if err != nil {
//	    log.Fatal(err)
//	}
//
//	// Run once at startup to create required tables.
//	if err := postgres.Migrate(ctx, pool); err != nil {
//	    log.Fatal(err)
//	}
//
//	store := postgres.New(pool)
//
//	engine, err := faction.New(faction.Config{
//	    Store: store,
//	    // ...
//	})
package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	faction "github.com/d1sco/cosfaction"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// Store implements faction.Store against a PostgreSQL database.
// All methods are safe for concurrent use.
type Store struct {
	db *pgxpool.Pool
}

// New constructs a Store from an existing pgxpool.Pool.
// Call Migrate before using the store to ensure required tables exist.
func New(db *pgxpool.Pool) *Store {
	return &Store{db: db}
}

// GetDisposition retrieves the stored disposition value and timestamp for
// an entity with a specific faction. Returns a zero StoredDisposition and
// no error if no record exists yet.
func (s *Store) GetDisposition(
	ctx context.Context,
	entityID faction.EntityID,
	factionID faction.FactionID,
) (faction.StoredDisposition, error) {
	const q = `
		SELECT value, updated_at
		FROM faction_dispositions
		WHERE entity_id = $1 AND faction_id = $2
	`

	var value int64
	var updatedAt time.Time

	err := s.db.QueryRow(ctx, q, string(entityID), string(factionID)).
		Scan(&value, &updatedAt)

	if errors.Is(err, pgx.ErrNoRows) {
		return faction.StoredDisposition{
			FactionID: factionID,
			Value:     0,
			UpdatedAt: time.Time{},
		}, nil
	}
	if err != nil {
		return faction.StoredDisposition{}, fmt.Errorf(
			"cosfaction postgres: GetDisposition %s/%s: %w",
			entityID, factionID, err,
		)
	}

	return faction.StoredDisposition{
		FactionID: factionID,
		Value:     value,
		UpdatedAt: updatedAt,
	}, nil
}

// SetDisposition writes a disposition value for an entity with a specific
// faction. Creates the record if it does not exist. UpdatedAt is set to
// the current time by the database.
func (s *Store) SetDisposition(
	ctx context.Context,
	entityID faction.EntityID,
	factionID faction.FactionID,
	value int64,
) error {
	const q = `
		INSERT INTO faction_dispositions (entity_id, faction_id, value, updated_at)
		VALUES ($1, $2, $3, NOW())
		ON CONFLICT (entity_id, faction_id)
		DO UPDATE SET
			value      = EXCLUDED.value,
			updated_at = NOW()
	`

	_, err := s.db.Exec(ctx, q, string(entityID), string(factionID), value)
	if err != nil {
		return fmt.Errorf(
			"cosfaction postgres: SetDisposition %s/%s: %w",
			entityID, factionID, err,
		)
	}
	return nil
}

// GetAllDispositions retrieves all stored faction dispositions for an entity.
// Returns an empty slice and no error if the entity has no dispositions.
func (s *Store) GetAllDispositions(
	ctx context.Context,
	entityID faction.EntityID,
) ([]faction.StoredDisposition, error) {
	const q = `
		SELECT faction_id, value, updated_at
		FROM faction_dispositions
		WHERE entity_id = $1
		ORDER BY faction_id
	`

	rows, err := s.db.Query(ctx, q, string(entityID))
	if err != nil {
		return nil, fmt.Errorf(
			"cosfaction postgres: GetAllDispositions %s: %w",
			entityID, err,
		)
	}
	defer rows.Close()

	var result []faction.StoredDisposition
	for rows.Next() {
		var factionID string
		var value int64
		var updatedAt time.Time

		if err := rows.Scan(&factionID, &value, &updatedAt); err != nil {
			return nil, fmt.Errorf(
				"cosfaction postgres: GetAllDispositions scan %s: %w",
				entityID, err,
			)
		}

		result = append(result, faction.StoredDisposition{
			FactionID: faction.FactionID(factionID),
			Value:     value,
			UpdatedAt: updatedAt,
		})
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"cosfaction postgres: GetAllDispositions rows %s: %w",
			entityID, err,
		)
	}

	return result, nil
}

// GetDispositionHistory retrieves the N most recent disposition changes for
// an entity with a specific faction, ordered newest first.
func (s *Store) GetDispositionHistory(
	ctx context.Context,
	entityID faction.EntityID,
	factionID faction.FactionID,
	limit int,
) ([]faction.DispositionRecord, error) {
	const q = `
		SELECT
			entity_id,
			faction_id,
			previous_value,
			new_value,
			delta,
			propagation_order,
			reason,
			source,
			occurred_at
		FROM faction_disposition_history
		WHERE entity_id = $1 AND faction_id = $2
		ORDER BY occurred_at DESC
		LIMIT $3
	`

	rows, err := s.db.Query(ctx, q, string(entityID), string(factionID), limit)
	if err != nil {
		return nil, fmt.Errorf(
			"cosfaction postgres: GetDispositionHistory %s/%s: %w",
			entityID, factionID, err,
		)
	}
	defer rows.Close()

	var result []faction.DispositionRecord
	for rows.Next() {
		var r faction.DispositionRecord
		var eid, fid string
		var reason, source *string

		if err := rows.Scan(
			&eid,
			&fid,
			&r.PreviousValue,
			&r.NewValue,
			&r.Delta,
			&r.PropagationOrder,
			&reason,
			&source,
			&r.OccurredAt,
		); err != nil {
			return nil, fmt.Errorf(
				"cosfaction postgres: GetDispositionHistory scan %s/%s: %w",
				entityID, factionID, err,
			)
		}

		r.EntityID = faction.EntityID(eid)
		r.FactionID = faction.FactionID(fid)
		if reason != nil {
			r.Reason = *reason
		}
		if source != nil {
			r.Source = *source
		}

		result = append(result, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf(
			"cosfaction postgres: GetDispositionHistory rows %s/%s: %w",
			entityID, factionID, err,
		)
	}

	return result, nil
}

// RecordDispositionChange appends a disposition change to the history table.
// Called by the engine after every direct and propagated disposition change.
func (s *Store) RecordDispositionChange(
	ctx context.Context,
	record faction.DispositionRecord,
) error {
	const q = `
		INSERT INTO faction_disposition_history (
			entity_id,
			faction_id,
			previous_value,
			new_value,
			delta,
			propagation_order,
			reason,
			source,
			occurred_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, err := s.db.Exec(ctx, q,
		string(record.EntityID),
		string(record.FactionID),
		record.PreviousValue,
		record.NewValue,
		record.Delta,
		record.PropagationOrder,
		nullableString(record.Reason),
		nullableString(record.Source),
		record.OccurredAt,
	)
	if err != nil {
		return fmt.Errorf(
			"cosfaction postgres: RecordDispositionChange %s/%s: %w",
			record.EntityID, record.FactionID, err,
		)
	}
	return nil
}

// nullableString converts an empty string to nil for nullable TEXT columns.
func nullableString(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
