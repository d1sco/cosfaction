package faction

import (
	"context"
	"fmt"
	"math"
	"sort"
	"time"

	"github.com/d1sco/cosfaction/internal/graph"
	"github.com/d1sco/cosfaction/internal/taylor"
)

// Config defines the complete configuration for a faction engine instance.
// All fields except Factions, Tiers, and Store are optional.
type Config struct {
	// Factions defines all factions tracked by this engine.
	// At least one faction is required.
	Factions []Faction

	// Tiers defines the named disposition threshold bands.
	// Tiers must be contiguous and cover all possible disposition values.
	// At least one tier is required.
	Tiers []Tier

	// Relations defines the weighted directed relationships between factions.
	// Factions with no defined relations do not receive propagated effects.
	Relations []Relation

	// Store is the persistent disposition storage implementation.
	// Required.
	Store Store

	// Cache is the optional fast read layer in front of the Store.
	// If nil the engine reads and writes directly to the Store.
	Cache Cache

	// Publisher is the optional event emission implementation.
	// If nil disposition events are not published.
	Publisher Publisher

	// Taylor controls the Higher-Order Taylor Expansion behaviour.
	// If zero values are provided the expansion uses sensible defaults.
	Taylor TaylorConfig

	// Decay controls automatic disposition decay toward a neutral state.
	// If Enabled is false no decay is applied.
	Decay DecayConfig
}

// TaylorConfig controls the Higher-Order Taylor Expansion used to propagate
// disposition changes across the faction relationship graph.
type TaylorConfig struct {
	// MaxOrder is the maximum number of Taylor expansion orders to compute.
	// Typical values are 2 to 5. Higher values capture more distant
	// indirect political effects at greater computational cost.
	// Default: 3
	MaxOrder int

	// Threshold is the minimum damped effect magnitude below which
	// expansion terminates early. Effects smaller than this are considered
	// politically negligible and are not applied.
	// Default: 0.5
	Threshold float64

	// DispositionRange is derived automatically from the configured tiers
	// as (maxTierValue - minTierValue). It is used to normalize the delta
	// before raising it to the nth power in the Taylor expansion, ensuring
	// convergence is guaranteed by both delta^n shrinkage and factorial
	// damping simultaneously.
	//
	// This field is set by the engine and should not be set manually.
	DispositionRange float64
}

// DecayConfig controls automatic disposition decay.
type DecayConfig struct {
	// Enabled activates automatic disposition decay.
	Enabled bool

	// RatePerHour is the absolute disposition points lost per real-world hour.
	RatePerHour float64

	// Target controls what value disposition decays toward.
	// "neutral" decays toward the midpoint of the neutral tier.
	// "zero" decays toward zero.
	// "minimum" decays toward the minimum possible value.
	Target string
}

// Engine is the entry point for all faction disposition operations.
// Create one Engine per game server instance using New.
//
// Engine is safe for concurrent use.
type Engine struct {
	factions  map[FactionID]Faction
	tiers     []Tier
	graph     *graph.Graph
	store     Store
	cache     Cache
	publisher Publisher
	taylor    taylor.Config
	decay     DecayConfig
}

// New constructs and validates a faction Engine from the provided Config.
// Returns an error if the configuration is invalid.
func New(cfg Config) (*Engine, error) {
	if len(cfg.Factions) == 0 {
		return nil, fmt.Errorf("cosfaction: at least one faction is required")
	}
	if len(cfg.Tiers) == 0 {
		return nil, fmt.Errorf("cosfaction: at least one tier is required")
	}
	if cfg.Store == nil {
		return nil, fmt.Errorf("cosfaction: Store is required")
	}

	factionMap := make(map[FactionID]Faction, len(cfg.Factions))
	factionIDs := make([]string, 0, len(cfg.Factions))
	for _, f := range cfg.Factions {
		if f.ID == "" {
			return nil, fmt.Errorf("cosfaction: faction ID must not be empty")
		}
		factionMap[f.ID] = f
		factionIDs = append(factionIDs, string(f.ID))
	}

	graphRelations := make([]graph.Relation, 0, len(cfg.Relations))
	for _, r := range cfg.Relations {
		graphRelations = append(graphRelations, graph.Relation{
			FactionA:  string(r.FactionA),
			FactionB:  string(r.FactionB),
			Influence: r.Influence,
		})
	}

	g, err := graph.New(factionIDs, graphRelations)
	if err != nil {
		return nil, fmt.Errorf("cosfaction: invalid relations: %w", err)
	}

	// Sort tiers by MinValue, derive rank, and validate for gaps and overlaps.
	sortedTiers, err := prepareTiers(cfg.Tiers)
	if err != nil {
		return nil, err
	}

	// Derive the disposition range from the tier configuration.
	// This is the full span of the disposition scale used to normalize
	// delta before raising it to the nth power in the Taylor expansion.
	dispRange := dispositionRangeFromTiers(sortedTiers)

	taylorCfg := taylor.Config{
		MaxOrder:         cfg.Taylor.MaxOrder,
		Threshold:        cfg.Taylor.Threshold,
		DispositionRange: dispRange,
	}
	if taylorCfg.MaxOrder <= 0 {
		taylorCfg.MaxOrder = 3
	}
	if taylorCfg.Threshold <= 0 {
		taylorCfg.Threshold = 0.5
	}

	return &Engine{
		factions:  factionMap,
		tiers:     sortedTiers,
		graph:     g,
		store:     cfg.Store,
		cache:     cfg.Cache,
		publisher: cfg.Publisher,
		taylor:    taylorCfg,
		decay:     cfg.Decay,
	}, nil
}

// ApplyDelta applies a disposition delta to an entity's standing with a
// specific faction and propagates indirect effects to related factions
// using Higher-Order Taylor Expansion.
//
// The expansion computes up to MaxOrder indirect effects, terminating
// early when all remaining terms fall below the Threshold. Factorial
// damping (1/n!) guarantees convergence of the series.
//
// All disposition changes — direct and propagated — are written to the
// Store and published as events if a Publisher is configured.
func (e *Engine) ApplyDelta(ctx context.Context, delta Delta) (PropagationResult, error) {
	if _, ok := e.factions[delta.FactionID]; !ok {
		return PropagationResult{}, fmt.Errorf(
			"cosfaction: unknown faction %q", delta.FactionID,
		)
	}

	// Build a graph adapter that satisfies taylor.Graph.
	graphAdapter := &graphAdapter{g: e.graph}

	// Compute the full Taylor expansion.
	expansion := taylor.Expand(
		graphAdapter,
		string(delta.FactionID),
		delta.Amount,
		e.taylor,
	)

	result := PropagationResult{
		SourceFaction:  delta.FactionID,
		OriginalDelta:  delta.Amount,
		Converged:      expansion.Converged,
		OrdersComputed: expansion.OrdersComputed,
	}

	// Apply each term of the expansion to the relevant faction.
	for _, term := range expansion.Terms {
		factionID := FactionID(term.FactionID)
		effect := int64(math.Round(term.DampedEffect))
		if effect == 0 {
			continue
		}

		prev, err := e.getDisposition(ctx, delta.EntityID, factionID)
		if err != nil {
			return PropagationResult{}, fmt.Errorf(
				"cosfaction: reading disposition for %s/%s: %w",
				delta.EntityID, factionID, err,
			)
		}

		next := prev + effect
		next = e.clamp(next)

		err = e.setDisposition(ctx, delta.EntityID, factionID, next)
		if err != nil {
			return PropagationResult{}, fmt.Errorf(
				"cosfaction: writing disposition for %s/%s: %w",
				delta.EntityID, factionID, err,
			)
		}

		prevTier := e.tierFor(prev)
		nextTier := e.tierFor(next)

		record := DispositionRecord{
			EntityID:         delta.EntityID,
			FactionID:        factionID,
			PreviousValue:    prev,
			NewValue:         next,
			Delta:            effect,
			PropagationOrder: term.Order,
			Reason:           delta.Reason,
			Source:           delta.Source,
			OccurredAt:       time.Now().Unix(),
		}

		if err := e.store.RecordDispositionChange(ctx, record); err != nil {
			return PropagationResult{}, fmt.Errorf(
				"cosfaction: recording disposition change: %w", err,
			)
		}

		if e.publisher != nil {
			event := Event{
				Type:             EventDispositionChanged,
				EntityID:         delta.EntityID,
				FactionID:        factionID,
				PreviousValue:    prev,
				NewValue:         next,
				PreviousTier:     prevTier.Name,
				NewTier:          nextTier.Name,
				TierChanged:      prevTier.rank != nextTier.rank,
				PropagationOrder: term.Order,
				Reason:           delta.Reason,
				Source:           delta.Source,
				OccurredAt:       time.Now(),
			}

			if err := e.publisher.Publish(event); err != nil {
				return PropagationResult{}, fmt.Errorf(
					"cosfaction: publishing event: %w", err,
				)
			}

			if prevTier.rank != nextTier.rank {
				tierEvent := event
				tierEvent.Type = EventTierCrossed
				if err := e.publisher.Publish(tierEvent); err != nil {
					return PropagationResult{}, fmt.Errorf(
						"cosfaction: publishing tier crossed event: %w", err,
					)
				}
			}
		}

		result.Orders = append(result.Orders, PropagationOrder{
			Order:     term.Order,
			FactionID: factionID,
			Effect:    float64(effect),
		})
	}

	return result, nil
}

// GetDisposition retrieves the current disposition and tier for an entity
// with a specific faction.
func (e *Engine) GetDisposition(ctx context.Context, entityID EntityID, factionID FactionID) (Disposition, error) {
	if _, ok := e.factions[factionID]; !ok {
		return Disposition{}, fmt.Errorf("cosfaction: unknown faction %q", factionID)
	}

	value, err := e.getDisposition(ctx, entityID, factionID)
	if err != nil {
		return Disposition{}, err
	}

	return Disposition{
		EntityID:  entityID,
		FactionID: factionID,
		Value:     value,
		Tier:      e.tierFor(value),
		UpdatedAt: time.Now(),
	}, nil
}

// GetAllDispositions retrieves the current disposition with every faction
// for the given entity. Lazy decay is applied to each value.
func (e *Engine) GetAllDispositions(ctx context.Context, entityID EntityID) ([]Disposition, error) {
	records, err := e.store.GetAllDispositions(ctx, entityID)
	if err != nil {
		return nil, fmt.Errorf("cosfaction: reading all dispositions: %w", err)
	}

	dispositions := make([]Disposition, 0, len(records))
	for _, r := range records {
		decayed := e.applyDecay(r.Value, r.UpdatedAt)
		dispositions = append(dispositions, Disposition{
			EntityID:  entityID,
			FactionID: r.FactionID,
			Value:     decayed,
			Tier:      e.tierFor(decayed),
			UpdatedAt: r.UpdatedAt,
		})
	}

	return dispositions, nil
}

// TierFor returns the named tier that a given raw disposition value falls within.
func (e *Engine) TierFor(value int64) Tier {
	return e.tierFor(value)
}

// getDisposition reads from cache if available, falling back to the store,
// and applies lazy decay based on elapsed time since the value was last written.
func (e *Engine) getDisposition(ctx context.Context, entityID EntityID, factionID FactionID) (int64, error) {
	var stored StoredDisposition

	if e.cache != nil {
		s, found, err := e.cache.GetDisposition(ctx, entityID, factionID)
		if err != nil {
			return 0, err
		}
		if found {
			stored = s
		}
	}

	if stored.UpdatedAt.IsZero() {
		s, err := e.store.GetDisposition(ctx, entityID, factionID)
		if err != nil {
			return 0, err
		}
		stored = s
	}

	return e.applyDecay(stored.Value, stored.UpdatedAt), nil
}

// setDisposition writes to the store and updates the cache.
func (e *Engine) setDisposition(ctx context.Context, entityID EntityID, factionID FactionID, value int64) error {
	if err := e.store.SetDisposition(ctx, entityID, factionID, value); err != nil {
		return err
	}

	if e.cache != nil {
		stored := StoredDisposition{
			Value:     value,
			UpdatedAt: time.Now(),
		}
		return e.cache.SetDisposition(ctx, entityID, factionID, stored)
	}

	return nil
}

// tierFor finds the tier a disposition value falls within.
// Returns the lowest tier if the value is below all defined tiers
// and the highest tier if it is above all defined tiers.
func (e *Engine) tierFor(value int64) Tier {
	if len(e.tiers) == 0 {
		return Tier{}
	}

	for _, t := range e.tiers {
		if value >= t.MinValue && value <= t.MaxValue {
			return t
		}
	}

	// Below all tiers — return lowest.
	lowest := e.tiers[0]
	for _, t := range e.tiers {
		if t.rank < lowest.rank {
			lowest = t
		}
	}

	// Above all tiers — return highest.
	if value > lowest.MaxValue {
		highest := e.tiers[0]
		for _, t := range e.tiers {
			if t.rank > highest.rank {
				highest = t
			}
		}
		return highest
	}

	return lowest
}

// clamp keeps disposition values within the bounds of the configured tiers.
func (e *Engine) clamp(value int64) int64 {
	if len(e.tiers) == 0 {
		return value
	}

	min := e.tiers[0].MinValue
	max := e.tiers[0].MaxValue
	for _, t := range e.tiers {
		if t.MinValue < min {
			min = t.MinValue
		}
		if t.MaxValue > max {
			max = t.MaxValue
		}
	}

	if value < min {
		return min
	}
	if value > max {
		return max
	}
	return value
}

// applyDecay computes the lazily decayed disposition value based on elapsed
// real time since the value was last written. If decay is disabled or the
// UpdatedAt timestamp is zero no decay is applied.
//
// Decay moves the value toward the configured target (neutral or zero) at
// RatePerHour disposition points per real-world hour. The value is clamped
// so it never crosses the decay target.
func (e *Engine) applyDecay(value int64, updatedAt time.Time) int64 {
	if !e.decay.Enabled || updatedAt.IsZero() || e.decay.RatePerHour <= 0 {
		return value
	}

	elapsed := time.Since(updatedAt).Hours()
	if elapsed <= 0 {
		return value
	}

	decayAmount := int64(e.decay.RatePerHour * elapsed)
	if decayAmount == 0 {
		return value
	}

	switch e.decay.Target {
	case "neutral":
		neutral := e.neutralValue()
		if value > neutral {
			result := value - decayAmount
			if result < neutral {
				return neutral
			}
			return result
		}
		if value < neutral {
			result := value + decayAmount
			if result > neutral {
				return neutral
			}
			return result
		}
		return value

	case "zero":
		if value > 0 {
			result := value - decayAmount
			if result < 0 {
				return 0
			}
			return result
		}
		if value < 0 {
			result := value + decayAmount
			if result > 0 {
				return 0
			}
			return result
		}
		return value

	default:
		return value
	}
}

// neutralValue returns the midpoint of the neutral tier if one exists,
// or zero if no neutral tier is configured.
func (e *Engine) neutralValue() int64 {
	for _, t := range e.tiers {
		if t.Name == "Neutral" || t.Name == "neutral" {
			return (t.MinValue + t.MaxValue) / 2
		}
	}
	return 0
}

// prepareTiers sorts tiers by MinValue, assigns derived rank values, and
// validates that tiers are contiguous with no gaps or overlaps.
func prepareTiers(tiers []Tier) ([]Tier, error) {
	if len(tiers) == 0 {
		return nil, nil
	}

	// Sort by MinValue so rank assignment is deterministic regardless
	// of the order the caller provided tiers.
	sorted := make([]Tier, len(tiers))
	copy(sorted, tiers)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].MinValue < sorted[j].MinValue
	})

	// Assign rank based on sorted position.
	for i := range sorted {
		sorted[i].rank = i
	}

	// Validate contiguity — each tier must start exactly where the previous
	// one ended. Gaps and overlaps both indicate misconfiguration.
	for i := 1; i < len(sorted); i++ {
		expected := sorted[i-1].MaxValue + 1
		if sorted[i].MinValue != expected {
			if sorted[i].MinValue > expected {
				return nil, fmt.Errorf(
					"cosfaction: gap in tiers between %q (max %d) and %q (min %d): values %d..%d are uncovered",
					sorted[i-1].Name, sorted[i-1].MaxValue,
					sorted[i].Name, sorted[i].MinValue,
					expected, sorted[i].MinValue-1,
				)
			}
			return nil, fmt.Errorf(
				"cosfaction: overlap in tiers between %q (max %d) and %q (min %d)",
				sorted[i-1].Name, sorted[i-1].MaxValue,
				sorted[i].Name, sorted[i].MinValue,
			)
		}
	}

	return sorted, nil
}

// dispositionRangeFromTiers derives the full disposition scale span from
// the configured tiers as (maxTierValue - minTierValue).
func dispositionRangeFromTiers(tiers []Tier) float64 {
	if len(tiers) == 0 {
		return 0
	}
	min := tiers[0].MinValue
	max := tiers[0].MaxValue
	for _, t := range tiers {
		if t.MinValue < min {
			min = t.MinValue
		}
		if t.MaxValue > max {
			max = t.MaxValue
		}
	}
	return float64(max - min)
}

// graphAdapter wraps the internal graph to satisfy the taylor.Graph interface.
type graphAdapter struct {
	g *graph.Graph
}

func (a *graphAdapter) RelationsFrom(factionID string) []taylor.Relation {
	edges := a.g.RelationsFrom(factionID)
	relations := make([]taylor.Relation, len(edges))
	for i, e := range edges {
		relations[i] = taylor.Relation{
			TargetFactionID: e.TargetFactionID,
			Influence:       e.Influence,
		}
	}
	return relations
}
