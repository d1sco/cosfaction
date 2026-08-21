// Package main demonstrates lazy disposition decay in cosfaction.
//
// Decay is evaluated at read time rather than by a background service.
// When GetDisposition is called, the engine computes how much time has
// elapsed since the value was last written and applies the configured
// decay rate before returning.
//
// This example simulates a character whose IAR standing decays toward
// neutral over 48 hours of in-game time without logging in.
//
// Run with:
//
//	go run ./examples/decay
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
	store := newMemoryStore()

	engine, err := faction.New(faction.Config{
		Factions: []faction.Faction{
			{ID: "iar", Name: "Interstellar Authority Republic", Type: faction.FactionTypeGoverning},
		},
		Tiers: []faction.Tier{
			{Name: "Outlawed", MinValue: -1000, MaxValue: -601},
			{Name: "Wanted", MinValue: -600,  MaxValue: -201},
			{Name: "Suspected", MinValue: -200,  MaxValue: -1},
			{Name: "Neutral", MinValue: 0,     MaxValue: 299},
			{Name: "Trusted", MinValue: 300,   MaxValue: 699},
			{Name: "Celebrated", MinValue: 700,   MaxValue: 1000},
		},
		Store: store,
		Decay: faction.DecayConfig{
			Enabled:     true,
			RatePerHour: 20,    // 20 disposition points per real-world hour
			Target:      "neutral",
		},
	})
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}

	smuggler := faction.EntityID("smuggler-001")

	fmt.Println("=== cosfaction decay example ===")
	fmt.Println()
	fmt.Println("Decay config: 20 points/hour toward neutral")
	fmt.Println()

	// Seed the store with a high IAR standing and a backdated timestamp.
	// This simulates the character having earned 800 IAR disposition
	// and then going offline for varying amounts of time.
	fmt.Println("Character earned 800 IAR disposition (Celebrated)")
	fmt.Println()

	snapshots := []struct {
		hoursAgo int
		label    string
	}{
		{0,  "Now (just earned)"},
		{6,  "6 hours later"},
		{12, "12 hours later"},
		{24, "24 hours later"},
		{36, "36 hours later"},
		{48, "48 hours later"},
	}

	for _, snap := range snapshots {
		// Seed the store with the backdated timestamp for this snapshot.
		store.seed(smuggler, faction.StoredDisposition{
			FactionID: "iar",
			Value:     800,
			UpdatedAt: time.Now().Add(-time.Duration(snap.hoursAgo) * time.Hour),
		})

		d, err := engine.GetDisposition(ctx, smuggler, "iar")
		if err != nil {
			log.Fatalf("GetDisposition failed: %v", err)
		}

		expected := int64(800 - (20 * snap.hoursAgo))
		neutral := int64(149) // midpoint of Neutral tier (0+299)/2
		if expected < neutral {
			expected = neutral
		}

		fmt.Printf("%-25s  value=%-5d  tier=%-10s  (expected ~%d)\n",
			snap.label, d.Value, d.Tier.Name, expected)
	}

	fmt.Println()
	fmt.Println("Decay stops at neutral midpoint (149) — never crosses to negative.")
	fmt.Println()

	// Demonstrate decay from below neutral toward neutral.
	fmt.Println("--- Decay from negative standing toward neutral ---")
	fmt.Println()

	negSnapshots := []struct {
		hoursAgo int
		label    string
	}{
		{0,  "Now (Wanted standing)"},
		{6,  "6 hours later"},
		{12, "12 hours later"},
		{24, "24 hours later"},
	}

	for _, snap := range negSnapshots {
		store.seed(smuggler, faction.StoredDisposition{
			FactionID: "iar",
			Value:     -400,
			UpdatedAt: time.Now().Add(-time.Duration(snap.hoursAgo) * time.Hour),
		})

		d, _ := engine.GetDisposition(ctx, smuggler, "iar")
		fmt.Printf("%-25s  value=%-5d  tier=%s\n",
			snap.label, d.Value, d.Tier.Name)
	}

	fmt.Println()
	fmt.Println("Decay is evaluated lazily on read — no background service required.")
}

// memoryStore with seed support for the decay example.
type memoryStore struct {
	mu           sync.Mutex
	dispositions map[string]faction.StoredDisposition
	history      []faction.DispositionRecord
}

func newMemoryStore() *memoryStore {
	return &memoryStore{dispositions: make(map[string]faction.StoredDisposition)}
}

func (s *memoryStore) seed(e faction.EntityID, d faction.StoredDisposition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dispositions[string(e)+"/"+string(d.FactionID)] = d
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
