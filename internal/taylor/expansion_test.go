package taylor_test

import (
	"math"
	"testing"

	"github.com/d1sco/cosfaction/cosfaction/internal/taylor"
)

// mockGraph implements taylor.Graph for testing.
type mockGraph struct {
	relations map[string][]taylor.Relation
}

func (g *mockGraph) RelationsFrom(factionID string) []taylor.Relation {
	return g.relations[factionID]
}

func TestExpand_FirstOrderOnly(t *testing.T) {
	// A graph with no relations should produce only the first order term
	// applying the full delta directly to the source faction.
	g := &mockGraph{relations: map[string][]taylor.Relation{}}

	expansion := taylor.Expand(g, "faction-a", 100.0, taylor.DefaultConfig())

	if len(expansion.Terms) != 1 {
		t.Fatalf("expected 1 term, got %d", len(expansion.Terms))
	}

	term := expansion.Terms[0]
	if term.Order != 1 {
		t.Errorf("expected order 1, got %d", term.Order)
	}
	if term.FactionID != "faction-a" {
		t.Errorf("expected faction-a, got %s", term.FactionID)
	}
	if term.DampedEffect != 100.0 {
		t.Errorf("expected effect 100.0, got %.2f", term.DampedEffect)
	}
}

func TestExpand_SecondOrderPropagation(t *testing.T) {
	// With Option B normalization, the second order effect is:
	//
	//   T_2 = (δ_n^2 / 2!) × R(iar,union) × range
	//
	// where δ_n = delta/range = 100/2000 = 0.05
	//
	//   T_2 = (0.05^2 / 2) × -0.8 × 2000
	//       = (0.0025 / 2) × -0.8 × 2000
	//       = 0.00125 × -1600
	//       = -2.0
	g := &mockGraph{
		relations: map[string][]taylor.Relation{
			"iar": {{TargetFactionID: "union", Influence: -0.8}},
		},
	}

	expansion := taylor.Expand(g, "iar", 100.0, taylor.Config{
		MaxOrder:         2,
		Threshold:        0.1,
		DispositionRange: 2000.0,
	})

	if len(expansion.Terms) < 2 {
		t.Fatalf("expected at least 2 terms, got %d", len(expansion.Terms))
	}

	var secondOrder *taylor.Term
	for i := range expansion.Terms {
		if expansion.Terms[i].Order == 2 {
			secondOrder = &expansion.Terms[i]
			break
		}
	}

	if secondOrder == nil {
		t.Fatal("expected second order term, found none")
	}

	expected := -2.0
	if math.Abs(secondOrder.DampedEffect-expected) > 0.01 {
		t.Errorf("expected second order effect %.2f, got %.2f", expected, secondOrder.DampedEffect)
	}

	if secondOrder.FactionID != "union" {
		t.Errorf("expected union, got %s", secondOrder.FactionID)
	}
}

func TestExpand_SecondOrderPropagation_MajorEvent(t *testing.T) {
	// For a major event (delta=1000, range=2000) the second order effect
	// should be substantially larger:
	//
	//   δ_n = 1000/2000 = 0.5
	//   T_2 = (0.5^2 / 2) × -0.8 × 2000
	//       = (0.25 / 2) × -1600
	//       = 0.125 × -1600
	//       = -200
	g := &mockGraph{
		relations: map[string][]taylor.Relation{
			"iar": {{TargetFactionID: "union", Influence: -0.8}},
		},
	}

	expansion := taylor.Expand(g, "iar", 1000.0, taylor.Config{
		MaxOrder:         2,
		Threshold:        0.1,
		DispositionRange: 2000.0,
	})

	var secondOrder *taylor.Term
	for i := range expansion.Terms {
		if expansion.Terms[i].Order == 2 {
			secondOrder = &expansion.Terms[i]
			break
		}
	}

	if secondOrder == nil {
		t.Fatal("expected second order term for major event, found none")
	}

	expected := -200.0
	if math.Abs(secondOrder.DampedEffect-expected) > 0.1 {
		t.Errorf("expected second order effect %.2f, got %.2f", expected, secondOrder.DampedEffect)
	}
}

func TestExpand_RoutineActionNoHigherOrder(t *testing.T) {
	// A routine action (small delta relative to range) should produce
	// no meaningful higher order effects — they fall below threshold.
	//
	// delta=50, range=2000: δ_n = 0.025
	// T_2 = (0.025^2 / 2) × -0.8 × 2000 = 0.000625 / 2 × -1600 = -0.5
	// Right at threshold. With threshold=1.0 this should converge.
	g := &mockGraph{
		relations: map[string][]taylor.Relation{
			"iar": {{TargetFactionID: "union", Influence: -0.8}},
		},
	}

	expansion := taylor.Expand(g, "iar", 50.0, taylor.Config{
		MaxOrder:         3,
		Threshold:        1.0,
		DispositionRange: 2000.0,
	})

	for _, term := range expansion.Terms {
		if term.Order > 1 {
			t.Errorf(
				"expected no higher order terms for routine action, got order %d effect %.4f",
				term.Order, term.DampedEffect,
			)
		}
	}
}

func TestExpand_ConvergenceGuarantee(t *testing.T) {
	// With Option B normalization the series has double-guaranteed convergence:
	// both delta^n shrinkage (since |delta_norm| < 1) and factorial damping.
	// Total effects must be strictly bounded.
	g := &mockGraph{
		relations: map[string][]taylor.Relation{
			"a": {{TargetFactionID: "b", Influence: 0.9}},
			"b": {{TargetFactionID: "a", Influence: 0.9}},
		},
	}

	expansion := taylor.Expand(g, "a", 1000.0, taylor.Config{
		MaxOrder:         10,
		Threshold:        0.001,
		DispositionRange: 2000.0,
	})

	totalEffect := 0.0
	for _, term := range expansion.Terms {
		totalEffect += math.Abs(term.DampedEffect)
	}

	// With normalization delta_norm = 0.5. The series is bounded by:
	// delta + range × Σ (delta_norm^n × r^n / n!) for n=2..inf
	// which converges much faster than the non-normalized version.
	// A generous bound is the original delta plus the full range.
	bound := 1000.0 + 2000.0
	if totalEffect > bound {
		t.Errorf("series did not converge: total effect %.2f exceeds bound %.2f", totalEffect, bound)
	}
}

func TestExpand_ThresholdTermination(t *testing.T) {
	// With Option B normalization and a small delta, higher order effects
	// should fall below threshold immediately.
	//
	// delta=10, range=2000: delta_norm = 0.005
	// T_2 = (0.005^2 / 2) × 0.1 × 2000 = 0.0000125 × 200 = 0.0025
	// Well below threshold=1.0 — expansion should converge at order 2.
	g := &mockGraph{
		relations: map[string][]taylor.Relation{
			"a": {{TargetFactionID: "b", Influence: 0.1}},
		},
	}

	expansion := taylor.Expand(g, "a", 10.0, taylor.Config{
		MaxOrder:         5,
		Threshold:        1.0,
		DispositionRange: 2000.0,
	})

	if !expansion.Converged {
		t.Error("expected expansion to converge below threshold")
	}
}

func TestExpand_CycleHandling(t *testing.T) {
	// Mutual opposition between two factions forms a cycle.
	// The expansion must not loop infinitely.
	g := &mockGraph{
		relations: map[string][]taylor.Relation{
			"iar":   {{TargetFactionID: "union", Influence: -0.8}},
			"union": {{TargetFactionID: "iar", Influence: -0.8}},
		},
	}

	done := make(chan struct{})
	go func() {
		taylor.Expand(g, "iar", 100.0, taylor.Config{
			MaxOrder:         5,
			Threshold:        0.5,
			DispositionRange: 2000.0,
		})
		close(done)
	}()

	select {
	case <-done:
		// success
	}
}

func TestExpand_ZeroInfluenceIgnored(t *testing.T) {
	// Relations with zero influence should not produce propagation terms.
	g := &mockGraph{
		relations: map[string][]taylor.Relation{
			"a": {{TargetFactionID: "b", Influence: 0.0}},
		},
	}

	expansion := taylor.Expand(g, "a", 100.0, taylor.Config{
		MaxOrder:         3,
		Threshold:        0.5,
		DispositionRange: 2000.0,
	})

	for _, term := range expansion.Terms {
		if term.FactionID == "b" {
			t.Error("expected no propagation to faction-b with zero influence")
		}
	}
}

func TestExpand_FirstOrderUnchanged(t *testing.T) {
	// The first order term must always equal the original delta exactly,
	// regardless of normalization. delta^1 / 1! × range^0 = delta.
	g := &mockGraph{relations: map[string][]taylor.Relation{}}

	for _, delta := range []float64{1, 50, 100, 500, 1000} {
		expansion := taylor.Expand(g, "a", delta, taylor.Config{
			MaxOrder:         3,
			Threshold:        0.5,
			DispositionRange: 2000.0,
		})

		if len(expansion.Terms) != 1 {
			t.Fatalf("delta=%.0f: expected 1 term, got %d", delta, len(expansion.Terms))
		}
		if math.Abs(expansion.Terms[0].DampedEffect-delta) > 0.001 {
			t.Errorf("delta=%.0f: first order term %.4f != delta", delta, expansion.Terms[0].DampedEffect)
		}
	}
}

func BenchmarkExpand_TwoFactions(b *testing.B) {
	g := &mockGraph{
		relations: map[string][]taylor.Relation{
			"iar":   {{TargetFactionID: "union", Influence: -0.8}},
			"union": {{TargetFactionID: "iar", Influence: -0.8}},
		},
	}
	cfg := taylor.Config{
		MaxOrder:         3,
		Threshold:        0.5,
		DispositionRange: 2000.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		taylor.Expand(g, "iar", 100.0, cfg)
	}
}

func BenchmarkExpand_FiveFactions(b *testing.B) {
	g := &mockGraph{
		relations: map[string][]taylor.Relation{
			"a": {
				{TargetFactionID: "b", Influence: -0.8},
				{TargetFactionID: "c", Influence: 0.5},
			},
			"b": {
				{TargetFactionID: "a", Influence: -0.8},
				{TargetFactionID: "d", Influence: 0.3},
			},
			"c": {
				{TargetFactionID: "e", Influence: 0.6},
			},
			"d": {
				{TargetFactionID: "e", Influence: -0.4},
			},
		},
	}
	cfg := taylor.Config{
		MaxOrder:         3,
		Threshold:        0.5,
		DispositionRange: 2000.0,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		taylor.Expand(g, "a", 100.0, cfg)
	}
}
