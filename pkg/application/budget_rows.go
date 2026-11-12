package application

import (
	"math"
	"sort"
	"strings"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
)

// CategoryTargetRow is one budge category on the budget page.
type CategoryTargetRow struct {
	models.BudgetCategory
	RowID        string // HTML-safe id, also how an edit names the row to keep open
	Order        int    // the category's place in the registry, for sorting
	ActualWeekly float64
	// Populated from a saved BudgetItem, if one exists for the category.
	ItemID    string
	Target    float64
	Frequency string
	// Comparison fields, in the target's own frequency for display.
	ActualDisplay float64
	TargetDisplay float64
	Per           string // "a week", "a month"...
	Percentage    float64
	IsOver        bool
	HasTarget     bool
	NeedsTarget   bool    // has actual spend but no target set
	PctOfIncome   float64 // targetWeekly / netWeekly (fraction)
	TargetWeekly  float64 // weekly-normalised target for internal use
	// Budgeted is false for the categories that take no target: savings and
	// loan interest, which are not spending, and what is not filed yet.
	Budgeted    bool
	WindowWeeks int // how many weeks the actual is averaged over
	// Suggested is a target to offer a category that has none, in the
	// frequency its target picker shows, rounded up to whole dollars. It is
	// the higher of its 13 week average and its spend over the window that
	// frequency is measured over, so accepting it lands the row on track
	// rather than tripping over a busy few weeks.
	Suggested          float64
	SuggestedFrequency string
	SuggestedPer       string // "a week", "a month"...
	SuggestedWindow    int    // weeks the frequency is judged over
	// Generous marks a target set well above what the category spends, and
	// LoweredTarget is what it could come down to, in the target's own
	// frequency. Zero means the category spends nothing to budget for.
	Generous      bool
	LoweredTarget float64
	// Subs is the row's spending split by subcategory, over the same window.
	Subs []SubcategorySpend
	// Splittable is whether the category has subcategories to budget by
	// instead. Split marks one that does: SubRows are its subcategories, each
	// against its own target, and its general row for what is filed under the
	// category itself, marked General. ParentRowID names the row a
	// subcategory row sits in, which an edit to it redraws.
	Splittable  bool
	Split       bool
	SubRows     []CategoryTargetRow
	General     bool
	ParentRowID string
	// Merchants seen in this category, highest spend first. Informational
	// only — they are derived from transactions, never edited.
	Merchants []string
	Open      bool // the row being edited, drawn expanded
}

// SubcategorySpend is what one subcategory, or the category itself when
// nothing finer filed it, spent each week.
type SubcategorySpend struct {
	Label  string
	Weekly float64
}

// BarWidth is how full the row's progress bar is drawn, capped at full.
func (r CategoryTargetRow) BarWidth() float64 {
	return math.Min(r.Percentage*100, 100)
}

// topMerchants orders merchant names by total spend, highest first, so that
// truncating the list keeps the most significant ones. Ties break by name to
// keep rendering deterministic.
func topMerchants(spend map[string]float64) []string {
	if len(spend) == 0 {
		return nil
	}
	names := make([]string, 0, len(spend))
	for name := range spend {
		names = append(names, name)
	}
	sort.Slice(names, func(i, j int) bool {
		if spend[names[i]] != spend[names[j]] {
			return spend[names[i]] > spend[names[j]]
		}
		return names[i] < names[j]
	})
	return names
}

// A target counts as generous when spending is under this share of it and at
// least this far below it each week. Both have to hold, so a small yearly
// budget that has not been touched yet is not flagged for a few dollars.
const (
	generousShare     = 0.5
	generousGapWeekly = 20.0
)

// loweredHeadroom is added to what a generous category spends when offering
// a lower target, so a slightly bigger week does not tip it straight over.
const loweredHeadroom = 1.1

func buildSetupRowData(category models.BudgetCategory, item models.BudgetItem, actualWeekly, netWeekly float64) CategoryTargetRow {
	targetWeekly := weeklyTarget(item)
	budgeted := category.Budgeted()
	hasTarget := budgeted && targetWeekly > 0

	freq := item.Frequency
	if freq == "" {
		freq = "weekly"
	}

	var pct, pctOfIncome float64
	if hasTarget {
		pct = actualWeekly / targetWeekly
		if netWeekly > 0 {
			pctOfIncome = targetWeekly / netWeekly
		}
	} else {
		targetWeekly = 0
	}
	return CategoryTargetRow{
		BudgetCategory: category,
		RowID:          sanitizeID(category.Key),
		Order:          categoryOrder(category.Key),
		ActualWeekly:   actualWeekly,
		ItemID:         item.ID,
		Target:         item.Amount,
		Frequency:      item.Frequency,
		ActualDisplay:  actualInFrequency(actualWeekly, freq),
		TargetDisplay:  item.Amount,
		Per:            frequencyPer(freq),
		WindowWeeks:    actualWindowWeeks(item.Frequency),
		TargetWeekly:   targetWeekly,
		Percentage:     pct,
		PctOfIncome:    pctOfIncome,
		IsOver:         hasTarget && actualWeekly > targetWeekly+overToleranceWeekly,
		HasTarget:      hasTarget,
		NeedsTarget:    budgeted && !hasTarget && actualWeekly > 0,
		Budgeted:       budgeted,
	}
}

// actualWindowWeeks is how far back a category's actual spend is averaged,
// matched to its target's frequency so weekly categories reflect recent spend
// while yearly ones amortise their bills.
func actualWindowWeeks(frequency string) int {
	switch frequency {
	case "fortnightly":
		return 8
	case "monthly":
		return 13
	case "quarterly":
		// Two quarters, so a quarterly bill is caught twice rather than once
		// or not at all depending on when it lands.
		return 26
	case "yearly":
		return 52
	}
	return 4
}

// untargetedWindowWeeks is how far back a category with no target is measured,
// and so what a suggested target is based on. Long enough that one unusual
// month does not set the figure.
const untargetedWindowWeeks = 13

// overToleranceWeekly is how far past its target spend must go to count as
// over, so a target set to the dollar is not flagged for a few cents.
const overToleranceWeekly = 1.0

// markGenerous flags a target set well above what its category spends, and
// works out what it could be lowered to. Spending is judged on the higher of
// the row's own window and the last 13 weeks, so a quiet month does not flag
// a weekly target and a yearly one is not judged on its quiet weeks alone.
func markGenerous(row *CategoryTargetRow, recentWeekly float64) {
	if !row.HasTarget || !row.Budgeted {
		return
	}
	spent := math.Max(row.ActualWeekly, recentWeekly)
	if spent >= row.TargetWeekly*generousShare || row.TargetWeekly-spent < generousGapWeekly {
		return
	}
	row.Generous = true
	freq := row.Frequency
	if freq == "" {
		freq = "weekly"
	}
	row.LoweredTarget = math.Ceil(actualInFrequency(spent*loweredHeadroom, freq))
}

// suggestTarget offers a target for a row with none, in the row's own
// frequency. measuredWeekly is its spend over the window that frequency is
// judged on, which for a weekly target is only the last 4 weeks.
func suggestTarget(row *CategoryTargetRow, measuredWeekly float64) {
	freq := row.Frequency
	if freq == "" {
		freq = "weekly"
	}
	row.SuggestedFrequency = freq
	row.SuggestedPer = frequencyPer(freq)
	row.SuggestedWindow = actualWindowWeeks(freq)
	row.Suggested = math.Ceil(actualInFrequency(math.Max(row.ActualWeekly, measuredWeekly), freq))
}

// splitRow is a category budgeted by its subcategories. It is measured per
// week, since its subcategories can each have a different period: the sum of
// what they spend, plus anything filed under the category itself, against
// the sum of their targets. Its merchants are left to its subcategories,
// each listing its own, rather than repeated above them.
func splitRow(category models.BudgetCategory, subRows []CategoryTargetRow, netWeekly float64) CategoryTargetRow {
	row := CategoryTargetRow{
		BudgetCategory: category,
		RowID:          sanitizeID(category.Key),
		Order:          categoryOrder(category.Key),
		Budgeted:       true,
		Splittable:     true,
		Split:          true,
		SubRows:        subRows,
		Frequency:      "weekly",
		Per:            frequencyPer("weekly"),
		WindowWeeks:    untargetedWindowWeeks,
	}
	// The row shows everything the category spends, but is judged only on
	// what has a target, against those targets, as the tracking summary
	// judges it. Spending with no target has nothing to be over, so it is
	// not counted against the targets of the parts that do.
	var targetedWeekly float64
	for _, sub := range subRows {
		row.ActualWeekly += sub.ActualWeekly
		row.TargetWeekly += sub.TargetWeekly
		if sub.HasTarget {
			targetedWeekly += sub.ActualWeekly
		}
	}
	row.ActualDisplay = row.ActualWeekly
	row.TargetDisplay = row.TargetWeekly
	row.Target = row.TargetWeekly
	row.HasTarget = row.TargetWeekly > 0
	row.NeedsTarget = !row.HasTarget && row.ActualWeekly > 0
	row.IsOver = row.HasTarget && targetedWeekly > row.TargetWeekly+overToleranceWeekly
	if row.HasTarget {
		row.Percentage = targetedWeekly / row.TargetWeekly
		if netWeekly > 0 {
			row.PctOfIncome = row.TargetWeekly / netWeekly
		}
	}
	return row
}

// generalSuffix marks the key of a split category's general row, for what is
// filed under the category itself. It is not a category of its own.
const generalSuffix = ":general"

// generalKey is the key of a category's general row.
func generalKey(key string) string { return key + generalSuffix }

// generalCategory is a split category's general row, dressed as the category
// it belongs to.
func generalCategory(category models.BudgetCategory) models.BudgetCategory {
	general := category
	general.Key = generalKey(category.Key)
	general.Label = category.Label + " (general)"
	general.Blurb = "Filed under " + category.Label + " itself rather than one of its subcategories"
	return general
}

// categoryOrder is a category's place in the registry, which is the order
// the budget lists categories in.
func categoryOrder(key string) int {
	for i, category := range models.TopBudgetCategories() {
		if category.Key == key {
			return i
		}
	}
	return -1
}

// subcategorySpend splits a category's weekly spending by subcategory, over
// the window its row is measured over. Spending filed under the category
// itself is listed last, as "Other <category>". A category with nothing finer
// returns nothing, since the split would only repeat the row.
func subcategorySpend(category models.BudgetCategory, spend map[string]map[int]float64, weeks int) []SubcategorySpend {
	var subs []SubcategorySpend
	for _, sub := range models.BudgetSubcategories(category.Key) {
		if total := spend[sub.Key][weeks]; total > 0 {
			subs = append(subs, SubcategorySpend{Label: sub.Label, Weekly: total / float64(weeks)})
		}
	}
	if len(subs) == 0 {
		return nil
	}
	if direct := spend[category.Key][weeks]; direct > 0 {
		var filed float64
		for _, sub := range models.BudgetSubcategories(category.Key) {
			filed += spend[sub.Key][weeks]
		}
		if rest := direct - filed; rest > 0.005 {
			subs = append(subs, SubcategorySpend{Label: "Other " + strings.ToLower(category.Label), Weekly: rest / float64(weeks)})
		}
	}
	sort.SliceStable(subs, func(i, j int) bool { return subs[i].Weekly > subs[j].Weekly })
	return subs
}
