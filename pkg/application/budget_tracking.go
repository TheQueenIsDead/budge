package application

import (
	"math"
	"sort"
)

// TrackingSummary is the budget page's headline, totalled across categories,
// each measured over its own window and expressed per week.
type TrackingSummary struct {
	Actual    float64 // spend in categories with a target
	Target    float64
	OverCount int
	// Spend with no target to measure it against, the usual reason a budget
	// does not add up.
	UntargetedWeekly float64
	UntargetedCount  int
	// Targets set well above what their categories spend.
	GenerousCount int

	// Over names what is over target, by how much each week, worst first:
	// a category, or the subcategory of a split one that is. Untargeted
	// names the largest spending with no target, and UntargetedMore how many
	// more there are, for the notifications that say what to look at.
	Over           []TrackingItem
	Untargeted     []TrackingItem
	UntargetedMore int
}

// TrackingItem is a category the budget has something to say about, and
// how much a week it is about.
type TrackingItem struct {
	Label  string
	Weekly float64
}

// untargetedNamed is how many categories with no target are named before
// saying how many more there are.
const untargetedNamed = 3

// listed sorts items largest first, keeping the first limit and counting the
// rest. A limit of zero keeps them all.
func listed(items []TrackingItem, limit int) ([]TrackingItem, int) {
	sort.SliceStable(items, func(i, j int) bool { return items[i].Weekly > items[j].Weekly })
	if limit > 0 && len(items) > limit {
		return items[:limit], len(items) - limit
	}
	return items, 0
}

// IsOver reports whether spending in targeted categories is over their
// targets in total, allowing the same dollar a week each row is allowed.
func (t TrackingSummary) IsOver() bool {
	return t.Target > 0 && t.Actual > t.Target+overToleranceWeekly
}

// Gap is how far spending is from the targets each week: positive when over,
// negative when under.
func (t TrackingSummary) Gap() float64 { return t.Actual - t.Target }

// BarWidth is how full the headline bar is drawn, capped at full.
func (t TrackingSummary) BarWidth() float64 {
	if t.Target <= 0 {
		return 0
	}
	return math.Min(t.Actual/t.Target*100, 100)
}

func buildTrackingSummary(rows []CategoryTargetRow) TrackingSummary {
	var summary TrackingSummary
	for _, category := range rows {
		// A split category is judged part by part: each subcategory against
		// its own target, and one with none, its general bucket included, as
		// spending with no target, rather than all of it against theirs.
		lines := []CategoryTargetRow{category}
		if category.Split {
			lines = category.SubRows
		}
		for _, row := range lines {
			switch {
			case row.HasTarget:
				summary.Actual += row.ActualWeekly
				summary.Target += row.TargetWeekly
				if row.Generous {
					summary.GenerousCount++
				}
				if row.IsOver {
					summary.Over = append(summary.Over, TrackingItem{Label: row.Label, Weekly: row.ActualWeekly - row.TargetWeekly})
				}
			case row.NeedsTarget:
				summary.UntargetedWeekly += row.ActualWeekly
				summary.UntargetedCount++
				summary.Untargeted = append(summary.Untargeted, TrackingItem{Label: row.Label, Weekly: row.ActualWeekly})
			}
		}
	}
	summary.OverCount = len(summary.Over)
	summary.Over, _ = listed(summary.Over, 0)
	summary.Untargeted, summary.UntargetedMore = listed(summary.Untargeted, untargetedNamed)
	return summary
}
