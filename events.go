package faction

import "time"

// EventType identifies the kind of disposition event that occurred.
type EventType string

const (
	// EventDispositionChanged fires whenever an entity's disposition
	// with any faction changes by any amount.
	EventDispositionChanged EventType = "disposition.changed"

	// EventTierCrossed fires when a disposition change moves an entity
	// from one named tier to another. This is the most significant event
	// for downstream game systems to react to.
	EventTierCrossed EventType = "disposition.tier_crossed"

	// EventDispositionDecayed fires when the decay service reduces an
	// entity's disposition toward the configured decay target.
	EventDispositionDecayed EventType = "disposition.decayed"

	// EventPropagationApplied fires when a higher order Taylor expansion
	// term applies an indirect disposition effect to a faction. Subscribers
	// can use this to trace the full political ripple of a player action.
	EventPropagationApplied EventType = "disposition.propagation_applied"
)

// Event represents a faction disposition occurrence emitted by the engine.
// All events share this shape regardless of type.
type Event struct {
	// Type identifies which kind of event this is.
	Type EventType

	// EntityID identifies the entity whose disposition changed.
	EntityID EntityID

	// FactionID identifies the faction whose disposition changed.
	FactionID FactionID

	// PreviousValue is the disposition value before this change.
	PreviousValue int64

	// NewValue is the disposition value after this change.
	NewValue int64

	// PreviousTier is the named tier before this change.
	// Empty if the entity had no prior disposition with this faction.
	PreviousTier string

	// NewTier is the named tier after this change.
	NewTier string

	// TierChanged indicates whether this event crossed a tier boundary.
	TierChanged bool

	// PropagationOrder is the Taylor expansion order that produced this
	// event. Zero for direct changes. One or higher for propagated effects.
	PropagationOrder int

	// Reason is the human readable reason for this disposition change,
	// carried from the originating Delta.
	Reason string

	// Source is the game action that produced this change,
	// carried from the originating Delta.
	Source string

	// OccurredAt is the timestamp when this event was produced.
	OccurredAt time.Time
}

// Publisher is implemented by the caller to receive disposition events
// from the engine. The engine calls Publish for every disposition change
// including propagated higher order effects.
//
// Implementations should be non-blocking. If delivery cannot be guaranteed
// synchronously, implementations should buffer internally.
type Publisher interface {
	Publish(event Event) error
}
