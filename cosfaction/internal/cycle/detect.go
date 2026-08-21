// Package cycle provides cycle detection for faction relationship graphs.
// Cycles in the graph are valid — opposing factions naturally form cycles —
// but the Taylor expansion must detect and handle them to prevent
// infinite propagation loops.
package cycle

// Detector tracks visited factions during a single Taylor expansion
// path traversal to prevent cycles from causing infinite loops.
//
// A new Detector must be created for each expansion computation.
// Detector is not safe for concurrent use.
type Detector struct {
	visited map[string]int // factionID -> visit count
	maxVisits int
}

// New creates a Detector that allows each faction to be visited at most
// maxVisits times during a single expansion path traversal.
//
// Setting maxVisits to 1 prevents any cycle traversal.
// Setting maxVisits to 2 allows cycles to be entered once, which can
// be useful for modeling mutual reinforcement between allied factions.
// Most games should use maxVisits of 1.
func New(maxVisits int) *Detector {
	if maxVisits < 1 {
		maxVisits = 1
	}
	return &Detector{
		visited:   make(map[string]int),
		maxVisits: maxVisits,
	}
}

// CanVisit returns true if the given faction can be visited again
// without exceeding the maximum visit count.
func (d *Detector) CanVisit(factionID string) bool {
	return d.visited[factionID] < d.maxVisits
}

// Visit records a visit to the given faction.
func (d *Detector) Visit(factionID string) {
	d.visited[factionID]++
}

// Leave removes one visit record for the given faction.
// Used when backtracking through the graph.
func (d *Detector) Leave(factionID string) {
	if d.visited[factionID] > 0 {
		d.visited[factionID]--
	}
}

// Reset clears all visit records, allowing the detector to be reused
// for a new traversal without allocation.
func (d *Detector) Reset() {
	for k := range d.visited {
		delete(d.visited, k)
	}
}
