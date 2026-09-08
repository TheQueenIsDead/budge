package application

import (
	"fmt"
	"math"
	"sort"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
)

// Cadence is the size of the bucket a spend group is totalled into. Akahu
// feeds arrive per transaction, so the question "what does a week of food
// cost?" is only answerable once transactions are folded into periods.
type Cadence string

const (
	CadenceWeekly  Cadence = "weekly"
	CadenceMonthly Cadence = "monthly"
)

// Valid reports whether the cadence is one this package knows how to bucket,
// so a hand-edited query string falls back rather than rendering an empty page.
func (c Cadence) Valid() bool {
	return c == CadenceWeekly || c == CadenceMonthly
}

// Label names the cadence in the singular, for prose like "vs last week".
func (c Cadence) Label() string {
	if c == CadenceWeekly {
		return "week"
	}
	return "month"
}

// Periods is how much history a cadence shows by default: a quarter of weeks,
// or a year of months. Both fit a phone screen without horizontal scrolling.
func (c Cadence) Periods() int {
	if c == CadenceWeekly {
		return 13
	}
	return 12
}

// SpendGroup is a bundle of Akahu categories that answers a recurring question
// ("how much petrol did I buy?"). Akahu's category names are specific enough to
// classify on directly, so a group is defined by the exact category strings the
// feed uses rather than by pattern matching merchant names, which change.
type SpendGroup struct {
	Key     string
	Label   string
	Blurb   string
	Icon    string
	Accent  string
	Cadence Cadence

	// Utility marks a group as a household running cost, so the insights page
	// can total power, internet and mobile as one "utilities" figure.
	Utility bool

	// RollingDays makes the dashboard tile read a trailing window rather than a
	// calendar one. A calendar week shows $0 every Monday morning, which answers
	// nothing and compares a part period against a whole one. Groups spent in
	// many small amounts get a rolling window; groups billed once a period keep
	// the calendar, because a trailing window would catch zero bills or two.
	RollingDays int

	Categories []string
}

// spendGroups is the ordered registry. Order is display order: the questions
// asked most often come first.
var spendGroups = []SpendGroup{
	{
		Key:         "groceries",
		Label:       "Groceries",
		Blurb:       "Supermarkets, dairies and food shops",
		Icon:        "bi-basket",
		Accent:      "sage",
		Cadence:     CadenceWeekly,
		RollingDays: 7,
		Categories: []string{
			"Supermarkets and grocery stores",
			"Convenience stores",
			"Specialty food stores",
			"Bakeries",
			"Meat supplies",
		},
	},
	{
		Key:         "petrol",
		Label:       "Petrol",
		Blurb:       "Fuel stations",
		Icon:        "bi-fuel-pump",
		Accent:      "clay",
		Cadence:     CadenceMonthly,
		RollingDays: 30,
		Categories:  []string{"Fuel stations"},
	},
	{
		Key:        "power",
		Label:      "Power",
		Blurb:      "Electricity and gas retailers",
		Icon:       "bi-lightning-charge",
		Accent:     "plum",
		Cadence:    CadenceMonthly,
		Utility:    true,
		Categories: []string{"Electricity services", "Gas services"},
	},
	{
		Key:        "connectivity",
		Label:      "Internet & Mobile",
		Blurb:      "Broadband and phone plans",
		Icon:       "bi-wifi",
		Accent:     "slate",
		Cadence:    CadenceMonthly,
		Utility:    true,
		Categories: []string{"Internet services", "Telecommunication services"},
	},
	{
		Key:         "eating-out",
		Label:       "Eating out",
		Blurb:       "Cafes, takeaways and bars",
		Icon:        "bi-cup-hot",
		Accent:      "brick",
		Cadence:     CadenceWeekly,
		RollingDays: 7,
		Categories: []string{
			"Cafes and restaurants",
			"Fast food stores",
			"Bars, pubs, nightclubs",
			"Caterers",
			"Ice cream, gelato, nut, and confectionary stores",
		},
	},
}

// categoryToGroup indexes every category string to its group key. Categories
// are lower cased on both sides so a change of casing upstream cannot silently
// drop a category out of its group.
var categoryToGroup = func() map[string]string {
	index := make(map[string]string)
	for _, group := range spendGroups {
		for _, category := range group.Categories {
			index[strings.ToLower(category)] = group.Key
		}
	}
	return index
}()

// SpendGroups returns the tracked groups in display order.
func SpendGroups() []SpendGroup {
	out := make([]SpendGroup, len(spendGroups))
	copy(out, spendGroups)
	return out
}

// SpendGroupByKey looks up a single group, reporting whether it exists.
func SpendGroupByKey(key string) (SpendGroup, bool) {
	for _, group := range spendGroups {
		if group.Key == key {
			return group, true
		}
	}
	return SpendGroup{}, false
}

// ClassifySpend returns the spend group a transaction belongs to. Transfers and
// money coming in are never spend, so they match nothing.
func ClassifySpend(tx models.Transaction) (string, bool) {
	if tx.Type == "TRANSFER" || tx.Amount >= 0 {
		return "", false
	}
	key, ok := categoryToGroup[strings.ToLower(tx.Category.Name)]
	return key, ok
}

// SpendBucket is one period of a group's spend. Total is a positive magnitude:
// spend reads more naturally counted up than as a negative balance movement.
type SpendBucket struct {
	Label string
	Start time.Time
	End   time.Time
	Total float64
	Count int

	// Partial marks the period containing "now", which has not finished
	// accruing. Comparing a part week against a whole one understates it, so
	// the UI labels this bucket rather than quietly charting it alongside.
	Partial bool
}

// SpendBar is a bucket scaled for the CSS bar charts on the dashboard tiles.
type SpendBar struct {
	SpendBucket

	// Pct is the bar's height as a percentage of the tallest bucket in the series.
	Pct float64

	// Compare marks the last completed period, which the headline delta is
	// measured against, so the chart can show what it is pointing at.
	Compare bool
}

// SpendSummary is everything the UI needs to answer "how is this tracking?"
// for one group at one cadence.
type SpendSummary struct {
	Group   SpendGroup
	Cadence Cadence

	// Current is the in-progress period, Previous the last completed one.
	// Comparing against the last completed period rather than the one before
	// "now" keeps the delta stable as the current period fills up.
	Current  float64
	Previous float64
	Delta    float64

	// HasPrevious is false when there is no completed prior period to compare
	// against, in which case Delta is meaningless and must not be shown.
	HasPrevious bool

	// Average is the mean of the completed periods in the window, which is a
	// steadier reference than a single previous period for lumpy spend.
	Average float64

	// Rolling is the length in days of a trailing window, or zero when the
	// summary is bucketed on calendar boundaries.
	Rolling int

	// Headline is the figure a dashboard tile should lead with. It is normally
	// the current period, but a group billed once a month reads $0 until the
	// bill lands, and "what is my power bill?" is not answered by $0. In that
	// case the headline falls back to the last period that actually has a
	// reading, and HeadlineIsPrevious says so, so the label can be honest.
	Headline           float64
	HeadlinePrevious   float64
	HeadlineDelta      float64
	HasHeadlineDelta   bool
	HeadlineIsPrevious bool

	Buckets []SpendBucket
}

// HeadlineLabel names the period the headline figure covers.
func (s SpendSummary) HeadlineLabel() string {
	if s.HeadlineIsPrevious {
		return s.PreviousLabel()
	}
	return s.CurrentLabel()
}

// HeadlineNote completes the sentence "vs $12.34 ...".
func (s SpendSummary) HeadlineNote() string {
	if s.Rolling > 0 {
		return fmt.Sprintf("the %d days before", s.Rolling)
	}
	if s.HeadlineIsPrevious {
		return "the " + s.Cadence.Label() + " before"
	}
	return "last " + s.Cadence.Label()
}

// CurrentLabel names the period on show. A trailing window is described by its
// length, because "this week" would be a lie about what it covers.
func (s SpendSummary) CurrentLabel() string {
	if s.Rolling > 0 {
		return fmt.Sprintf("Last %d days", s.Rolling)
	}
	return "This " + s.Cadence.Label()
}

// PreviousLabel names the period being compared against.
func (s SpendSummary) PreviousLabel() string {
	if s.Rolling > 0 {
		return fmt.Sprintf("Previous %d days", s.Rolling)
	}
	return "Last " + s.Cadence.Label()
}

// ComparisonNote completes the sentence "vs $12.34 ...".
func (s SpendSummary) ComparisonNote() string {
	if s.Rolling > 0 {
		return fmt.Sprintf("the %d days before", s.Rolling)
	}
	return "last " + s.Cadence.Label()
}

// periodStart truncates a time to the start of its bucket. Weeks start Monday,
// which is how a "weekly shop" is actually lived.
func periodStart(t time.Time, cadence Cadence) time.Time {
	year, month, day := t.Date()
	if cadence == CadenceWeekly {
		offset := (int(t.Weekday()) + 6) % 7
		return time.Date(year, month, day-offset, 0, 0, 0, 0, t.Location())
	}
	return time.Date(year, month, 1, 0, 0, 0, 0, t.Location())
}

// periodAdd steps n buckets forward (or back, when negative) from a start.
func periodAdd(start time.Time, cadence Cadence, n int) time.Time {
	if cadence == CadenceWeekly {
		return start.AddDate(0, 0, 7*n)
	}
	return start.AddDate(0, n, 0)
}

// periodLabel names a bucket as briefly as it can while staying unambiguous.
func periodLabel(start time.Time, cadence Cadence) string {
	if cadence == CadenceWeekly {
		return start.Format("2 Jan")
	}
	return start.Format("Jan 06")
}

// BuildSpendBuckets totals a group's transactions into the last `periods`
// buckets ending with the one containing `now`. Empty periods are kept, so a
// fortnight with no shop shows as a gap instead of vanishing from the series.
func BuildSpendBuckets(transactions []models.Transaction, group SpendGroup, cadence Cadence, periods int, now time.Time) []SpendBucket {
	if periods <= 0 {
		return nil
	}
	if !cadence.Valid() {
		cadence = group.Cadence
	}

	// Every date is read in the reference time's location before it is bucketed.
	// Akahu stores transaction dates in UTC while time.Now() is local, and a
	// time.Time map key compares its location as well as its instant, so mixing
	// the two silently drops every transaction on the floor. Local boundaries
	// are also the correct ones: a shop at 11pm belongs to the day it felt like.
	location := now.Location()

	current := periodStart(now, cadence)
	first := periodAdd(current, cadence, -(periods - 1))

	buckets := make([]SpendBucket, periods)
	index := make(map[time.Time]int, periods)
	for i := range buckets {
		start := periodAdd(first, cadence, i)
		buckets[i] = SpendBucket{
			Label:   periodLabel(start, cadence),
			Start:   start,
			End:     periodAdd(start, cadence, 1),
			Partial: start.Equal(current),
		}
		index[start] = i
	}

	for _, tx := range transactions {
		key, ok := ClassifySpend(tx)
		if !ok || key != group.Key {
			continue
		}
		start := periodStart(tx.Date.In(location), cadence)
		i, ok := index[start]
		if !ok {
			continue
		}
		buckets[i].Total += -tx.Amount
		buckets[i].Count++
	}

	return buckets
}

// BuildSpendSummary folds a group's transactions into the comparison the
// dashboard and insights pages both render.
func BuildSpendSummary(transactions []models.Transaction, group SpendGroup, cadence Cadence, now time.Time) SpendSummary {
	if !cadence.Valid() {
		cadence = group.Cadence
	}

	buckets := BuildSpendBuckets(transactions, group, cadence, cadence.Periods(), now)
	summary := SpendSummary{
		Group:   group,
		Cadence: cadence,
		Buckets: buckets,
	}
	if len(buckets) == 0 {
		return summary
	}

	summary.Current = buckets[len(buckets)-1].Total

	// Everything before the in-progress period is complete, and only complete
	// periods are fair to compare or average against.
	completed := buckets[:len(buckets)-1]
	if len(completed) == 0 {
		return summary
	}

	summary.Previous = completed[len(completed)-1].Total
	summary.HasPrevious = true
	if summary.Previous != 0 {
		summary.Delta = (summary.Current - summary.Previous) / math.Abs(summary.Previous)
	}

	var total float64
	for _, bucket := range completed {
		total += bucket.Total
	}
	summary.Average = total / float64(len(completed))

	summary.setHeadline(completed)

	return summary
}

// setHeadline picks the figure a tile leads with. A period still waiting on its
// bill is skipped in favour of the last one that has a reading.
func (s *SpendSummary) setHeadline(completed []SpendBucket) {
	s.Headline = s.Current
	s.HeadlinePrevious = s.Previous
	s.HasHeadlineDelta = s.HasPrevious && s.Previous != 0
	if s.HasHeadlineDelta {
		s.HeadlineDelta = s.Delta
	}

	if !s.Awaiting() {
		return
	}

	s.HeadlineIsPrevious = true
	s.Headline = s.Previous
	s.HeadlinePrevious = 0
	s.HasHeadlineDelta = false
	s.HeadlineDelta = 0

	if len(completed) < 2 {
		return
	}
	previous := completed[len(completed)-2].Total
	s.HeadlinePrevious = previous
	if previous != 0 {
		s.HasHeadlineDelta = true
		s.HeadlineDelta = (s.Headline - previous) / math.Abs(previous)
	}
}

// Awaiting reports that the current period has not been billed yet: it is still
// in progress and nothing has landed. Monthly bills arrive on one day, so early
// in the month the delta would otherwise read as a triumphant 100% drop.
func (s SpendSummary) Awaiting() bool {
	// A trailing window is always complete, so an empty one is a real result
	// rather than a bill that has not landed.
	if s.Rolling > 0 {
		return false
	}
	return s.Current == 0 && s.HasPrevious && s.Previous > 0
}

// AwaitingLabel words the empty-period chip for the kind of spending it is.
// A power bill has not been billed yet; petrol simply has not been bought.
func (s SpendSummary) AwaitingLabel() string {
	if s.Group.Utility {
		return "Not billed yet"
	}
	return "No spend yet"
}

// Bars scales the series against its tallest bucket for the CSS bar charts.
// A group with no spend at all returns bars of zero height rather than
// dividing by zero.
func (s SpendSummary) Bars() []SpendBar {
	var max float64
	for _, bucket := range s.Buckets {
		if bucket.Total > max {
			max = bucket.Total
		}
	}

	bars := make([]SpendBar, len(s.Buckets))
	for i, bucket := range s.Buckets {
		bars[i] = SpendBar{SpendBucket: bucket}
		if max > 0 {
			bars[i].Pct = bucket.Total / max * 100
		}
	}
	if len(bars) >= 2 {
		bars[len(bars)-2].Compare = true
	}
	return bars
}

// Labels and Totals feed Chart.js, which wants the series split into parallel
// arrays rather than a list of structs.
func (s SpendSummary) Labels() []string {
	labels := make([]string, len(s.Buckets))
	for i, bucket := range s.Buckets {
		labels[i] = bucket.Label
	}
	return labels
}

func (s SpendSummary) Totals() []float64 {
	totals := make([]float64, len(s.Buckets))
	for i, bucket := range s.Buckets {
		totals[i] = math.Round(bucket.Total*100) / 100
	}
	return totals
}

// Window is the span the summary covers, for the "since" line on the page.
func (s SpendSummary) Window() (start, end time.Time) {
	if len(s.Buckets) == 0 {
		return
	}
	return s.Buckets[0].Start, s.Buckets[len(s.Buckets)-1].End
}

// BuildSpendMerchants ranks the merchants inside a group over the summary's
// window. This is what turns "power costs $X" into "power is Mercury", and is
// how a merchant gets identified as a utility without manual tagging.
func BuildSpendMerchants(transactions []models.Transaction, group SpendGroup, start, end time.Time, n int) []models.MerchantTotal {
	totals := make(map[string]float64)
	for _, tx := range transactions {
		key, ok := ClassifySpend(tx)
		if !ok || key != group.Key {
			continue
		}
		if tx.Date.Before(start) || !tx.Date.Before(end) {
			continue
		}
		name := tx.Merchant.Name
		if name == "" {
			name = tx.Description
		}
		if name == "" {
			continue
		}
		totals[name] += -tx.Amount
	}

	results := make([]models.MerchantTotal, 0, len(totals))
	for merchant, total := range totals {
		results = append(results, models.MerchantTotal{Merchant: merchant, Total: total})
	}
	sort.Slice(results, func(i, j int) bool {
		if results[i].Total == results[j].Total {
			return results[i].Merchant < results[j].Merchant
		}
		return results[i].Total > results[j].Total
	})

	if n > 0 && len(results) > n {
		results = results[:n]
	}
	return results
}

// startOfDay is the local midnight that a date falls on.
func startOfDay(t time.Time, location *time.Location) time.Time {
	year, month, day := t.In(location).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, location)
}

// BuildRollingSummary totals a group into trailing windows of `days`, the last
// of which ends at the end of today. Unlike calendar buckets every window is a
// complete one, so the comparison is always like for like and the headline
// figure is never an empty Monday morning.
func BuildRollingSummary(transactions []models.Transaction, group SpendGroup, days, periods int, now time.Time) SpendSummary {
	summary := SpendSummary{Group: group, Cadence: group.Cadence, Rolling: days}
	if days <= 0 || periods <= 0 {
		return summary
	}

	location := now.Location()
	// Windows close at the end of today, so today counts and the boundaries do
	// not creep as the clock moves through the day.
	end := startOfDay(now, location).AddDate(0, 0, 1)

	buckets := make([]SpendBucket, periods)
	for i := range buckets {
		stop := end.AddDate(0, 0, -days*(periods-1-i))
		start := stop.AddDate(0, 0, -days)
		buckets[i] = SpendBucket{
			Label: start.Format("2 Jan"),
			Start: start,
			End:   stop,
		}
	}

	for _, tx := range transactions {
		key, ok := ClassifySpend(tx)
		if !ok || key != group.Key {
			continue
		}
		when := tx.Date.In(location)
		for i := range buckets {
			if !when.Before(buckets[i].Start) && when.Before(buckets[i].End) {
				buckets[i].Total += -tx.Amount
				buckets[i].Count++
				break
			}
		}
	}

	summary.Buckets = buckets
	summary.Current = buckets[periods-1].Total

	if periods < 2 {
		return summary
	}

	summary.Previous = buckets[periods-2].Total
	summary.HasPrevious = true
	if summary.Previous != 0 {
		summary.Delta = (summary.Current - summary.Previous) / math.Abs(summary.Previous)
	}

	// "Typical" describes what came before, not the window being judged.
	var total float64
	for _, bucket := range buckets[:periods-1] {
		total += bucket.Total
	}
	summary.Average = total / float64(periods-1)

	summary.setHeadline(buckets[:periods-1])

	return summary
}

// BuildSpendSummaries builds one summary per requested group, at each group's
// own natural cadence, which is what the dashboard tiles want.
func BuildSpendSummaries(transactions []models.Transaction, keys []string, now time.Time) []SpendSummary {
	summaries := make([]SpendSummary, 0, len(keys))
	for _, key := range keys {
		group, ok := SpendGroupByKey(key)
		if !ok {
			continue
		}
		if group.RollingDays > 0 {
			summaries = append(summaries, BuildRollingSummary(transactions, group, group.RollingDays, group.Cadence.Periods(), now))
			continue
		}
		summaries = append(summaries, BuildSpendSummary(transactions, group, group.Cadence, now))
	}
	return summaries
}
