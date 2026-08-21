// Package faction provides a multi-axis faction disposition engine for
// multiplayer games. Disposition changes propagate across competing factions
// using Higher-Order Taylor Expansion on a weighted directed relationship
// graph, with mathematically guaranteed convergence.
package faction

import "time"

// EntityID identifies any entity whose faction dispositions are tracked.
// This is typically a player character but may be any game entity.
type EntityID string

// FactionID uniquely identifies a faction within the engine.
type FactionID string

// FactionType classifies the nature of a faction within the game world.
type FactionType string

const (
	// FactionTypeGoverning represents an authority faction such as a
	// government, empire, or regulatory body.
	FactionTypeGoverning FactionType = "governing"

	// FactionTypeResistance represents an opposition faction operating
	// against a governing authority.
	FactionTypeResistance FactionType = "resistance"

	// FactionTypeNeutral represents a faction with no inherent political
	// alignment toward other factions.
	FactionTypeNeutral FactionType = "neutral"

	// FactionTypePlayer represents a player-controlled faction such as
	// a guild or player city.
	FactionTypePlayer FactionType = "player"
)

// Faction defines a faction within the game world.
type Faction struct {
	// ID is the unique identifier for this faction.
	ID FactionID

	// Name is the human readable name of this faction.
	Name string

	// Type classifies the nature of this faction.
	Type FactionType

	// Description provides context about this faction for documentation
	// and tooling purposes.
	Description string
}

// Tier defines a named disposition threshold band.
//
// Tiers are ordered automatically by MinValue — you do not need to assign
// or maintain a rank. Use EvenTiers or WeightedTiers to construct a tier
// set without calculating boundaries manually.
type Tier struct {
	// Name is the human readable label for this disposition tier.
	// Examples: "Celebrated", "Trusted", "Neutral", "Suspected", "Outlawed"
	Name string

	// MinValue is the inclusive lower bound of this tier.
	MinValue int64

	// MaxValue is the inclusive upper bound of this tier.
	MaxValue int64

	// rank is assigned by the engine after sorting tiers by MinValue.
	// It is not set by the caller.
	rank int
}

// Relation defines the weighted directed relationship between two factions.
// Influence determines how disposition changes with FactionA propagate
// to FactionB through the Taylor expansion.
type Relation struct {
	// FactionA is the source faction in this relationship.
	FactionA FactionID

	// FactionB is the target faction that receives propagated disposition
	// changes originating from FactionA.
	FactionB FactionID

	// Influence is the relationship weight in the range [-1.0, 1.0].
	// Negative values indicate opposition: gaining standing with FactionA
	// costs standing with FactionB proportionally.
	// Positive values indicate alliance: gaining standing with FactionA
	// also gains standing with FactionB proportionally.
	// Zero indicates no relationship between these factions.
	Influence float64
}

// Disposition represents an entity's current standing with a single faction.
type Disposition struct {
	// EntityID identifies the entity this disposition belongs to.
	EntityID EntityID

	// FactionID identifies the faction this disposition is with.
	FactionID FactionID

	// Value is the raw numeric disposition score.
	Value int64

	// Tier is the named tier this value currently falls within.
	Tier Tier

	// UpdatedAt is the timestamp of the most recent disposition change.
	UpdatedAt time.Time
}

// Delta represents a requested change to an entity's disposition
// with a specific faction.
type Delta struct {
	// EntityID identifies the entity whose disposition is changing.
	EntityID EntityID

	// FactionID identifies the faction this delta is applied to directly.
	// Higher order effects propagate to related factions automatically.
	FactionID FactionID

	// Amount is the signed disposition change. Positive values increase
	// standing. Negative values decrease standing.
	Amount float64

	// Reason is a human readable description of why this delta occurred.
	// Used for audit logging and player notifications.
	Reason string

	// Source optionally identifies the game action that produced this delta.
	// Examples: "quest_completed", "inspection_failed", "combat_victory"
	Source string
}

// PropagationOrder represents the computed disposition effect at a single
// order of the Taylor expansion for a single faction.
type PropagationOrder struct {
	// Order is the Taylor expansion order at which this effect was computed.
	// Order 1 is the direct effect. Higher orders are indirect effects.
	Order int

	// FactionID is the faction receiving this propagated effect.
	FactionID FactionID

	// Effect is the computed disposition change at this order.
	// Magnitude decreases with order due to factorial damping.
	Effect float64
}

// PropagationResult contains the full Taylor expansion result for a
// disposition delta, including all computed orders and convergence metadata.
type PropagationResult struct {
	// SourceFaction is the faction the original delta was applied to.
	SourceFaction FactionID

	// OriginalDelta is the delta amount before expansion.
	OriginalDelta float64

	// Orders contains all computed propagation effects ordered by
	// Taylor expansion order.
	Orders []PropagationOrder

	// Converged indicates whether the expansion reached the threshold
	// cutoff before MaxOrder was reached.
	Converged bool

	// OrdersComputed is the number of Taylor orders that were evaluated.
	OrdersComputed int
}
