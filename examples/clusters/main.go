// Package clusters demonstrates cosfaction configured for a science fiction
// MMO with two competing authorities: the Interstellar Authority Republic (IAR)
// and the Union — a resistance movement operating outside IAR jurisdiction.
//
// This example shows how Higher-Order Taylor Expansion naturally produces
// the political tension of a two-faction world: gaining standing with the
// IAR costs Union standing automatically, and vice versa, without any
// special casing. The faction relationship graph handles it.
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	faction "github.com/d1sco/cosfaction"
)

// Faction IDs for the Clusters game world.
const (
	FactionIAR   faction.FactionID = "iar"
	FactionUnion faction.FactionID = "union"
)

func main() {
	ctx := context.Background()

	// Configure the faction engine with IAR and Union as opposing factions.
	// The relationship weight of -0.8 means gaining 100 IAR disposition
	// automatically costs 80 Union disposition through second order
	// Taylor expansion — and vice versa.
	engine, err := faction.New(faction.Config{
		Factions: []faction.Faction{
			{
				ID:          FactionIAR,
				Name:        "Interstellar Authority Republic",
				Type:        faction.FactionTypeGoverning,
				Description: "The governing authority responsible for enforcement across all chartered settlements.",
			},
			{
				ID:          FactionUnion,
				Name:        "The Union",
				Type:        faction.FactionTypeResistance,
				Description: "A resistance movement operating outside IAR jurisdiction, distributing guerilla items.",
			},
		},

		// Six named disposition tiers from most hostile to most trusted.
		// These map directly to enforcement probability modifiers in Clusters.
		Tiers: []faction.Tier{
			{Name: "Outlawed", MinValue: -1000, MaxValue: -601},
			{Name: "Wanted", MinValue: -600, MaxValue: -201},
			{Name: "Suspected", MinValue: -200, MaxValue: -1},
			{Name: "Neutral", MinValue: 0, MaxValue: 299},
			{Name: "Trusted", MinValue: 300, MaxValue: 699},
			{Name: "Celebrated", MinValue: 700, MaxValue: 1000},
		},

		// IAR and Union are directly opposed.
		// Gaining standing with either costs standing with the other
		// through second order Taylor expansion.
		//
		// The -0.8 weight means 80% of any disposition gain with one faction
		// becomes a loss with the opposing faction after factorial damping.
		Relations: []faction.Relation{
			{FactionA: FactionIAR, FactionB: FactionUnion, Influence: -0.8},
			{FactionA: FactionUnion, FactionB: FactionIAR, Influence: -0.8},
		},

		Store: newMemoryStore(),

		Publisher: &loggingPublisher{},

		Taylor: faction.TaylorConfig{
			MaxOrder:  3,
			Threshold: 0.5,
		},
	})
	if err != nil {
		log.Fatalf("failed to create faction engine: %v", err)
	}

	smuggler := faction.EntityID("smuggler-char-001")

	fmt.Println("=== Clusters Faction Disposition Engine ===")
	fmt.Println()

	// Scenario 1: Smuggler successfully moves Union weapons.
	// Direct effect: Union disposition +100 (always exact delta).
	// Second order effect via Option B normalization (range=2000):
	//   delta_norm = 100/2000 = 0.05
	//   T_2 = (0.05^2 / 2!) × -0.8 × 2000 = -2.0
	// Routine action — higher order effects are intentionally small.
	// A major event (delta=1000) would produce T_2 = -200.
	fmt.Println("--- Scenario 1: Smuggler moves Union weapons (routine action) ---")
	result, err := engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  smuggler,
		FactionID: FactionUnion,
		Amount:    100,
		Reason:    "Successfully delivered guerilla weapons to Union contact",
		Source:    "smuggler_delivery_quest",
	})
	if err != nil {
		log.Fatalf("failed to apply delta: %v", err)
	}
	printResult(result)
	printDispositions(ctx, engine, smuggler)

	fmt.Println()

	// Scenario 2: Smuggler fails an IAR inspection.
	// Direct effect: IAR disposition decreases.
	// Second order effect: Union disposition increases slightly.
	// The Union reads IAR enforcement as a signal of the Smuggler's loyalty.
	fmt.Println("--- Scenario 2: Smuggler fails IAR inspection ---")
	result, err = engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  smuggler,
		FactionID: FactionIAR,
		Amount:    -150,
		Reason:    "Contraband seized during spaceport inspection",
		Source:    "inspection_failed",
	})
	if err != nil {
		log.Fatalf("failed to apply delta: %v", err)
	}
	printResult(result)
	printDispositions(ctx, engine, smuggler)

	fmt.Println()

	// Scenario 3: Smuggler completes IAR sanctioned bounty contract.
	// delta=200, delta_norm=0.1
	// T_2 = (0.1^2 / 2!) × -0.8 × 2000 = -8.0
	// Still a small higher order effect — this is a significant but
	// not world-changing action.
	fmt.Println("--- Scenario 3: Smuggler completes IAR bounty contract ---")
	result, err = engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  smuggler,
		FactionID: FactionIAR,
		Amount:    200,
		Reason:    "Delivered wanted Union operative to IAR custody",
		Source:    "bounty_contract_completed",
	})
	if err != nil {
		log.Fatalf("failed to apply delta: %v", err)
	}
	printResult(result)
	printDispositions(ctx, engine, smuggler)

	fmt.Println()

	// Scenario 4: Politician switches city alignment from IAR to Union.
	// This is a major world event — delta=800, delta_norm=0.4
	// T_2 = (0.4^2 / 2!) × -0.8 × 2000 = -128
	// T_3 = (0.4^3 / 3!) × 0.64 × 2000 = +13.65
	// Major events produce genuine political ripples across the graph.
	// The entire faction landscape reacts to this.
	fmt.Println("--- Scenario 4: Politician switches city to Union alignment (major event) ---")
	politician := faction.EntityID("politician-char-001")
	result, err = engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  politician,
		FactionID: FactionUnion,
		Amount:    800,
		Reason:    "City of New Meridian formally aligned with the Union",
		Source:    "city_alignment_changed",
	})
	if err != nil {
		log.Fatalf("failed to apply delta: %v", err)
	}
	printResult(result)
	printDispositions(ctx, engine, politician)
}

func printResult(result faction.PropagationResult) {
	fmt.Printf("Taylor expansion: %d orders computed, converged=%v\n",
		result.OrdersComputed, result.Converged)
	for _, order := range result.Orders {
		fmt.Printf("  Order %d → %s: %.0f\n", order.Order, order.FactionID, order.Effect)
	}
}

func printDispositions(ctx context.Context, engine *faction.Engine, entityID faction.EntityID) {
	for _, factionID := range []faction.FactionID{FactionIAR, FactionUnion} {
		d, err := engine.GetDisposition(ctx, entityID, factionID)
		if err != nil {
			log.Printf("error reading disposition: %v", err)
			continue
		}
		fmt.Printf("  %s: %d (%s)\n", factionID, d.Value, d.Tier.Name)
	}
}

// loggingPublisher prints disposition events to stdout.
// In production this would publish to NATS or Kafka.
type loggingPublisher struct{}

func (p *loggingPublisher) Publish(event faction.Event) error {
	if event.Type == faction.EventTierCrossed {
		fmt.Printf("  [TIER CROSSED] %s → %s/%s: %s → %s\n",
			event.EntityID, event.FactionID, event.Type,
			event.PreviousTier, event.NewTier)
	}
	return nil
}

// memoryStore is an in-memory Store implementation for the example.
// Production use should replace this with the PostgreSQL adapter.
type memoryStore struct {
	dispositions map[string]faction.StoredDisposition
	history      []faction.DispositionRecord
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		dispositions: make(map[string]faction.StoredDisposition),
	}
}

func (s *memoryStore) key(entityID faction.EntityID, factionID faction.FactionID) string {
	return string(entityID) + "/" + string(factionID)
}

func (s *memoryStore) GetDisposition(_ context.Context, entityID faction.EntityID, factionID faction.FactionID) (faction.StoredDisposition, error) {
	return s.dispositions[s.key(entityID, factionID)], nil
}

func (s *memoryStore) SetDisposition(_ context.Context, entityID faction.EntityID, factionID faction.FactionID, value int64) error {
	s.dispositions[s.key(entityID, factionID)] = faction.StoredDisposition{
		FactionID: factionID,
		Value:     value,
		UpdatedAt: time.Now(),
	}
	return nil
}

func (s *memoryStore) GetAllDispositions(_ context.Context, entityID faction.EntityID) ([]faction.StoredDisposition, error) {
	var result []faction.StoredDisposition
	prefix := string(entityID) + "/"
	for k, v := range s.dispositions {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			result = append(result, v)
		}
	}
	return result, nil
}

func (s *memoryStore) GetDispositionHistory(_ context.Context, _ faction.EntityID, _ faction.FactionID, limit int) ([]faction.DispositionRecord, error) {
	if limit > len(s.history) {
		limit = len(s.history)
	}
	return s.history[len(s.history)-limit:], nil
}

func (s *memoryStore) RecordDispositionChange(_ context.Context, record faction.DispositionRecord) error {
	s.history = append(s.history, record)
	return nil
}
