// Package main demonstrates cosfaction with a complex multi-faction graph.
//
// This example models a three-faction political landscape:
//
//	IAR (governing authority)
//	Union (resistance movement)
//	Merchant Guild (neutral trade body, allied with IAR)
//
// The faction relationship graph:
//
//	IAR   ←→ Union         influence: -0.8  (strong opposition)
//	IAR   →  Guild         influence: +0.6  (IAR endorses Guild)
//	Union →  Guild         influence: -0.4  (Union distrusts Guild/IAR alliance)
//
// Actions with one faction ripple through the graph via Taylor expansion.
// Gaining IAR standing costs Union standing and boosts Guild standing.
//
// Run with:
//
//	go run ./examples/multi_faction
package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	faction "github.com/cosfaction/cosfaction"
)

func main() {
	ctx := context.Background()

	engine, err := faction.New(faction.Config{
		Factions: []faction.Faction{
			{ID: "iar",   Name: "Interstellar Authority Republic", Type: faction.FactionTypeGoverning},
			{ID: "union", Name: "The Union",                       Type: faction.FactionTypeResistance},
			{ID: "guild", Name: "Merchant Guild",                   Type: faction.FactionTypeNeutral},
		},
		Tiers: []faction.Tier{
			{Name: "Hostile", MinValue: -1000, MaxValue: -601},
			{Name: "Unfriendly", MinValue: -600,  MaxValue: -201},
			{Name: "Neutral", MinValue: -200,  MaxValue: 199},
			{Name: "Friendly", MinValue: 200,   MaxValue: 599},
			{Name: "Honored", MinValue: 600,   MaxValue: 1000},
		},
		Relations: []faction.Relation{
			// IAR and Union are strongly opposed.
			{FactionA: "iar",   FactionB: "union", Influence: -0.8},
			{FactionA: "union", FactionB: "iar",   Influence: -0.8},
			// IAR endorses the Merchant Guild.
			{FactionA: "iar",   FactionB: "guild", Influence: +0.6},
			// Union distrusts the Guild because of its IAR alignment.
			{FactionA: "union", FactionB: "guild", Influence: -0.4},
		},
		Store: newMemoryStore(),
		Taylor: faction.TaylorConfig{
			MaxOrder:  3,
			Threshold: 0.5,
		},
	})
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}

	bountyHunter := faction.EntityID("bounty-hunter-001")

	fmt.Println("=== cosfaction multi-faction example ===")
	fmt.Println()
	fmt.Println("Faction graph:")
	fmt.Println("  IAR   ←→ Union  influence: -0.8  (strong opposition)")
	fmt.Println("  IAR    →  Guild  influence: +0.6  (IAR endorses Guild)")
	fmt.Println("  Union  →  Guild  influence: -0.4  (Union distrusts Guild)")
	fmt.Println()

	printAllDispositions(ctx, engine, bountyHunter, "Initial state")

	// Scenario 1: Bounty Hunter completes an IAR contract.
	// Direct: IAR +500 (major action)
	// Second order: Union -200 (IAR × -0.8), Guild +150 (IAR × +0.6)
	// Third order: further ripples
	fmt.Println("--- Scenario 1: Completes major IAR bounty contract (+500 IAR) ---")
	result, err := engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  bountyHunter,
		FactionID: "iar",
		Amount:    500,
		Reason:    "Delivered high-value Union operative to IAR custody",
		Source:    "bounty_contract",
	})
	if err != nil {
		log.Fatalf("ApplyDelta failed: %v", err)
	}
	printExpansion(result)
	printAllDispositions(ctx, engine, bountyHunter, "After IAR contract")

	// Scenario 2: Bounty Hunter does a favor for the Union.
	// Direct: Union +300
	// Second order: IAR -120 (Union × -0.8), Guild -60 (Union × -0.4)
	fmt.Println("--- Scenario 2: Helps the Union (+300 Union) ---")
	result, err = engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  bountyHunter,
		FactionID: "union",
		Amount:    300,
		Reason:    "Provided intelligence about IAR patrol routes",
		Source:    "union_contract",
	})
	if err != nil {
		log.Fatalf("ApplyDelta failed: %v", err)
	}
	printExpansion(result)
	printAllDispositions(ctx, engine, bountyHunter, "After Union favor")

	// Scenario 3: Bounty Hunter trades legitimately with the Guild.
	// Direct: Guild +400
	// No relations FROM guild in this graph — no propagation.
	fmt.Println("--- Scenario 3: Legitimate Guild trade (+400 Guild) ---")
	result, err = engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  bountyHunter,
		FactionID: "guild",
		Amount:    400,
		Reason:    "Secured exclusive trade license",
		Source:    "guild_trade",
	})
	if err != nil {
		log.Fatalf("ApplyDelta failed: %v", err)
	}
	printExpansion(result)
	printAllDispositions(ctx, engine, bountyHunter, "After Guild trade")
}

func printExpansion(result faction.PropagationResult) {
	fmt.Printf("  Taylor expansion: %d orders, converged=%v\n",
		result.OrdersComputed, result.Converged)
	for _, o := range result.Orders {
		fmt.Printf("  Order %d → %-6s %+.0f\n", o.Order, o.FactionID, o.Effect)
	}
	fmt.Println()
}

func printAllDispositions(
	ctx context.Context,
	engine *faction.Engine,
	entityID faction.EntityID,
	label string,
) {
	fmt.Printf("%s:\n", label)
	for _, fid := range []faction.FactionID{"iar", "union", "guild"} {
		d, err := engine.GetDisposition(ctx, entityID, fid)
		if err != nil {
			log.Printf("GetDisposition error: %v", err)
			continue
		}
		fmt.Printf("  %-6s  %+5d  (%s)\n", fid, d.Value, d.Tier.Name)
	}
	fmt.Println()
}

type memoryStore struct {
	mu           sync.Mutex
	dispositions map[string]faction.StoredDisposition
	history      []faction.DispositionRecord
}

func newMemoryStore() *memoryStore {
	return &memoryStore{dispositions: make(map[string]faction.StoredDisposition)}
}

func (s *memoryStore) key(e faction.EntityID, f faction.FactionID) string {
	return string(e) + "/" + string(f)
}

func (s *memoryStore) GetDisposition(_ context.Context, e faction.EntityID, f faction.FactionID) (faction.StoredDisposition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dispositions[s.key(e, f)], nil
}

func (s *memoryStore) SetDisposition(_ context.Context, e faction.EntityID, f faction.FactionID, v int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dispositions[s.key(e, f)] = faction.StoredDisposition{FactionID: f, Value: v, UpdatedAt: time.Now()}
	return nil
}

func (s *memoryStore) GetAllDispositions(_ context.Context, e faction.EntityID) ([]faction.StoredDisposition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	prefix := string(e) + "/"
	var result []faction.StoredDisposition
	for k, v := range s.dispositions {
		if len(k) > len(prefix) && k[:len(prefix)] == prefix {
			result = append(result, v)
		}
	}
	return result, nil
}

func (s *memoryStore) GetDispositionHistory(_ context.Context, _ faction.EntityID, _ faction.FactionID, limit int) ([]faction.DispositionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit > len(s.history) {
		limit = len(s.history)
	}
	return s.history[len(s.history)-limit:], nil
}

func (s *memoryStore) RecordDispositionChange(_ context.Context, r faction.DispositionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, r)
	return nil
}
