package faction

import (
	"context"
	"time"
)

// StoredDisposition is the raw disposition value and its last write timestamp
// as returned by the Store. The engine uses the timestamp to calculate lazy
// decay — the elapsed time since last_updated determines how much decay to
// apply before returning or acting on the value.
type StoredDisposition struct {
	// FactionID identifies which faction this disposition belongs to.
	// Required when returning multiple dispositions from GetAllDispositions.
	FactionID FactionID

	// Value is the stored disposition score.
	Value int64

	// UpdatedAt is when this value was last written to the store.
	// Used by the engine to compute elapsed time for lazy decay.
	UpdatedAt time.Time
}

// Store is implemented by the caller to provide persistent disposition
// storage. The engine treats the Store as the authoritative source of
// truth for all disposition values.
//
// Implementations must be safe for concurrent use.
type Store interface {
	// GetDisposition retrieves the current stored disposition for an entity
	// with a specific faction. Returns a zero StoredDisposition and no error
	// if no record exists yet for this entity-faction pair.
	GetDisposition(ctx context.Context, entityID EntityID, factionID FactionID) (StoredDisposition, error)

	// SetDisposition writes a disposition value for an entity with a
	// specific faction. Creates the record if it does not exist.
	// The store must record the current time as UpdatedAt.
	SetDisposition(ctx context.Context, entityID EntityID, factionID FactionID, value int64) error

	// GetAllDispositions retrieves all stored faction dispositions for an
	// entity. Returns an empty slice and no error if the entity has none.
	GetAllDispositions(ctx context.Context, entityID EntityID) ([]StoredDisposition, error)

	// GetDispositionHistory retrieves the N most recent disposition changes
	// for an entity with a specific faction, ordered newest first.
	// Useful for audit trails and player-facing history displays.
	GetDispositionHistory(ctx context.Context, entityID EntityID, factionID FactionID, limit int) ([]DispositionRecord, error)

	// RecordDispositionChange appends a disposition change record for
	// audit and history purposes. Called by the engine after every change.
	RecordDispositionChange(ctx context.Context, record DispositionRecord) error
}

// Cache is optionally implemented by the caller to provide a fast read
// layer in front of the Store. The engine checks the Cache before the
// Store on reads and writes through on changes.
//
// If no Cache is configured the engine reads and writes directly to
// the Store for all operations.
//
// Implementations must be safe for concurrent use.
type Cache interface {
	// GetDisposition retrieves a cached stored disposition.
	// Returns the value, whether it was found, and any error.
	GetDisposition(ctx context.Context, entityID EntityID, factionID FactionID) (StoredDisposition, bool, error)

	// SetDisposition writes a stored disposition to the cache.
	SetDisposition(ctx context.Context, entityID EntityID, factionID FactionID, stored StoredDisposition) error

	// Invalidate removes a cached disposition value, forcing the next
	// read to fall through to the Store.
	Invalidate(ctx context.Context, entityID EntityID, factionID FactionID) error

	// InvalidateAll removes all cached dispositions for an entity.
	InvalidateAll(ctx context.Context, entityID EntityID) error
}

// DispositionRecord represents a single historical disposition change
// stored for audit and history purposes.
type DispositionRecord struct {
	// EntityID identifies the entity whose disposition changed.
	EntityID EntityID

	// FactionID identifies the faction whose disposition changed.
	FactionID FactionID

	// PreviousValue is the disposition value before this change.
	PreviousValue int64

	// NewValue is the disposition value after this change.
	NewValue int64

	// Delta is the signed change amount.
	Delta int64

	// PropagationOrder is the Taylor expansion order that produced
	// this change. Zero for direct changes.
	PropagationOrder int

	// Reason is the human readable reason for this change.
	Reason string

	// Source is the originating game action.
	Source string

	// OccurredAt is when this change was applied.
	OccurredAt int64 // Unix timestamp
}
