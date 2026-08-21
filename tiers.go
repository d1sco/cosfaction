package faction

import "fmt"

// TierWeight pairs a tier name with a relative width weight for use with
// WeightedTiers. Higher weights produce wider tiers.
type TierWeight struct {
	// Name is the human readable label for this tier.
	Name string

	// Weight is the relative width of this tier compared to others.
	// A tier with weight 2 is twice as wide as one with weight 1.
	// Must be greater than zero.
	Weight int
}

// EvenTiers constructs a slice of Tier values by dividing the disposition
// scale [dispositionMin, dispositionMax] evenly across the provided tier
// names, ordered from lowest to highest standing.
//
// dispositionMin and dispositionMax define the full extent of the disposition
// scale for your game. All player standings will be clamped to this range.
// Common choices are -1000 to 1000 or -100 to 100 depending on how granular
// your delta values are.
//
// Example — six equal tiers from -1000 to 1000:
//
//	tiers, err := faction.EvenTiers(-1000, 1000,
//	    "Outlawed",
//	    "Wanted",
//	    "Suspected",
//	    "Neutral",
//	    "Trusted",
//	    "Celebrated",
//	)
//
// Adding a tier is as simple as inserting a name:
//
//	tiers, err := faction.EvenTiers(-1000, 1000,
//	    "Outlawed",
//	    "Wanted",
//	    "Suspected",
//	    "Neutral",
//	    "Trusted",
//	    "Revered",      // inserted — boundaries recalculate automatically
//	    "Celebrated",
//	)
//
// Returns an error if fewer than one name is provided or if
// dispositionMin >= dispositionMax.
func EvenTiers(dispositionMin, dispositionMax int64, names ...string) ([]Tier, error) {
	if len(names) == 0 {
		return nil, fmt.Errorf("cosfaction: EvenTiers requires at least one tier name")
	}
	if dispositionMin >= dispositionMax {
		return nil, fmt.Errorf(
			"cosfaction: EvenTiers dispositionMin (%d) must be less than dispositionMax (%d)",
			dispositionMin, dispositionMax,
		)
	}

	weights := make([]TierWeight, len(names))
	for i, name := range names {
		weights[i] = TierWeight{Name: name, Weight: 1}
	}

	return weightedTiers(dispositionMin, dispositionMax, weights)
}

// WeightedTiers constructs a slice of Tier values by dividing the disposition
// scale [dispositionMin, dispositionMax] proportionally according to each
// tier's weight, ordered from lowest to highest standing.
//
// dispositionMin and dispositionMax define the full extent of the disposition
// scale for your game. All player standings will be clamped to this range.
//
// A tier with weight 2 is twice as wide as one with weight 1. This is
// useful when you want the neutral zone to be wider (making standing feel
// stable) or the hostile zones to be narrower (making them hard to reach).
//
// Example — six tiers with a wider Neutral zone:
//
//	tiers, err := faction.WeightedTiers(-1000, 1000, []faction.TierWeight{
//	    {Name: "Outlawed",   Weight: 1},
//	    {Name: "Wanted",     Weight: 1},
//	    {Name: "Suspected",  Weight: 1},
//	    {Name: "Neutral",    Weight: 3},  // three times wider than others
//	    {Name: "Trusted",    Weight: 2},
//	    {Name: "Celebrated", Weight: 1},
//	})
//
// Returns an error if fewer than one weight is provided, if
// dispositionMin >= dispositionMax, or if any weight is less than or
// equal to zero.
func WeightedTiers(dispositionMin, dispositionMax int64, weights []TierWeight) ([]Tier, error) {
	if len(weights) == 0 {
		return nil, fmt.Errorf("cosfaction: WeightedTiers requires at least one tier")
	}
	if dispositionMin >= dispositionMax {
		return nil, fmt.Errorf(
			"cosfaction: WeightedTiers dispositionMin (%d) must be less than dispositionMax (%d)",
			dispositionMin, dispositionMax,
		)
	}
	for i, w := range weights {
		if w.Weight <= 0 {
			return nil, fmt.Errorf(
				"cosfaction: WeightedTiers tier %d (%q) has non-positive weight %d",
				i, w.Name, w.Weight,
			)
		}
	}
	return weightedTiers(dispositionMin, dispositionMax, weights)
}

// weightedTiers is the shared implementation for EvenTiers and WeightedTiers.
func weightedTiers(dispositionMin, dispositionMax int64, weights []TierWeight) ([]Tier, error) {
	totalWeight := 0
	for _, w := range weights {
		totalWeight += w.Weight
	}

	span := dispositionMax - dispositionMin
	tiers := make([]Tier, len(weights))
	cursor := dispositionMin

	for i, w := range weights {
		// Compute this tier's width as a proportion of total weight.
		// Use integer arithmetic with rounding to avoid floating point drift.
		var width int64
		if i == len(weights)-1 {
			// Last tier gets whatever remains to avoid rounding gaps.
			width = dispositionMax - cursor
		} else {
			width = int64(float64(span) * float64(w.Weight) / float64(totalWeight))
			if width < 1 {
				width = 1
			}
		}

		tiers[i] = Tier{
			Name:     w.Name,
			MinValue: cursor,
			MaxValue: cursor + width - 1,
		}
		cursor += width
	}

	// Ensure the last tier reaches exactly dispositionMax regardless of rounding.
	tiers[len(tiers)-1].MaxValue = dispositionMax

	return tiers, nil
}
