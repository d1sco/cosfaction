// Package main demonstrates cosfaction's Publisher interface and event
// handling patterns for downstream game systems.
//
// Events are emitted for every disposition change — direct and propagated.
// Tier crossing events are the most actionable for game systems:
// enforcement recalculation, NPC behavior updates, and UI notifications
// should subscribe to disposition.tier_crossed.
//
// This example shows three realistic downstream subscribers:
//
//   - EnforcementService  recalculates inspection probability on tier change
//   - UIService           sends a player notification on tier change
//   - AuditService        logs every disposition change for analytics
//
// Run with:
//
//	go run ./examples/events
package main

import (
	"context"
	"fmt"
	"log"
	"sync"
	"time"

	faction "github.com/d1sco/cosfaction"
)

func main() {
	ctx := context.Background()

	pub := newDispatchPublisher()

	// EnforcementService reacts to tier crossings to recalculate
	// inspection probability for the affected character.
	pub.on(faction.EventTierCrossed, func(e faction.Event) {
		prob := inspectionProbability(e.NewTier)
		fmt.Printf(
			"  [EnforcementService] %s/%s tier crossed %s → %s  new inspection probability: %.0f%%\n",
			e.EntityID, e.FactionID, e.PreviousTier, e.NewTier, prob*100,
		)
	})

	// UIService sends a player notification on any tier change.
	pub.on(faction.EventTierCrossed, func(e faction.Event) {
		msg := tierCrossedMessage(e.FactionID, e.PreviousTier, e.NewTier, e.NewValue)
		fmt.Printf("  [UIService] notification → %s: %s\n", e.EntityID, msg)
	})

	// AuditService logs every disposition change for analytics.
	pub.on(faction.EventDispositionChanged, func(e faction.Event) {
		fmt.Printf(
			"  [AuditService] %s/%s  %+d → %+d  order=%d  source=%s\n",
			e.EntityID, e.FactionID,
			e.PreviousValue, e.NewValue,
			e.PropagationOrder, e.Source,
		)
	})

	engine, err := faction.New(faction.Config{
		Factions: []faction.Faction{
			{ID: "iar", Name: "Interstellar Authority Republic", Type: faction.FactionTypeGoverning},
			{ID: "union", Name: "The Union", Type: faction.FactionTypeResistance},
		},
		Tiers: []faction.Tier{
			{Name: "Outlawed", MinValue: -1000, MaxValue: -601},
			{Name: "Wanted", MinValue: -600, MaxValue: -201},
			{Name: "Suspected", MinValue: -200, MaxValue: -1},
			{Name: "Neutral", MinValue: 0, MaxValue: 299},
			{Name: "Trusted", MinValue: 300, MaxValue: 699},
			{Name: "Celebrated", MinValue: 700, MaxValue: 1000},
		},
		Relations: []faction.Relation{
			{FactionA: "iar", FactionB: "union", Influence: -0.8},
			{FactionA: "union", FactionB: "iar", Influence: -0.8},
		},
		Store:     newMemoryStore(),
		Publisher: pub,
	})
	if err != nil {
		log.Fatalf("failed to create engine: %v", err)
	}

	smuggler := faction.EntityID("smuggler-001")

	fmt.Println("=== cosfaction events example ===")
	fmt.Println()

	// Action 1: Routine delivery — small delta, no tier change.
	fmt.Println("--- Action 1: Routine delivery (+100 Union) ---")
	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  smuggler,
		FactionID: "union",
		Amount:    100,
		Reason:    "Delivered supplies to Union outpost",
		Source:    "delivery_quest",
	})
	fmt.Println()

	// Action 2: Major delivery — crosses IAR into Suspected.
	fmt.Println("--- Action 2: Major Union action (+800 Union) ---")
	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  smuggler,
		FactionID: "union",
		Amount:    800,
		Reason:    "Moved large weapons shipment for Union",
		Source:    "major_delivery",
	})
	fmt.Println()

	// Action 3: IAR bounty completion — crosses tiers in both directions.
	fmt.Println("--- Action 3: IAR bounty completion (+600 IAR) ---")
	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  smuggler,
		FactionID: "iar",
		Amount:    600,
		Reason:    "Delivered Union commander to IAR",
		Source:    "bounty_contract",
	})
	fmt.Println()

	fmt.Println("Subscribers react only to events they care about.")
	fmt.Println("AuditService sees everything. EnforcementService and UIService")
	fmt.Println("see only tier crossings — the signal that matters for game state.")
}

// inspectionProbability returns the base IAR inspection likelihood
// for a character at the given disposition tier.
func inspectionProbability(tierName string) float64 {
	switch tierName {
	case "Celebrated":
		return 0.02
	case "Trusted":
		return 0.05
	case "Neutral":
		return 0.15
	case "Suspected":
		return 0.45
	case "Wanted":
		return 0.75
	case "Outlawed":
		return 0.95
	default:
		return 0.15
	}
}

// tierCrossedMessage produces a human-readable notification for the player.
func tierCrossedMessage(factionID faction.FactionID, prev, next string, value int64) string {
	names := map[faction.FactionID]string{
		"iar":   "the Interstellar Authority Republic",
		"union": "the Union",
	}
	name := names[factionID]
	if name == "" {
		name = string(factionID)
	}

	rank := map[string]int{
		"Outlawed": 0, "Wanted": 1, "Suspected": 2,
		"Neutral": 3, "Trusted": 4, "Celebrated": 5,
	}

	if rank[next] > rank[prev] {
		return fmt.Sprintf("Your standing with %s has improved to %s (%d)", name, next, value)
	}
	return fmt.Sprintf("Your standing with %s has fallen to %s (%d)", name, next, value)
}

// dispatchPublisher routes events to registered handlers by event type.
type dispatchPublisher struct {
	mu       sync.RWMutex
	handlers map[faction.EventType][]func(faction.Event)
}

func newDispatchPublisher() *dispatchPublisher {
	return &dispatchPublisher{
		handlers: make(map[faction.EventType][]func(faction.Event)),
	}
}

func (p *dispatchPublisher) on(t faction.EventType, fn func(faction.Event)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.handlers[t] = append(p.handlers[t], fn)
}

func (p *dispatchPublisher) Publish(e faction.Event) error {
	p.mu.RLock()
	handlers := p.handlers[e.Type]
	p.mu.RUnlock()
	for _, h := range handlers {
		h(e)
	}
	return nil
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
