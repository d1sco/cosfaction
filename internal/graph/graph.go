// Package graph provides an in-memory weighted directed relationship graph
// for faction disposition propagation. The graph is built once from the
// engine configuration and is read-only during operation.
package graph

import (
	"fmt"
)

// RelationEdge represents a single directed relationship between two factions.
type RelationEdge struct {
	TargetFactionID string
	Influence       float64
}

// Graph is an adjacency list representation of the faction relationship graph.
// All methods are safe for concurrent reads. The graph is not safe for
// concurrent writes and must be fully built before use.
type Graph struct {
	edges map[string][]RelationEdge
}

// New constructs a Graph from a set of relation definitions.
// Returns an error if any relation references an undefined faction or
// if any influence weight falls outside [-1.0, 1.0].
func New(factionIDs []string, relations []Relation) (*Graph, error) {
	known := make(map[string]bool, len(factionIDs))
	for _, id := range factionIDs {
		known[id] = true
	}

	g := &Graph{
		edges: make(map[string][]RelationEdge, len(factionIDs)),
	}

	for _, r := range relations {
		if !known[r.FactionA] {
			return nil, fmt.Errorf("relation references unknown faction %q", r.FactionA)
		}
		if !known[r.FactionB] {
			return nil, fmt.Errorf("relation references unknown faction %q", r.FactionB)
		}
		if r.Influence < -1.0 || r.Influence > 1.0 {
			return nil, fmt.Errorf(
				"influence weight %.2f for relation %s->%s is outside [-1.0, 1.0]",
				r.Influence, r.FactionA, r.FactionB,
			)
		}

		g.edges[string(r.FactionA)] = append(g.edges[string(r.FactionA)], RelationEdge{
			TargetFactionID: string(r.FactionB),
			Influence:       r.Influence,
		})
	}

	return g, nil
}

// RelationsFrom returns all outgoing relationship edges from a faction.
// Returns an empty slice if the faction has no outgoing relations.
// Implements the taylor.Graph interface.
func (g *Graph) RelationsFrom(factionID string) []RelationEdge {
	return g.edges[factionID]
}

// Relation is the input type for building the graph.
type Relation struct {
	FactionA  string
	FactionB  string
	Influence float64
}
