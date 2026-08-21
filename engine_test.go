package faction_test

import (
	"context"
	"sync"
	"testing"
	"time"

	faction "github.com/d1sco/cosfaction"
)

// ── Test fixtures ─────────────────────────────────────────────────────────────

// clusters returns the standard IAR/Union two-faction config used across
// most engine tests. Range is 2000 (-1000 to +1000).
func clustersConfig(store faction.Store, opts ...func(*faction.Config)) faction.Config {
	cfg := faction.Config{
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
		Store: store,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	return cfg
}

// memStore is a minimal in-memory Store for engine tests.
// It is not safe for concurrent use — engine tests are sequential.
type memStore struct {
	mu           sync.Mutex
	dispositions map[string]faction.StoredDisposition
	history      []faction.DispositionRecord
}

func newMemStore() *memStore {
	return &memStore{
		dispositions: make(map[string]faction.StoredDisposition),
	}
}

// seed writes a StoredDisposition directly to the store, bypassing the
// engine. Used by decay tests to control UpdatedAt precisely.
func (s *memStore) seed(e faction.EntityID, d faction.StoredDisposition) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dispositions[string(e)+"/"+string(d.FactionID)] = d
}

func (s *memStore) key(e faction.EntityID, f faction.FactionID) string {
	return string(e) + "/" + string(f)
}

func (s *memStore) GetDisposition(_ context.Context, e faction.EntityID, f faction.FactionID) (faction.StoredDisposition, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.dispositions[s.key(e, f)], nil
}

func (s *memStore) SetDisposition(_ context.Context, e faction.EntityID, f faction.FactionID, v int64) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.dispositions[s.key(e, f)] = faction.StoredDisposition{
		FactionID: f,
		Value:     v,
		UpdatedAt: time.Now(),
	}
	return nil
}

func (s *memStore) GetAllDispositions(_ context.Context, e faction.EntityID) ([]faction.StoredDisposition, error) {
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

func (s *memStore) GetDispositionHistory(_ context.Context, _ faction.EntityID, _ faction.FactionID, limit int) ([]faction.DispositionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if limit > len(s.history) {
		limit = len(s.history)
	}
	return s.history[len(s.history)-limit:], nil
}

func (s *memStore) RecordDispositionChange(_ context.Context, r faction.DispositionRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.history = append(s.history, r)
	return nil
}

// capturePublisher collects all published events for assertion in tests.
type capturePublisher struct {
	mu     sync.Mutex
	events []faction.Event
}

func (p *capturePublisher) Publish(e faction.Event) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.events = append(p.events, e)
	return nil
}

func (p *capturePublisher) all() []faction.Event {
	p.mu.Lock()
	defer p.mu.Unlock()
	out := make([]faction.Event, len(p.events))
	copy(out, p.events)
	return out
}

func (p *capturePublisher) ofType(t faction.EventType) []faction.Event {
	all := p.all()
	var result []faction.Event
	for _, e := range all {
		if e.Type == t {
			result = append(result, e)
		}
	}
	return result
}

// ── New() validation ──────────────────────────────────────────────────────────

func TestNew_RequiresFactions(t *testing.T) {
	_, err := faction.New(faction.Config{
		Tiers: []faction.Tier{{Name: "Neutral", MinValue: -100, MaxValue: 100}},
		Store: newMemStore(),
	})
	if err == nil {
		t.Error("expected error when no factions configured")
	}
}

func TestNew_RequiresTiers(t *testing.T) {
	_, err := faction.New(faction.Config{
		Factions: []faction.Faction{{ID: "iar", Name: "IAR"}},
		Store:    newMemStore(),
	})
	if err == nil {
		t.Error("expected error when no tiers configured")
	}
}

func TestNew_RequiresStore(t *testing.T) {
	_, err := faction.New(faction.Config{
		Factions: []faction.Faction{{ID: "iar", Name: "IAR"}},
		Tiers:    []faction.Tier{{Name: "Neutral", MinValue: -100, MaxValue: 100}},
	})
	if err == nil {
		t.Error("expected error when no store configured")
	}
}

func TestNew_RejectsEmptyFactionID(t *testing.T) {
	_, err := faction.New(faction.Config{
		Factions: []faction.Faction{{ID: "", Name: "Unnamed"}},
		Tiers:    []faction.Tier{{Name: "Neutral", MinValue: -100, MaxValue: 100}},
		Store:    newMemStore(),
	})
	if err == nil {
		t.Error("expected error for empty faction ID")
	}
}

func TestNew_RejectsInvalidRelationInfluence(t *testing.T) {
	_, err := faction.New(faction.Config{
		Factions: []faction.Faction{
			{ID: "iar", Name: "IAR"},
			{ID: "union", Name: "Union"},
		},
		Tiers: []faction.Tier{{Name: "Neutral", MinValue: -100, MaxValue: 100}},
		Relations: []faction.Relation{
			{FactionA: "iar", FactionB: "union", Influence: 1.5},
		},
		Store: newMemStore(),
	})
	if err == nil {
		t.Error("expected error for influence weight outside [-1.0, 1.0]")
	}
}

func TestNew_RejectsRelationToUnknownFaction(t *testing.T) {
	_, err := faction.New(faction.Config{
		Factions: []faction.Faction{{ID: "iar", Name: "IAR"}},
		Tiers:    []faction.Tier{{Name: "Neutral", MinValue: -100, MaxValue: 100}},
		Relations: []faction.Relation{
			{FactionA: "iar", FactionB: "unknown", Influence: -0.8},
		},
		Store: newMemStore(),
	})
	if err == nil {
		t.Error("expected error for relation referencing unknown faction")
	}
}

func TestNew_Succeeds(t *testing.T) {
	engine, err := faction.New(clustersConfig(newMemStore()))
	if err != nil {
		t.Fatalf("expected successful construction, got: %v", err)
	}
	if engine == nil {
		t.Fatal("expected non-nil engine")
	}
}

// ── GetDisposition ────────────────────────────────────────────────────────────

func TestGetDisposition_DefaultsToZero(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	d, err := engine.GetDisposition(ctx, "char-001", "iar")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if d.Value != 0 {
		t.Errorf("expected zero disposition for new entity, got %d", d.Value)
	}
	if d.Tier.Name != "Neutral" {
		t.Errorf("expected Neutral tier at zero, got %s", d.Tier.Name)
	}
}

func TestGetDisposition_UnknownFaction(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	_, err := engine.GetDisposition(ctx, "char-001", "unknown-faction")
	if err == nil {
		t.Error("expected error for unknown faction")
	}
}

// ── ApplyDelta ────────────────────────────────────────────────────────────────

func TestApplyDelta_DirectEffect(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	result, err := engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  "char-001",
		FactionID: "iar",
		Amount:    100,
		Reason:    "quest completed",
		Source:    "test",
	})
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}

	// First order term must equal the original delta exactly.
	var firstOrder *faction.PropagationOrder
	for i := range result.Orders {
		if result.Orders[i].Order == 1 {
			firstOrder = &result.Orders[i]
			break
		}
	}
	if firstOrder == nil {
		t.Fatal("expected first order term in result")
	}
	if firstOrder.Effect != 100 {
		t.Errorf("expected first order effect 100, got %.0f", firstOrder.Effect)
	}
	if firstOrder.FactionID != "iar" {
		t.Errorf("expected first order faction iar, got %s", firstOrder.FactionID)
	}

	// Verify disposition was stored.
	d, err := engine.GetDisposition(ctx, "char-001", "iar")
	if err != nil {
		t.Fatalf("GetDisposition after ApplyDelta failed: %v", err)
	}
	if d.Value != 100 {
		t.Errorf("expected stored value 100, got %d", d.Value)
	}
}

func TestApplyDelta_UnknownFaction(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	_, err := engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  "char-001",
		FactionID: "unknown",
		Amount:    100,
	})
	if err == nil {
		t.Error("expected error for unknown faction")
	}
}

func TestApplyDelta_PropagatesSecondOrder(t *testing.T) {
	// For delta=1000, range=2000: delta_norm=0.5
	// T_2 = (0.5^2 / 2!) × -0.8 × 2000 = -200
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	_, err := engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  "char-001",
		FactionID: "union",
		Amount:    1000,
		Reason:    "city alignment switched",
	})
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}

	// Union should have gained 1000.
	union, _ := engine.GetDisposition(ctx, "char-001", "union")
	if union.Value != 1000 {
		t.Errorf("expected union=1000, got %d", union.Value)
	}

	// IAR should have a second order loss of -200.
	iar, _ := engine.GetDisposition(ctx, "char-001", "iar")
	if iar.Value != -200 {
		t.Errorf("expected iar=-200 from second order propagation, got %d", iar.Value)
	}
}

func TestApplyDelta_RoutineActionNoSecondOrder(t *testing.T) {
	// For delta=50, range=2000: delta_norm=0.025
	// T_2 = (0.025^2 / 2!) × -0.8 × 2000 = -0.5
	// math.Round(-0.5) = -1 in Go. The effect is applied but is minimal.
	// Verify the IAR effect is at most 1 point — negligible for game purposes.
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	_, err := engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  "char-001",
		FactionID: "union",
		Amount:    50,
	})
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}

	// IAR should be at most -1 — the second order effect at this delta
	// is at the rounding boundary between 0 and -1.
	iar, _ := engine.GetDisposition(ctx, "char-001", "iar")
	if iar.Value < -1 {
		t.Errorf("expected iar >= -1 for routine action (delta=50), got %d", iar.Value)
	}
}

func TestApplyDelta_ClampsAtMaximum(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	// Apply a delta that would exceed the maximum tier value of 1000.
	engine.ApplyDelta(ctx, faction.Delta{EntityID: "char-001", FactionID: "iar", Amount: 800})
	engine.ApplyDelta(ctx, faction.Delta{EntityID: "char-001", FactionID: "iar", Amount: 800})

	d, _ := engine.GetDisposition(ctx, "char-001", "iar")
	if d.Value > 1000 {
		t.Errorf("expected value clamped to 1000, got %d", d.Value)
	}
}

func TestApplyDelta_ClampsAtMinimum(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	engine.ApplyDelta(ctx, faction.Delta{EntityID: "char-001", FactionID: "iar", Amount: -800})
	engine.ApplyDelta(ctx, faction.Delta{EntityID: "char-001", FactionID: "iar", Amount: -800})

	d, _ := engine.GetDisposition(ctx, "char-001", "iar")
	if d.Value < -1000 {
		t.Errorf("expected value clamped to -1000, got %d", d.Value)
	}
}

func TestApplyDelta_AccumulatesCorrectly(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	engine.ApplyDelta(ctx, faction.Delta{EntityID: "char-001", FactionID: "iar", Amount: 100})
	engine.ApplyDelta(ctx, faction.Delta{EntityID: "char-001", FactionID: "iar", Amount: 150})
	engine.ApplyDelta(ctx, faction.Delta{EntityID: "char-001", FactionID: "iar", Amount: -50})

	d, _ := engine.GetDisposition(ctx, "char-001", "iar")
	if d.Value != 200 {
		t.Errorf("expected accumulated value 200, got %d", d.Value)
	}
}

// ── Tier resolution ───────────────────────────────────────────────────────────

func TestTierFor_AllTiers(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))

	tests := []struct {
		value    int64
		expected string
	}{
		{-1000, "Outlawed"},
		{-750, "Outlawed"},
		{-601, "Outlawed"},
		{-600, "Wanted"},
		{-400, "Wanted"},
		{-201, "Wanted"},
		{-200, "Suspected"},
		{-100, "Suspected"},
		{-1, "Suspected"},
		{0, "Neutral"},
		{150, "Neutral"},
		{299, "Neutral"},
		{300, "Trusted"},
		{500, "Trusted"},
		{699, "Trusted"},
		{700, "Celebrated"},
		{850, "Celebrated"},
		{1000, "Celebrated"},
	}

	for _, tt := range tests {
		tier := engine.TierFor(tt.value)
		if tier.Name != tt.expected {
			t.Errorf("TierFor(%d) = %s, want %s", tt.value, tier.Name, tt.expected)
		}
	}
}

func TestTierFor_BelowMinimumReturnsLowest(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	tier := engine.TierFor(-9999)
	if tier.Name != "Outlawed" {
		t.Errorf("expected Outlawed for value below minimum, got %s", tier.Name)
	}
}

func TestTierFor_AboveMaximumReturnsHighest(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	tier := engine.TierFor(9999)
	if tier.Name != "Celebrated" {
		t.Errorf("expected Celebrated for value above maximum, got %s", tier.Name)
	}
}

// ── GetAllDispositions ────────────────────────────────────────────────────────

func TestGetAllDispositions_ReturnsAll(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	engine.ApplyDelta(ctx, faction.Delta{EntityID: "char-001", FactionID: "union", Amount: 1000})

	all, err := engine.GetAllDispositions(ctx, "char-001")
	if err != nil {
		t.Fatalf("GetAllDispositions failed: %v", err)
	}

	byFaction := make(map[faction.FactionID]faction.Disposition)
	for _, d := range all {
		byFaction[d.FactionID] = d
	}

	if byFaction["union"].Value != 1000 {
		t.Errorf("expected union=1000, got %d", byFaction["union"].Value)
	}
	if byFaction["iar"].Value != -200 {
		t.Errorf("expected iar=-200 from propagation, got %d", byFaction["iar"].Value)
	}
}

func TestGetAllDispositions_EmptyEntity(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	all, err := engine.GetAllDispositions(ctx, "new-entity")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(all) != 0 {
		t.Errorf("expected empty slice for new entity, got %d records", len(all))
	}
}

// ── Event emission ────────────────────────────────────────────────────────────

func TestApplyDelta_PublishesDispositionChangedEvent(t *testing.T) {
	pub := &capturePublisher{}
	engine, _ := faction.New(clustersConfig(newMemStore(), func(cfg *faction.Config) {
		cfg.Publisher = pub
	}))
	ctx := context.Background()

	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  "char-001",
		FactionID: "iar",
		Amount:    100,
		Reason:    "quest",
		Source:    "test",
	})

	events := pub.ofType(faction.EventDispositionChanged)
	if len(events) == 0 {
		t.Fatal("expected at least one disposition.changed event")
	}

	e := events[0]
	if e.EntityID != "char-001" {
		t.Errorf("EntityID: got %s, want char-001", e.EntityID)
	}
	if e.FactionID != "iar" {
		t.Errorf("FactionID: got %s, want iar", e.FactionID)
	}
	if e.NewValue != 100 {
		t.Errorf("NewValue: got %d, want 100", e.NewValue)
	}
	if e.Reason != "quest" {
		t.Errorf("Reason: got %s, want quest", e.Reason)
	}
}

func TestApplyDelta_PublishesTierCrossedEvent(t *testing.T) {
	pub := &capturePublisher{}
	engine, _ := faction.New(clustersConfig(newMemStore(), func(cfg *faction.Config) {
		cfg.Publisher = pub
	}))
	ctx := context.Background()

	// delta=1000 on union should cross IAR into Suspected (-200).
	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  "char-001",
		FactionID: "union",
		Amount:    1000,
	})

	tierEvents := pub.ofType(faction.EventTierCrossed)
	if len(tierEvents) == 0 {
		t.Fatal("expected at least one disposition.tier_crossed event")
	}

	// Find the IAR tier crossing.
	var iarCrossing *faction.Event
	for i := range tierEvents {
		if tierEvents[i].FactionID == "iar" {
			iarCrossing = &tierEvents[i]
			break
		}
	}
	if iarCrossing == nil {
		t.Fatal("expected tier crossed event for iar faction")
	}
	if iarCrossing.PreviousTier != "Neutral" {
		t.Errorf("PreviousTier: got %s, want Neutral", iarCrossing.PreviousTier)
	}
	if iarCrossing.NewTier != "Suspected" {
		t.Errorf("NewTier: got %s, want Suspected", iarCrossing.NewTier)
	}
}

func TestApplyDelta_NoTierCrossedEventWithinSameTier(t *testing.T) {
	pub := &capturePublisher{}
	engine, _ := faction.New(clustersConfig(newMemStore(), func(cfg *faction.Config) {
		cfg.Publisher = pub
	}))
	ctx := context.Background()

	// Small delta stays within Neutral tier — no tier crossing.
	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  "char-001",
		FactionID: "iar",
		Amount:    50,
	})

	tierEvents := pub.ofType(faction.EventTierCrossed)
	for _, e := range tierEvents {
		if e.FactionID == "iar" {
			t.Errorf("unexpected tier crossed event for iar: %s → %s", e.PreviousTier, e.NewTier)
		}
	}
}

func TestApplyDelta_PropagationOrderInEvents(t *testing.T) {
	pub := &capturePublisher{}
	engine, _ := faction.New(clustersConfig(newMemStore(), func(cfg *faction.Config) {
		cfg.Publisher = pub
	}))
	ctx := context.Background()

	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  "char-001",
		FactionID: "union",
		Amount:    1000,
	})

	events := pub.ofType(faction.EventDispositionChanged)

	orders := make(map[int]bool)
	for _, e := range events {
		orders[e.PropagationOrder] = true
	}

	if !orders[1] {
		t.Error("expected order 1 (direct) event")
	}
	if !orders[2] {
		t.Error("expected order 2 (propagated) event for major delta")
	}
}

// ── Decay ─────────────────────────────────────────────────────────────────────

func TestDecay_DisabledByDefault(t *testing.T) {
	store := newMemStore()
	engine, _ := faction.New(clustersConfig(store))
	ctx := context.Background()

	store.seed("char-001", faction.StoredDisposition{
		FactionID: "iar",
		Value:     500,
		UpdatedAt: time.Now().Add(-48 * time.Hour),
	})

	d, _ := engine.GetDisposition(ctx, "char-001", "iar")
	if d.Value != 500 {
		t.Errorf("expected no decay when disabled, got value %d", d.Value)
	}
}

func TestDecay_TowardNeutral(t *testing.T) {
	store := newMemStore()
	engine, _ := faction.New(clustersConfig(store, func(cfg *faction.Config) {
		cfg.Decay = faction.DecayConfig{
			Enabled:     true,
			RatePerHour: 100,
			Target:      "neutral",
		}
	}))
	ctx := context.Background()

	// 2 hours ago: decay = 100 × 2 = 200. Value = 500 - 200 = 300.
	store.seed("char-001", faction.StoredDisposition{
		FactionID: "iar",
		Value:     500,
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	})

	d, _ := engine.GetDisposition(ctx, "char-001", "iar")
	// Allow ±1 for sub-second timing imprecision.
	if d.Value < 299 || d.Value > 301 {
		t.Errorf("expected decayed value ~300, got %d", d.Value)
	}
}

func TestDecay_DoesNotCrossNeutral(t *testing.T) {
	store := newMemStore()
	engine, _ := faction.New(clustersConfig(store, func(cfg *faction.Config) {
		cfg.Decay = faction.DecayConfig{
			Enabled:     true,
			RatePerHour: 1000,
			Target:      "neutral",
		}
	}))
	ctx := context.Background()

	store.seed("char-001", faction.StoredDisposition{
		FactionID: "iar",
		Value:     500,
		UpdatedAt: time.Now().Add(-24 * time.Hour),
	})

	d, _ := engine.GetDisposition(ctx, "char-001", "iar")
	neutral := int64(149) // (0 + 299) / 2
	if d.Value != neutral {
		t.Errorf("expected decay to clamp at neutral %d, got %d", neutral, d.Value)
	}
}

func TestDecay_TowardZero(t *testing.T) {
	store := newMemStore()
	engine, _ := faction.New(clustersConfig(store, func(cfg *faction.Config) {
		cfg.Decay = faction.DecayConfig{
			Enabled:     true,
			RatePerHour: 100,
			Target:      "zero",
		}
	}))
	ctx := context.Background()

	// 2 hours ago: decay = 100 × 2 = 200 toward zero. Value = -400 + 200 = -200.
	store.seed("char-001", faction.StoredDisposition{
		FactionID: "iar",
		Value:     -400,
		UpdatedAt: time.Now().Add(-2 * time.Hour),
	})

	d, _ := engine.GetDisposition(ctx, "char-001", "iar")
	// Allow ±1 for sub-second timing imprecision.
	if d.Value < -201 || d.Value > -199 {
		t.Errorf("expected decayed value ~-200, got %d", d.Value)
	}
}

// ── No Publisher ──────────────────────────────────────────────────────────────

func TestApplyDelta_NilPublisherDoesNotPanic(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	defer func() {
		if r := recover(); r != nil {
			t.Errorf("ApplyDelta panicked with nil publisher: %v", r)
		}
	}()

	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  "char-001",
		FactionID: "iar",
		Amount:    100,
	})
}

// ── Result structure ──────────────────────────────────────────────────────────

func TestApplyDelta_ResultStructure(t *testing.T) {
	engine, _ := faction.New(clustersConfig(newMemStore()))
	ctx := context.Background()

	result, err := engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  "char-001",
		FactionID: "union",
		Amount:    1000,
	})
	if err != nil {
		t.Fatalf("ApplyDelta failed: %v", err)
	}

	if result.SourceFaction != "union" {
		t.Errorf("SourceFaction: got %s, want union", result.SourceFaction)
	}
	if result.OriginalDelta != 1000 {
		t.Errorf("OriginalDelta: got %.0f, want 1000", result.OriginalDelta)
	}
	if len(result.Orders) == 0 {
		t.Error("expected at least one order in result")
	}
}

// ── Tier helpers ──────────────────────────────────────────────────────────────

func TestEvenTiers_DividesEvenly(t *testing.T) {
	tiers, err := faction.EvenTiers(-1000, 1000,
		"Outlawed", "Wanted", "Suspected", "Neutral", "Trusted", "Celebrated",
	)
	if err != nil {
		t.Fatalf("EvenTiers failed: %v", err)
	}
	if len(tiers) != 6 {
		t.Fatalf("expected 6 tiers, got %d", len(tiers))
	}

	// First tier must start at min.
	if tiers[0].MinValue != -1000 {
		t.Errorf("first tier MinValue: got %d, want -1000", tiers[0].MinValue)
	}
	// Last tier must end at max.
	if tiers[len(tiers)-1].MaxValue != 1000 {
		t.Errorf("last tier MaxValue: got %d, want 1000", tiers[len(tiers)-1].MaxValue)
	}
	// Tiers must be contiguous.
	for i := 1; i < len(tiers); i++ {
		if tiers[i].MinValue != tiers[i-1].MaxValue+1 {
			t.Errorf("gap between tier %d and %d: %d -> %d",
				i-1, i, tiers[i-1].MaxValue, tiers[i].MinValue)
		}
	}
	// Names must be in order.
	if tiers[0].Name != "Outlawed" {
		t.Errorf("tier 0: got %s, want Outlawed", tiers[0].Name)
	}
	if tiers[5].Name != "Celebrated" {
		t.Errorf("tier 5: got %s, want Celebrated", tiers[5].Name)
	}
}

func TestEvenTiers_InsertingTierRecalculatesBoundaries(t *testing.T) {
	// Adding Revered between Trusted and Celebrated should produce
	// valid contiguous tiers without the caller touching any values.
	tiers, err := faction.EvenTiers(-1000, 1000,
		"Outlawed", "Wanted", "Suspected", "Neutral", "Trusted", "Revered", "Celebrated",
	)
	if err != nil {
		t.Fatalf("EvenTiers failed: %v", err)
	}
	if len(tiers) != 7 {
		t.Fatalf("expected 7 tiers, got %d", len(tiers))
	}
	if tiers[0].MinValue != -1000 {
		t.Errorf("first MinValue: got %d, want -1000", tiers[0].MinValue)
	}
	if tiers[6].MaxValue != 1000 {
		t.Errorf("last MaxValue: got %d, want 1000", tiers[6].MaxValue)
	}
	for i := 1; i < len(tiers); i++ {
		if tiers[i].MinValue != tiers[i-1].MaxValue+1 {
			t.Errorf("gap at tier %d/%d", i-1, i)
		}
	}
}

func TestEvenTiers_ErrorOnEmpty(t *testing.T) {
	_, err := faction.EvenTiers(-1000, 1000)
	if err == nil {
		t.Error("expected error for empty tier names")
	}
}

func TestEvenTiers_ErrorOnInvalidRange(t *testing.T) {
	_, err := faction.EvenTiers(100, -100, "A", "B")
	if err == nil {
		t.Error("expected error when min >= max")
	}
}

func TestWeightedTiers_ProportionalWidths(t *testing.T) {
	tiers, err := faction.WeightedTiers(-1000, 1000, []faction.TierWeight{
		{Name: "Hostile", Weight: 1},
		{Name: "Neutral", Weight: 2}, // twice as wide
		{Name: "Friendly", Weight: 1},
	})
	if err != nil {
		t.Fatalf("WeightedTiers failed: %v", err)
	}
	if len(tiers) != 3 {
		t.Fatalf("expected 3 tiers, got %d", len(tiers))
	}
	if tiers[0].MinValue != -1000 {
		t.Errorf("first MinValue: got %d, want -1000", tiers[0].MinValue)
	}
	if tiers[2].MaxValue != 1000 {
		t.Errorf("last MaxValue: got %d, want 1000", tiers[2].MaxValue)
	}

	neutralWidth := tiers[1].MaxValue - tiers[1].MinValue + 1
	hostileWidth := tiers[0].MaxValue - tiers[0].MinValue + 1

	// Neutral should be approximately twice the width of Hostile.
	ratio := float64(neutralWidth) / float64(hostileWidth)
	if ratio < 1.8 || ratio > 2.2 {
		t.Errorf("expected Neutral ~2x Hostile width, got ratio %.2f (%d vs %d)",
			ratio, neutralWidth, hostileWidth)
	}
}

func TestWeightedTiers_ErrorOnZeroWeight(t *testing.T) {
	_, err := faction.WeightedTiers(-1000, 1000, []faction.TierWeight{
		{Name: "A", Weight: 1},
		{Name: "B", Weight: 0},
	})
	if err == nil {
		t.Error("expected error for zero weight")
	}
}

func TestWeightedTiers_Contiguous(t *testing.T) {
	tiers, err := faction.WeightedTiers(-500, 500, []faction.TierWeight{
		{Name: "Low", Weight: 3},
		{Name: "Middle", Weight: 5},
		{Name: "High", Weight: 2},
	})
	if err != nil {
		t.Fatalf("WeightedTiers failed: %v", err)
	}
	for i := 1; i < len(tiers); i++ {
		if tiers[i].MinValue != tiers[i-1].MaxValue+1 {
			t.Errorf("gap between tier %d and %d", i-1, i)
		}
	}
}

// ── Tier validation ───────────────────────────────────────────────────────────

func TestNew_RejectsTiersWithGap(t *testing.T) {
	_, err := faction.New(faction.Config{
		Factions: []faction.Faction{{ID: "a", Name: "A"}},
		Tiers: []faction.Tier{
			{Name: "Low", MinValue: -100, MaxValue: -1},
			// Gap: 0 is uncovered
			{Name: "High", MinValue: 1, MaxValue: 100},
		},
		Store: newMemStore(),
	})
	if err == nil {
		t.Error("expected error for tier gap")
	}
}

func TestNew_RejectsTiersWithOverlap(t *testing.T) {
	_, err := faction.New(faction.Config{
		Factions: []faction.Faction{{ID: "a", Name: "A"}},
		Tiers: []faction.Tier{
			{Name: "Low", MinValue: -100, MaxValue: 10},
			// Overlap: 0..10 covered by both
			{Name: "High", MinValue: 0, MaxValue: 100},
		},
		Store: newMemStore(),
	})
	if err == nil {
		t.Error("expected error for tier overlap")
	}
}

func TestNew_AcceptsTiersInAnyOrder(t *testing.T) {
	// Tiers provided out of MinValue order should be sorted and accepted.
	_, err := faction.New(faction.Config{
		Factions: []faction.Faction{{ID: "a", Name: "A"}},
		Tiers: []faction.Tier{
			{Name: "High", MinValue: 1, MaxValue: 100},
			{Name: "Low", MinValue: -100, MaxValue: -1},
			{Name: "Neutral", MinValue: 0, MaxValue: 0},
		},
		Store: newMemStore(),
	})
	if err != nil {
		t.Errorf("expected success for out-of-order contiguous tiers, got: %v", err)
	}
}

func TestNew_RankDerivedFromOrder(t *testing.T) {
	// Engine uses internally derived rank — not any value set by the caller.
	// Provide tiers in reverse order and confirm tier resolution still works.
	engine, err := faction.New(faction.Config{
		Factions: []faction.Faction{{ID: "a", Name: "A"}},
		Tiers: []faction.Tier{
			{Name: "High", MinValue: 101, MaxValue: 200},
			{Name: "Low", MinValue: -200, MaxValue: -101},
			{Name: "Neutral", MinValue: -100, MaxValue: 100},
		},
		Store: newMemStore(),
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if engine.TierFor(-150).Name != "Low" {
		t.Errorf("expected Low for -150, got %s", engine.TierFor(-150).Name)
	}
	if engine.TierFor(0).Name != "Neutral" {
		t.Errorf("expected Neutral for 0, got %s", engine.TierFor(0).Name)
	}
	if engine.TierFor(150).Name != "High" {
		t.Errorf("expected High for 150, got %s", engine.TierFor(150).Name)
	}
}

func TestEvenTiers_UsableInEngineConfig(t *testing.T) {
	tiers, err := faction.EvenTiers(-1000, 1000,
		"Outlawed", "Wanted", "Suspected", "Neutral", "Trusted", "Celebrated",
	)
	if err != nil {
		t.Fatalf("EvenTiers failed: %v", err)
	}

	engine, err := faction.New(faction.Config{
		Factions: []faction.Faction{{ID: "iar", Name: "IAR"}},
		Tiers:    tiers,
		Store:    newMemStore(),
	})
	if err != nil {
		t.Fatalf("New with EvenTiers failed: %v", err)
	}

	// Verify the engine works correctly with derived tiers.
	ctx := context.Background()
	engine.ApplyDelta(ctx, faction.Delta{
		EntityID:  "char-001",
		FactionID: "iar",
		Amount:    900,
	})

	d, _ := engine.GetDisposition(ctx, "char-001", "iar")
	if d.Tier.Name != "Celebrated" {
		t.Errorf("expected Celebrated tier at 900, got %s", d.Tier.Name)
	}
}
