// Package main demonstrates the minimal cosfaction setup.
//
// This example configures a single faction with named disposition tiers,
// applies a delta, and reads the resulting disposition and tier back.
// No faction relations are configured — this is the simplest possible
// use of the engine.
//
// Run with:
//
//	go run ./examples/basic
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
			{
				ID:          "guild",
				Name:        "Merchant Guild",
				Type:        faction.FactionTypeNeutral,
				Description: "The governing body of interplanetary trade.",
			},
		},

		Tiers: []faction.Tier{
			{Name: "Hostile", MinValue: -1000, MaxValue: -501},
			{Name: "Unfriendly", MinValue: -500, MaxValue: -101},
			{Name: "Neutral", MinValue: -100, MaxValue: 100},
			{Name: "Friendly", MinValue: 101, MaxValue: 500},
			{Name: "Honored", MinValue: 501, MaxValue: 1000},
		},

		Store: newMemoryStore(),
	})
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}

	player := faction.EntityID("player-001")

	fmt.Println("=== cosfaction basic example ===")
	fmt.Println()

	// Read initial disposition — zero, Neutral tier.
	printDisposition(ctx, engine, player, "guild", "Initial state")

	// Complete a trade quest — small positive delta.
	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  player,
		FactionID: "guild",
		Amount:    150,
		Reason:    "Completed trade route contract",
		Source:    "trade_quest",
	})
	printDisposition(ctx, engine, player, "guild", "After trade quest (+150)")

	// Pay a fine — negative delta, drops back toward Neutral.
	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  player,
		FactionID: "guild",
		Amount:    -200,
		Reason:    "Fined for unlicensed trading",
		Source:    "enforcement",
	})
	printDisposition(ctx, engine, player, "guild", "After fine (-200)")

	// Complete a major contract — large positive delta, crosses into Honored.
	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  player,
		FactionID: "guild",
		Amount:    600,
		Reason:    "Secured exclusive trade agreement",
		Source:    "major_contract",
	})
	printDisposition(ctx, engine, player, "guild", "After major contract (+600)")

	fmt.Println()
	fmt.Println("Tier reference:")
	for _, value := range []int64{-800, -300, 0, 300, 800} {
		tier := engine.TierFor(value)
		fmt.Printf("  %+5d → %s\n", value, tier.Name)
	}
}

func printDisposition(
	ctx context.Context,
	engine *faction.Engine,
	entityID faction.EntityID,
	factionID faction.FactionID,
	label string,
) {
	d, err := engine.GetDisposition(ctx, entityID, factionID)
	if err != nil {
		log.Printf("GetDisposition error: %v", err)
		return
	}
	fmt.Printf("%s\n  value=%d  tier=%s\n\n", label, d.Value, d.Tier.Name)
}

// memoryStore is an in-memory Store for the basic example.
// Production use should replace this with adapters/postgres.
type memoryStore struct {
	mu           sync.Mutex
	dispositions map[string]faction.StoredDisposition
	history      []faction.DispositionRecord
}

func newMemoryStore() *memoryStore {
	return &memoryStore{
		dispositions: make(map[string]faction.StoredDisposition),
	}
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
	s.dispositions[s.key(e, f)] = faction.StoredDisposition{
		FactionID: f,
		Value:     v,
		UpdatedAt: time.Now(),
	}
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
