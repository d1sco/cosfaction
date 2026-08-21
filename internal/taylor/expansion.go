// Package taylor implements Higher-Order Taylor Expansion for faction
// disposition propagation across weighted directed relationship graphs.
//
// A disposition delta is first normalized against the full disposition range
// to produce a dimensionless value δ_n ∈ (-1, 1). The nth order term for
// faction f is then:
//
//	T_n(f) = (δ_n^n / n!) × Σ R(f0,g1) × R(g1,g2) × ... × R(g(n-1),f) × range
//
// where δ_n = delta/range, R(a,b) is the influence weight from faction a to b,
// and range is the full disposition range derived from the tier configuration.
//
// Normalization ensures δ_n^n shrinks geometrically with order, combining with
// factorial damping to double-guarantee convergence. This produces politically
// meaningful behavior: routine actions (small delta relative to range) produce
// negligible higher order effects, while major events (large delta relative to
// range) generate genuine ripples across the faction graph.
//
// The expansion terminates when either MaxOrder is reached or all terms at
// an order fall below the convergence Threshold.
package taylor

import (
	"math"
)

// Term represents a single contribution to the Taylor expansion at a
// specific order for a specific faction in the relationship graph.
type Term struct {
	// Order is the Taylor expansion order of this term.
	// Order 1 is the direct (first order) effect.
	// Orders 2 and above are indirect (higher order) effects.
	Order int

	// FactionID is the faction receiving this disposition effect.
	FactionID string

	// RawEffect is the disposition change before factorial damping.
	RawEffect float64

	// DampedEffect is the disposition change after applying 1/n! damping.
	// This is the value actually applied to the faction's disposition.
	DampedEffect float64

	// PathFactors records the relationship weights traversed to reach
	// this term. Useful for debugging and explaining why a faction
	// received an indirect effect.
	PathFactors []float64
}

// Expansion is the full result of computing a Taylor series for a
// disposition delta across a faction relationship graph.
type Expansion struct {
	// SourceFaction is the faction the original delta was applied to.
	SourceFaction string

	// OriginalDelta is the raw delta before expansion.
	OriginalDelta float64

	// Terms contains all computed terms of the expansion, including
	// the direct first order term and all higher order indirect terms.
	Terms []Term

	// Converged is true if the expansion terminated because the nth
	// order term fell below the threshold rather than reaching MaxOrder.
	Converged bool

	// OrdersComputed is the number of orders evaluated before termination.
	OrdersComputed int
}

// Graph provides the relationship data the expansion traverses.
// It is implemented by the internal graph package.
type Graph interface {
	// RelationsFrom returns all outgoing relationships from a faction,
	// as (targetFactionID, influence) pairs.
	RelationsFrom(factionID string) []Relation
}

// Relation represents a single directed relationship between two factions.
type Relation struct {
	TargetFactionID string
	Influence       float64
}

// Config controls the behaviour of the Taylor expansion.
type Config struct {
	// MaxOrder is the maximum number of Taylor expansion orders to compute.
	// Higher values capture more distant indirect effects at greater
	// computational cost. Typical values are 2 to 5.
	// Default: 3
	MaxOrder int

	// Threshold is the minimum DampedEffect magnitude below which
	// expansion terminates early. Effects smaller than this value are
	// considered politically negligible.
	// Default: 0.5
	Threshold float64

	// DispositionRange is the full span of the disposition scale, derived
	// from the tier configuration as (maxTierValue - minTierValue).
	// For example, tiers spanning -1000 to +1000 produce a range of 2000.
	//
	// The delta is normalized against this range before being raised to
	// the nth power, ensuring delta^n shrinks geometrically with order
	// rather than growing for deltas greater than 1.
	//
	// The engine sets this automatically from the configured tiers.
	// If zero, normalization is skipped and the expansion degrades to
	// the non-normalized form (delta^1 throughout).
	DispositionRange float64
}

// DefaultConfig returns a Config suitable for most games.
// DispositionRange must be set from the tier configuration before use.
func DefaultConfig() Config {
	return Config{
		MaxOrder:  3,
		Threshold: 0.5,
	}
}

// Expand computes the Higher-Order Taylor Expansion of a disposition delta
// originating at sourceFaction and propagating through the relationship graph.
//
// The delta is normalized against DispositionRange before being raised to
// the nth power. The nth order term for faction f is:
//
//	T_n(f) = (δ_n^n / n!) × Σ R(f0,g1) × R(g1,g2) × ... × R(g(n-1),f) × range
//
// where δ_n = delta/range and R(a,b) is the influence weight from a to b.
//
// Normalization guarantees |δ_n| < 1 for any game-legal delta, so δ_n^n
// shrinks geometrically with order. Combined with factorial damping this
// produces double-guaranteed convergence: routine actions generate negligible
// higher order effects, major events produce genuine political ripples.
//
// If DispositionRange is zero normalization is skipped and the expansion
// uses delta^1 throughout (backward compatible behavior).
func Expand(graph Graph, sourceFaction string, delta float64, cfg Config) Expansion {
	if cfg.MaxOrder <= 0 {
		cfg.MaxOrder = DefaultConfig().MaxOrder
	}
	if cfg.Threshold <= 0 {
		cfg.Threshold = DefaultConfig().Threshold
	}

	expansion := Expansion{
		SourceFaction: sourceFaction,
		OriginalDelta: delta,
	}

	// Normalize delta against the disposition range.
	// deltaNorm is dimensionless and satisfies |deltaNorm| <= 1
	// for any delta within the configured tier bounds.
	// If range is not configured fall back to delta^1 behavior.
	deltaNorm := delta
	normalized := cfg.DispositionRange > 0
	if normalized {
		deltaNorm = delta / cfg.DispositionRange
	}

	// First order — direct effect on the source faction itself.
	// delta^1 / 1! × range = delta exactly. First order is always
	// the full unmodified delta regardless of normalization.
	firstTerm := Term{
		Order:        1,
		FactionID:    sourceFaction,
		RawEffect:    delta,
		DampedEffect: delta,
		PathFactors:  []float64{1.0},
	}
	expansion.Terms = append(expansion.Terms, firstTerm)
	expansion.OrdersComputed = 1

	// Track paths as (factionID, accumulatedProduct) pairs.
	type pathState struct {
		factionID string
		product   float64
		factors   []float64
	}

	// Seed the path frontier with direct relations from the source faction.
	frontier := []pathState{}
	for _, rel := range graph.RelationsFrom(sourceFaction) {
		if rel.Influence == 0 {
			continue
		}
		frontier = append(frontier, pathState{
			factionID: rel.TargetFactionID,
			product:   rel.Influence,
			factors:   []float64{rel.Influence},
		})
	}

	// Carry deltaNorm^n and n! forward across orders to avoid
	// recomputing from scratch at each iteration.
	deltaPower := deltaNorm // deltaNorm^1 at order 1, updated each order
	factorial := 1.0       // 1! at order 1, updated each order

	// Compute higher order terms by extending paths one hop per order.
	for order := 2; order <= cfg.MaxOrder; order++ {
		// Advance the carried values for this order.
		// deltaPower = deltaNorm^order, factorial = order!
		deltaPower *= deltaNorm
		factorial *= float64(order)

		// dampingCoeff = deltaNorm^n / n!
		// Multiplied by range at the end to restore original scale.
		dampingCoeff := deltaPower / factorial

		orderConverged := true
		nextFrontier := []pathState{}

		// Aggregate effects by faction at this order.
		// Multiple paths of length n may reach the same faction.
		effects := map[string]*Term{}

		for _, path := range frontier {
			// Raw effect is the path product of relationship weights.
			// Damped effect rescales by dampingCoeff and range.
			rawEffect := path.product
			var dampedEffect float64
			if normalized {
				dampedEffect = rawEffect * dampingCoeff * cfg.DispositionRange
			} else {
				dampedEffect = rawEffect * delta / factorial
			}

			if math.Abs(dampedEffect) < cfg.Threshold {
				continue
			}

			orderConverged = false

			if existing, ok := effects[path.factionID]; ok {
				existing.RawEffect += rawEffect
				existing.DampedEffect += dampedEffect
			} else {
				factorsCopy := make([]float64, len(path.factors))
				copy(factorsCopy, path.factors)
				effects[path.factionID] = &Term{
					Order:        order,
					FactionID:    path.factionID,
					RawEffect:    rawEffect,
					DampedEffect: dampedEffect,
					PathFactors:  factorsCopy,
				}
			}

			// Extend paths to next order.
			for _, rel := range graph.RelationsFrom(path.factionID) {
				if rel.Influence == 0 {
					continue
				}
				newFactors := make([]float64, len(path.factors)+1)
				copy(newFactors, path.factors)
				newFactors[len(path.factors)] = rel.Influence

				nextFrontier = append(nextFrontier, pathState{
					factionID: rel.TargetFactionID,
					product:   path.product * rel.Influence,
					factors:   newFactors,
				})
			}
		}

		for _, term := range effects {
			expansion.Terms = append(expansion.Terms, *term)
		}

		expansion.OrdersComputed = order
		frontier = nextFrontier

		if orderConverged || len(frontier) == 0 {
			expansion.Converged = true
			break
		}
	}

	return expansion
}
