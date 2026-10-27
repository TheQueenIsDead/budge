package application

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// The timeline answers "what did this year's money actually go towards?" It
// leaves out the weekly shop and the power bill and keeps the purchases that
// stand out, then lets the owner gather those into memories: a holiday, a goal
// reached, the couch that took six months to save for.

const (
	// notableFloor is the least a purchase has to cost to be worth a place on
	// the timeline, however frugal the year. Below it the list fills with
	// takeaways.
	notableFloor = 100.0

	// notablePercentile is where in the year's spending a purchase has to sit
	// to count as notable. Scaling with spend keeps the timeline a handful of
	// entries a month whatever the household's budget.
	notablePercentile = 0.9

	// outlierMultiple marks a purchase as an outlier once it costs this many
	// times the notable threshold: the purchases that make a month look odd.
	outlierMultiple = 3.0

	// recurringMonths is how many separate months a merchant has to appear in
	// before it is routine. Rent, insurance and the mortgage are big, but they
	// are not memories.
	recurringMonths = 4

	// routineMultiple lets a routine merchant back onto the timeline when one
	// payment is well above its usual and is an outlier besides: the annual
	// insurance renewal, or the bond on a new flat. Being above its usual alone
	// is not enough, or every big weekly shop would make the list.
	routineMultiple = 2.0

	// tripGapDays is the longest run of days without an overseas purchase that
	// still counts as one trip.
	tripGapDays = 4

	// tripMinPurchases is how many overseas purchases make a trip. A lone USD
	// charge is an online subscription, not a holiday.
	tripMinPurchases = 3

	// tripPlaceShare is the share of a trip's overseas purchases a second
	// country needs before it is named alongside the first.
	tripPlaceShare = 0.25

	// tripLeadDays is how far ahead of a trip its flights and accommodation may
	// have been booked and still be pulled into it.
	tripLeadDays = 180
)

// Timeline entry types.
const (
	TimelineMemory     = "memory"
	TimelineSuggestion = "suggestion"
	TimelinePurchase   = "purchase"
)

// Timeline views.
const (
	TimelineViewAll      = "all"
	TimelineViewMemories = "memories"
	TimelineViewHidden   = "hidden"
)

// TimelineEntry is one card on the timeline: a saved memory, a group the
// timeline thinks belongs together, or a single notable purchase.
type TimelineEntry struct {
	Type string

	// Key identifies the entry to the actions on it: a memory ID, a suggestion
	// key, or a transaction ID.
	Key string

	Title  string
	Note   string
	Kind   models.MemoryKind
	Icon   string
	Accent string

	Start time.Time
	End   time.Time

	// Total is what the entry cost as a positive magnitude. Refunds inside a
	// memory reduce it.
	Total float64
	Rows  []TransactionRow

	// Outlier marks a purchase far beyond the usual notable spend, and Multiple
	// says how far.
	Outlier  bool
	Multiple float64

	// Reason explains why a suggestion was made.
	Reason string

	// Merchant names who a single purchase was from, when the bank feed knows,
	// so everything from them can be hidden at once.
	Merchant string

	// SameDay is what else was bought the day of a single purchase, biggest
	// first. A bank feed says where money went but never what it bought, and
	// the rest of the day is often what jogs the memory: Bunnings, then Mitre
	// 10 the same afternoon, was the deck. SameDayMore counts what did not fit.
	SameDay     []TimelineContextRow
	SameDayMore int

	// Nearby is what else was bought on and around a memory's days, offered
	// while editing it so whatever was missed can be added.
	Nearby []TimelineContextRow
}

// TimelineContextRow is a purchase shown for context. Claimed marks one that
// is already in a memory, which cannot be picked for another from here.
type TimelineContextRow struct {
	TransactionRow
	Claimed bool
}

// TimelineMemoryOption is a memory that purchases can be added to.
type TimelineMemoryOption struct {
	Id    string
	Label string
	end   time.Time
}

// nearbyLimit caps the purchases offered while editing a memory. A long trip
// has a lot of days around it, and past this the biggest are what matter.
const nearbyLimit = 30

// sameDayLimit is how many of a day's other purchases are listed under a
// notable one. Past a handful the line stops being a hint and becomes a
// statement.
const sameDayLimit = 5

// Date is where the entry sits on the timeline. A memory is placed when it
// ended, which is when it was achieved.
func (e TimelineEntry) Date() time.Time {
	return e.End
}

// Span words the dates an entry covers as briefly as it can.
func (e TimelineEntry) Span() string {
	switch {
	case e.Start.IsZero() || models.SameDay(e.Start, e.End):
		return e.End.Format("Mon 2 Jan")
	case e.Start.Year() != e.End.Year():
		return e.Start.Format("2 Jan 2006") + " – " + e.End.Format("2 Jan 2006")
	case e.Start.Month() == e.End.Month():
		return e.Start.Format("2") + " – " + e.End.Format("2 Jan")
	default:
		return e.Start.Format("2 Jan") + " – " + e.End.Format("2 Jan")
	}
}

// TransactionIDs lists the transactions behind the entry, for the forms that
// save or hide it.
func (e TimelineEntry) TransactionIDs() []string {
	ids := make([]string, len(e.Rows))
	for i, row := range e.Rows {
		ids[i] = row.Id
	}
	return ids
}

// TimelineMonth gathers the entries that fell in one month, newest first.
type TimelineMonth struct {
	Month   time.Time
	Total   float64
	Entries []TimelineEntry
}

// Timeline is everything the page shows for one window.
type Timeline struct {
	Memories    []TimelineEntry
	Suggestions []TimelineEntry
	Purchases   []TimelineEntry

	// MemoryOptions lists every memory, whatever the window, newest first, so
	// a purchase can join a memory that started in another year.
	MemoryOptions []TimelineMemoryOption

	// Hidden lists what the owner has hidden, so it can be restored.
	Hidden          []TransactionRow
	HiddenMerchants []string
	Dismissed       int

	// Threshold is what a purchase had to cost to be notable in this window.
	Threshold float64
}

// MemoryTotal is what the window's memories cost between them.
func (t Timeline) MemoryTotal() float64 {
	total := 0.0
	for _, memory := range t.Memories {
		total += memory.Total
	}
	return total
}

// Biggest is the single most expensive memory, or failing that the most
// expensive purchase, so the summary always has something to lead with.
// It is nil when the window holds neither.
func (t Timeline) Biggest() *TimelineEntry {
	pool := t.Memories
	if len(pool) == 0 {
		pool = t.Purchases
	}
	if len(pool) == 0 {
		return nil
	}
	biggest := pool[0]
	for _, entry := range pool[1:] {
		if entry.Total > biggest.Total {
			biggest = entry
		}
	}
	return &biggest
}

// Months folds the entries a view shows into months, newest first.
func (t Timeline) Months(view string, location *time.Location) []TimelineMonth {
	entries := slices.Clone(t.Memories)
	if view != TimelineViewMemories {
		entries = append(entries, t.Suggestions...)
		entries = append(entries, t.Purchases...)
	}
	sort.SliceStable(entries, func(i, j int) bool {
		return entries[i].Date().After(entries[j].Date())
	})

	var months []TimelineMonth
	for _, entry := range entries {
		month := periodStart(entry.Date().In(location), CadenceMonthly)
		if len(months) == 0 || !months[len(months)-1].Month.Equal(month) {
			months = append(months, TimelineMonth{Month: month})
		}
		current := &months[len(months)-1]
		current.Total += entry.Total
		current.Entries = append(current.Entries, entry)
	}
	return months
}

// timelineSpend reports whether a transaction is money spent on something.
// Transfers and cash withdrawals move money without buying anything, and
// interest is a cost of borrowing rather than a purchase. Akahu's "CREDIT
// CARD" type is not a repayment: banks use it for purchases made by card,
// debit cards on everyday accounts included, and leaving it out dropped
// nearly every card purchase from the timeline. Paying off a card is moved
// between the owner's accounts, which the sync already marks as a transfer.
func timelineSpend(tx models.Transaction) bool {
	if tx.Amount >= 0 {
		return false
	}
	switch tx.Type {
	case "TRANSFER", "ATM", "INTEREST", "LOAN":
		return false
	}
	return true
}

// cashWithdrawal reports whether a transaction took cash out of an account.
func cashWithdrawal(tx models.Transaction) bool {
	return tx.Type == "ATM" && tx.Amount < 0
}

// billCategories are the obligations of running a household. They are never
// memories, however large, and are left off even when they are paid too
// irregularly to look routine: council rates land three or four times a year
// on no fixed day, so they never show the monthly pattern a routine merchant
// does.
var billCategories = []string{"local government", "insurance", "government services", "tax"}

// billPayment reports whether a transaction is a household bill rather than a
// purchase. The utility spend groups count too, so power and broadband are
// bills here for the same reason they are utilities on the insights page.
func billPayment(tx models.Transaction) bool {
	category := strings.ToLower(tx.Category.Name)
	if category == "" {
		return false
	}
	for _, bill := range billCategories {
		if strings.Contains(category, bill) {
			return true
		}
	}
	if key, ok := ClassifySpend(tx); ok {
		if group, found := SpendGroupByKey(key); found && group.Utility {
			return true
		}
	}
	return false
}

// merchantKey names who was paid, for spotting the same merchant month after
// month.
func merchantKey(tx models.Transaction) string {
	if tx.Merchant.Name != "" {
		return strings.ToLower(tx.Merchant.Name)
	}
	return strings.ToLower(strings.Join(strings.Fields(tx.Description), " "))
}

// notableThreshold is what a purchase must cost to stand out from the rest of
// the window's spending.
func notableThreshold(spend []models.Transaction) float64 {
	if len(spend) == 0 {
		return notableFloor
	}
	amounts := make([]float64, len(spend))
	for i, tx := range spend {
		amounts[i] = -tx.Amount
	}
	sort.Float64s(amounts)
	return math.Max(notableFloor, amounts[int(float64(len(amounts)-1)*notablePercentile)])
}

// routineMerchants finds the merchants paid in enough separate months to be
// part of ordinary life, with the median of what each was paid.
func routineMerchants(spend []models.Transaction, location *time.Location) map[string]float64 {
	months := map[string]map[time.Time]bool{}
	amounts := map[string][]float64{}
	for _, tx := range spend {
		key := merchantKey(tx)
		if months[key] == nil {
			months[key] = map[time.Time]bool{}
		}
		months[key][periodStart(tx.Date.In(location), CadenceMonthly)] = true
		amounts[key] = append(amounts[key], -tx.Amount)
	}

	routine := map[string]float64{}
	for key, seen := range months {
		if len(seen) < recurringMonths {
			continue
		}
		values := amounts[key]
		sort.Float64s(values)
		routine[key] = values[len(values)/2]
	}
	return routine
}

// currencyPlaces names where a foreign currency is spent. Euros cover too many
// countries to name one, so they name the continent.
var currencyPlaces = map[string]string{
	"AUD": "Australia", "USD": "the United States", "GBP": "the United Kingdom",
	"EUR": "Europe", "SGD": "Singapore", "JPY": "Japan", "THB": "Thailand",
	"FJD": "Fiji", "IDR": "Indonesia", "VND": "Vietnam", "MYR": "Malaysia",
	"HKD": "Hong Kong", "CNY": "China", "KRW": "South Korea", "CAD": "Canada",
	"CHF": "Switzerland", "PHP": "the Philippines", "INR": "India",
	"XPF": "French Polynesia", "WST": "Samoa", "TOP": "Tonga", "AED": "the UAE",
	"TWD": "Taiwan", "MXN": "Mexico", "ZAR": "South Africa",
}

// countryCodes names the countries card descriptions are tagged with, as in
// "HAWKER CENTRE SINGAPORE SG". New Zealand is deliberately missing: home is
// not a trip.
var countryCodes = map[string]string{
	"AU": "Australia", "US": "the United States", "GB": "the United Kingdom",
	"SG": "Singapore", "JP": "Japan", "TH": "Thailand", "FJ": "Fiji",
	"ID": "Indonesia", "VN": "Vietnam", "MY": "Malaysia", "HK": "Hong Kong",
	"CN": "China", "KR": "South Korea", "CA": "Canada", "FR": "France",
	"DE": "Germany", "IT": "Italy", "ES": "Spain", "PT": "Portugal",
	"NL": "the Netherlands", "GR": "Greece", "IE": "Ireland", "CH": "Switzerland",
	"AT": "Austria", "PH": "the Philippines", "IN": "India", "PF": "French Polynesia",
	"WS": "Samoa", "TO": "Tonga", "CK": "the Cook Islands", "NC": "New Caledonia",
	"VU": "Vanuatu", "AE": "the UAE", "TW": "Taiwan", "MX": "Mexico",
	"ZA": "South Africa", "HR": "Croatia", "TR": "Turkey",
}

// conversionNote matches the conversion some banks write into the description
// of a foreign purchase, as in "GENEVA CH119.88EUR @ 0.4954 conversion rate
// (INC. $4.48 INTERNATIONAL TRANSACTION FEE)". The country code, when there is
// one, is run together with the amount.
var conversionNote = regexp.MustCompile(`([A-Z]{2})?\d[\d,]*(?:\.\d+)?([A-Z]{3}) @ [\d.]+ conversion rate(?: \(INC\. [^)]*\))?`)

// transactionPlace names the country a purchase was made in, or nothing for a
// purchase at home. A country code in the description is preferred because it
// is specific; a foreign currency is the fallback, and catches purchases whose
// description carries no country at all. The currency is read from the feed's
// conversion when there is one, and from the description when not, so
// transactions synced before the conversion was stored still place.
func transactionPlace(tx models.Transaction) string {
	if match := conversionNote.FindStringSubmatch(tx.Description); match != nil {
		if place, ok := countryCodes[match[1]]; ok {
			return place
		}
		if place, ok := currencyPlaces[match[2]]; ok {
			return place
		}
		if match[2] != "NZD" {
			return match[2]
		}
	}

	fields := strings.Fields(tx.Description)
	if len(fields) >= 2 {
		last := fields[len(fields)-1]
		if len(last) == 2 && last == strings.ToUpper(last) {
			if place, ok := countryCodes[last]; ok {
				return place
			}
		}
	}
	if currency := strings.ToUpper(tx.Meta.Conversion.Currency); currency != "" && currency != "NZD" {
		if place, ok := currencyPlaces[currency]; ok {
			return place
		}
		return currency
	}
	return ""
}

// withAccountCurrencies marks each purchase on a foreign currency account,
// such as a Wise AUD balance, as made in that currency. Akahu only records a
// conversion when a purchase was in a currency other than the account's, so a
// purchase in Melbourne paid from an AUD balance carries no sign of being
// overseas at all. One paid from that balance back home does carry a
// conversion, from NZD, and is left alone, so it stays at home. The copy is
// for reading only and never stored.
func withAccountCurrencies(transactions []models.Transaction, accounts []models.Account) []models.Transaction {
	foreign := map[string]string{}
	for _, account := range accounts {
		if currency := strings.ToUpper(account.Balance.Currency); currency != "" && currency != "NZD" {
			foreign[account.Id] = currency
		}
	}
	if len(foreign) == 0 {
		return transactions
	}
	marked := slices.Clone(transactions)
	for i, tx := range marked {
		if currency, ok := foreign[tx.Account]; ok && tx.Meta.Conversion.Currency == "" {
			marked[i].Meta.Conversion.Currency = currency
		}
	}
	return marked
}

// travelBooking reports whether a purchase is the kind of thing booked for a
// trip: flights, accommodation, a rental car or a tour.
func travelBooking(tx models.Transaction) bool {
	haystack := strings.ToLower(tx.Category.Name + " " + tx.Category.Groups.PersonalFinance.Name)
	// Akahu files flights under "Air transport services".
	for _, needle := range []string{"airline", "air travel", "air transport", "travel agenc", "tour operator", "hotel", "motel", "accommodation", "lodging", "car rental", "vehicle hire"} {
		if strings.Contains(haystack, needle) {
			return true
		}
	}
	return false
}

// overseasFee reports whether a charge is the bank's cut of spending overseas,
// which belongs with the trip rather than standing on its own. A purchase that
// mentions the fee folded into it is still a purchase.
func overseasFee(tx models.Transaction) bool {
	if transactionPlace(tx) != "" {
		return false
	}
	description := strings.ToLower(tx.Description)
	for _, needle := range []string{"offshore service", "international transaction", "intl transaction", "foreign transaction", "foreign currency", "currency conversion", "overseas transaction"} {
		if strings.Contains(description, needle) {
			return true
		}
	}
	return false
}

// suggestTrips groups the window's overseas spending into trips, pulling in
// the flights and accommodation booked for each and the fees charged on it.
// Only spend in the pool is considered, so anything already in a memory or
// hidden stays out of a suggestion. Routine merchants are skipped: a streaming
// service billed in US dollars charges during a holiday as well, but it is not
// part of it.
func suggestTrips(pool []models.Transaction, routine map[string]float64, location *time.Location) []TimelineEntry {
	sorted := slices.Clone(pool)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.Before(sorted[j].Date) })

	type trip struct {
		places []string
		count  map[string]int
		txs    []models.Transaction

		// away is how many of txs were made while away. Bookings and fees are
		// appended after them.
		away int
	}

	// Overseas purchases a few days apart are one trip, whichever countries
	// they were in: a week in Singapore then Bali is one holiday.
	var trips []*trip
	for _, tx := range sorted {
		place := transactionPlace(tx)
		if _, regular := routine[merchantKey(tx)]; place == "" || regular {
			continue
		}
		var current *trip
		if len(trips) > 0 {
			last := trips[len(trips)-1]
			if tx.Date.Sub(last.txs[len(last.txs)-1].Date) <= tripGapDays*24*time.Hour {
				current = last
			}
		}
		if current == nil {
			current = &trip{count: map[string]int{}}
			trips = append(trips, current)
		}
		if current.count[place] == 0 {
			current.places = append(current.places, place)
		}
		current.count[place]++
		current.txs = append(current.txs, tx)
	}

	trips = slices.DeleteFunc(trips, func(t *trip) bool { return len(t.txs) < tripMinPurchases })

	used := map[string]bool{}
	for _, t := range trips {
		t.away = len(t.txs)
		for _, tx := range t.txs {
			used[tx.Id] = true
		}
	}

	// Bookings and fees go to the trip they lead up to. Trips are taken in
	// order, so a flight booked between two trips joins the earlier one only
	// if it falls inside it, and otherwise waits for the next.
	for _, t := range trips {
		start := t.txs[0].Date
		end := t.txs[len(t.txs)-1].Date
		for _, tx := range sorted {
			if used[tx.Id] {
				continue
			}
			// Flights and tours are booked months ahead; somewhere to stay is
			// usually charged on the night, so a motel in March is not part
			// of a holiday in July.
			lead := tripLeadDays
			if accommodation(tx) {
				lead = 2
			}
			booked := travelBooking(tx) && !tx.Date.Before(start.AddDate(0, 0, -lead)) && !tx.Date.After(end.AddDate(0, 0, 2))
			charged := overseasFee(tx) && !tx.Date.Before(start) && !tx.Date.After(end.AddDate(0, 0, 5))
			if booked || charged {
				t.txs = append(t.txs, tx)
				used[tx.Id] = true
			}
		}
	}

	kind := models.MemoryKindByKey("trip")
	var suggestions []TimelineEntry
	for _, t := range trips {
		// The trip's own dates are the time spent away. A flight booked in
		// March should not stretch a July holiday back across four months.
		start := t.txs[0].Date
		end := t.txs[t.away-1].Date

		// A trip is named after where most of the spending happened. A few
		// charges in US dollars during a week in Singapore are online
		// purchases, not a second destination.
		places := slices.Clone(t.places)
		sort.SliceStable(places, func(i, j int) bool { return t.count[places[i]] > t.count[places[j]] })
		named := places[:1]
		if len(places) > 1 && float64(t.count[places[1]]) >= float64(t.away)*tripPlaceShare {
			named = places[:2]
		}
		name := strings.Join(named, " & ")

		entry := TimelineEntry{
			Type:   TimelineSuggestion,
			Key:    "trip-" + start.In(location).Format(time.DateOnly),
			Title:  "Trip to " + name,
			Kind:   kind,
			Icon:   kind.Icon,
			Accent: kind.Accent,
			Start:  start.In(location),
			End:    end.In(location),
		}
		entry.addRows(t.txs)

		entry.Reason = fmt.Sprintf("%d purchases in %s", t.away, name)
		if len(named) < len(places) {
			entry.Reason = fmt.Sprintf("%d purchases overseas, mostly in %s", t.away, name)
		}
		if extra := len(t.txs) - t.away; extra > 0 {
			entry.Reason += fmt.Sprintf(", plus %d booking%s and fees around it", extra, plural(extra))
		}
		suggestions = append(suggestions, entry)
	}
	return suggestions
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

// cardTimestamp matches the time a card terminal stamps on the end of a
// description, as in "POS W/D BIKE BARN-11:14".
var cardTimestamp = regexp.MustCompile(`-\d{2}:\d{2}$`)

// timelineTitle names a purchase for the timeline. Without a merchant the row
// falls back to the raw bank description, which carries card terminal noise
// that says nothing about what was bought.
func timelineTitle(row TransactionRow) string {
	if row.Merchant.Name != "" {
		return row.Title
	}
	title := strings.TrimSpace(row.Title)

	// A conversion note splits the description in two, often around the same
	// merchant name written twice. The part before it is kept, unless there is
	// nothing before it.
	if loc := conversionNote.FindStringIndex(title); loc != nil {
		before, after := strings.TrimSpace(title[:loc[0]]), strings.TrimSpace(title[loc[1]:])
		title = before
		if title == "" {
			title = after
		}
	}

	for _, prefix := range []string{"POS W/D ", "EFTPOS ", "VISA PURCHASE ", "DEBIT CARD PURCHASE "} {
		if len(title) > len(prefix) && strings.EqualFold(title[:len(prefix)], prefix) {
			title = strings.TrimSpace(title[len(prefix):])
		}
	}
	title = strings.TrimSpace(cardTimestamp.ReplaceAllString(title, ""))
	if title == "" {
		return row.Title
	}
	return title
}

// addRows sets an entry's rows, newest first, and totals them.
func (e *TimelineEntry) addRows(txs []models.Transaction) {
	sorted := slices.Clone(txs)
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].Date.After(sorted[j].Date) })
	for _, tx := range sorted {
		e.Rows = append(e.Rows, BuildTransactionRow(tx))
		e.Total -= tx.Amount
	}
}

// BuildTimeline lays out the notable spending between start and end.
//
// Memories are shown when they ended inside the window, whatever transactions
// they hold. Suggestions and notable purchases are drawn from what is left:
// anything already in a memory or hidden is never offered again.
func BuildTimeline(transactions []models.Transaction, memories []models.Memory, prefs models.TimelinePreferences, start, end time.Time) Timeline {
	location := end.Location()

	byID := make(map[string]models.Transaction, len(transactions))
	for _, tx := range transactions {
		byID[tx.Id] = tx
	}

	hidden := map[string]bool{}
	for _, id := range prefs.HiddenTransactions {
		hidden[id] = true
	}
	dismissed := map[string]bool{}
	for _, key := range prefs.DismissedSuggestions {
		dismissed[key] = true
	}
	hiddenMerchants := map[string]bool{}
	for _, merchant := range prefs.HiddenMerchants {
		hiddenMerchants[strings.ToLower(merchant)] = true
	}

	var timeline Timeline
	timeline.HiddenMerchants = prefs.HiddenMerchants

	claimed := map[string]bool{}
	for _, memory := range memories {
		var txs []models.Transaction
		for _, id := range memory.TransactionIDs {
			claimed[id] = true
			if tx, ok := byID[id]; ok {
				txs = append(txs, tx)
			}
		}
		// A memory whose transactions have all gone, after the synced data was
		// cleared, has nothing to show and no date to sit at.
		if len(txs) == 0 {
			continue
		}

		kind := models.MemoryKindByKey(memory.Kind)
		entry := TimelineEntry{
			Type:   TimelineMemory,
			Key:    memory.Id,
			Title:  memory.Title,
			Note:   memory.Note,
			Kind:   kind,
			Icon:   kind.Icon,
			Accent: kind.Accent,
		}
		entry.addRows(txs)
		entry.Start = entry.Rows[len(entry.Rows)-1].Date.In(location)
		entry.End = entry.Rows[0].Date.In(location)

		if !entry.End.Before(start) && entry.End.Before(end) {
			timeline.Memories = append(timeline.Memories, entry)
		}
		timeline.MemoryOptions = append(timeline.MemoryOptions, TimelineMemoryOption{
			Id:    memory.Id,
			Label: memory.Title + " · " + entry.End.Format("Jan 2006"),
			end:   entry.End,
		})
	}
	sort.SliceStable(timeline.MemoryOptions, func(i, j int) bool {
		return timeline.MemoryOptions[i].end.After(timeline.MemoryOptions[j].end)
	})

	// Every purchase by local day, for the context under each notable one.
	byDay := map[string][]models.Transaction{}
	for _, tx := range transactions {
		if timelineSpend(tx) {
			day := tx.Date.In(location).Format(time.DateOnly)
			byDay[day] = append(byDay[day], tx)
		}
	}

	for i := range timeline.Memories {
		timeline.Memories[i].Nearby = nearbyPurchases(timeline.Memories[i], byDay, claimed, location)
	}

	// A large cash withdrawal is offered like a large purchase, since the cash
	// was usually taken out to buy something worth remembering. It is left out
	// of spend, which sets the threshold and finds routine merchants: cash is
	// not spent until it is used, and the same ATM visited every month is not
	// a routine merchant whose big withdrawals should be skipped.
	var spend, pool []models.Transaction
	for _, tx := range transactions {
		if !timelineSpend(tx) && !cashWithdrawal(tx) || tx.Date.Before(start) || !tx.Date.Before(end) {
			continue
		}
		if !cashWithdrawal(tx) {
			spend = append(spend, tx)
		}
		if hidden[tx.Id] {
			timeline.Hidden = append(timeline.Hidden, BuildTransactionRow(tx))
			continue
		}
		if hiddenMerchants[merchantKey(tx)] || billPayment(tx) {
			continue
		}
		if !claimed[tx.Id] {
			pool = append(pool, tx)
		}
	}
	sort.Slice(timeline.Hidden, func(i, j int) bool { return timeline.Hidden[i].Date.After(timeline.Hidden[j].Date) })

	timeline.Threshold = notableThreshold(spend)
	routine := routineMerchants(spend, location)

	// Overseas trips are found first, since a foreign currency is the surer
	// sign. Trips at home are looked for in whatever they leave behind.
	trips := suggestTrips(pool, routine, location)
	inTrip := map[string]bool{}
	for _, trip := range trips {
		for _, id := range trip.TransactionIDs() {
			inTrip[id] = true
		}
	}
	rest := slices.DeleteFunc(slices.Clone(pool), func(tx models.Transaction) bool { return inTrip[tx.Id] })
	trips = append(trips, suggestAwayTrips(rest, homePlaces(transactions, location), routine, location)...)

	suggested := map[string]bool{}
	for _, suggestion := range trips {
		if dismissed[suggestion.Key] {
			timeline.Dismissed++
			continue
		}
		timeline.Suggestions = append(timeline.Suggestions, suggestion)
		for _, id := range suggestion.TransactionIDs() {
			suggested[id] = true
		}
	}

	for _, tx := range pool {
		amount := -tx.Amount
		if suggested[tx.Id] || tx.Type == "FEE" || amount < timeline.Threshold {
			continue
		}
		if usual, ok := routine[merchantKey(tx)]; ok && (amount < usual*routineMultiple || amount < timeline.Threshold*outlierMultiple) {
			continue
		}

		row := BuildTransactionRow(tx)
		kind := models.MemoryKindByKey("purchase")
		if travelBooking(tx) {
			kind = models.MemoryKindByKey("trip")
		}
		entry := TimelineEntry{
			Type:     TimelinePurchase,
			Key:      tx.Id,
			Title:    timelineTitle(row),
			Merchant: tx.Merchant.Name,
			Kind:     kind,
			Icon:     row.Icon,
			Accent:   row.Accent,
			Start:    tx.Date.In(location),
			End:      tx.Date.In(location),
			Total:    amount,
			Rows:     []TransactionRow{row},
			Multiple: amount / timeline.Threshold,
		}
		entry.Outlier = entry.Multiple >= outlierMultiple

		var others []models.Transaction
		for _, other := range byDay[tx.Date.In(location).Format(time.DateOnly)] {
			if other.Id != tx.Id {
				others = append(others, other)
			}
		}
		sort.SliceStable(others, func(i, j int) bool { return others[i].Amount < others[j].Amount })
		if len(others) > sameDayLimit {
			entry.SameDayMore = len(others) - sameDayLimit
			others = others[:sameDayLimit]
		}
		for _, other := range others {
			row := BuildTransactionRow(other)
			row.Title = timelineTitle(row)
			entry.SameDay = append(entry.SameDay, TimelineContextRow{TransactionRow: row, Claimed: claimed[other.Id]})
		}

		if cashWithdrawal(tx) {
			entry.Title, entry.Icon = "Cash withdrawal", "bi-cash-stack"
		}
		if entry.Accent == "" {
			entry.Accent = "muted"
		}
		timeline.Purchases = append(timeline.Purchases, entry)
	}

	return timeline
}

// nearbyPurchases finds what was bought on the days a memory's purchases fall
// on, and the day either side of each, that the memory does not already hold.
// Only those days are searched rather than the whole span: a trip whose
// flights were booked in March would otherwise drag in four months of
// groceries. The biggest are kept when there are too many, listed by date.
func nearbyPurchases(memory TimelineEntry, byDay map[string][]models.Transaction, claimed map[string]bool, location *time.Location) []TimelineContextRow {
	held := map[string]bool{}
	days := map[string]bool{}
	for _, row := range memory.Rows {
		held[row.Id] = true
		local := row.Date.In(location)
		for offset := -1; offset <= 1; offset++ {
			days[local.AddDate(0, 0, offset).Format(time.DateOnly)] = true
		}
	}

	var found []models.Transaction
	for day := range days {
		for _, tx := range byDay[day] {
			if !held[tx.Id] {
				found = append(found, tx)
			}
		}
	}

	sort.Slice(found, func(i, j int) bool { return found[i].Amount < found[j].Amount })
	if len(found) > nearbyLimit {
		found = found[:nearbyLimit]
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].Date.Before(found[j].Date) })

	rows := make([]TimelineContextRow, len(found))
	for i, tx := range found {
		row := BuildTransactionRow(tx)
		row.Title = timelineTitle(row)
		rows[i] = TimelineContextRow{TransactionRow: row, Claimed: claimed[tx.Id]}
	}
	return rows
}

// TransactionsLink opens the transactions list over the days a memory spans,
// to look through everything around it.
func (e TimelineEntry) TransactionsLink() string {
	return "/transactions?from=" + e.Start.AddDate(0, 0, -1).Format(time.DateOnly) +
		"&to=" + e.End.AddDate(0, 0, 1).Format(time.DateOnly)
}

// TimelinePeriod is a window the timeline can be viewed over: the last twelve
// months, or a calendar year.
type TimelinePeriod struct {
	Key   string
	Label string
	Start time.Time
	End   time.Time
}

// timelinePeriods offers the rolling year plus each calendar year with spend in
// it, newest first.
func timelinePeriods(transactions []models.Transaction, now time.Time) []TimelinePeriod {
	location := now.Location()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, location)

	periods := []TimelinePeriod{{
		Key:   "",
		Label: "Last 12 months",
		Start: today.AddDate(-1, 0, 1),
		End:   today.AddDate(0, 0, 1),
	}}

	years := map[int]bool{}
	for _, tx := range transactions {
		if timelineSpend(tx) {
			years[tx.Date.In(location).Year()] = true
		}
	}
	var sorted []int
	for year := range years {
		sorted = append(sorted, year)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(sorted)))

	for _, year := range sorted {
		periods = append(periods, TimelinePeriod{
			Key:   strconv.Itoa(year),
			Label: strconv.Itoa(year),
			Start: time.Date(year, time.January, 1, 0, 0, 0, 0, location),
			End:   time.Date(year+1, time.January, 1, 0, 0, 0, 0, location),
		})
	}
	return periods
}

// TimelineData drives the timeline page.
type TimelineData struct {
	Timeline
	Months []TimelineMonth

	View    string
	Period  TimelinePeriod
	Periods []TimelinePeriod
	Kinds   []models.MemoryKind

	// Open is the memory whose purchases stay unfolded after a re-render, so
	// taking several out of one does not mean reopening it each time. Editing
	// does the same for the memory being edited, so adding a few nearby
	// purchases one after another keeps the list in view.
	Open    string
	Editing string
}

// State carries the current view and period through every action on the page,
// so saving or hiding something re-renders the page as it was.
func (d TimelineData) State() string {
	state, _ := json.Marshal(map[string]string{"view": d.View, "period": d.Period.Key})
	return string(state)
}

func (app *Application) Timeline(c echo.Context) error {
	return app.renderTimeline(c)
}

// renderTimeline reads the view and period from the request, which may be a
// page load's query string or an action's form, and renders the page.
func (app *Application) renderTimeline(c echo.Context) error {
	now := time.Now()

	transactions, err := app.store.ReadTransactions()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	memories, err := app.store.ReadMemories()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	prefs, err := app.store.GetTimelinePreferences()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}

	accounts, err := app.store.ReadAccounts()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	transactions = withAccountCurrencies(transactions, accounts)

	periods := timelinePeriods(transactions, now)
	period := periods[0]
	for _, p := range periods {
		if p.Key == c.FormValue("period") {
			period = p
		}
	}

	view := c.FormValue("view")
	if view != TimelineViewMemories && view != TimelineViewHidden {
		view = TimelineViewAll
	}

	timeline := BuildTimeline(transactions, memories, prefs, period.Start, period.End)

	return c.Render(http.StatusOK, "timeline", TimelineData{
		Timeline: timeline,
		Months:   timeline.Months(view, now.Location()),
		View:     view,
		Period:   period,
		Periods:  periods,
		Kinds:    models.MemoryKinds,
		Open:     c.FormValue("open"),
		Editing:  c.FormValue("editing"),
	})
}

// TimelineSaveMemory adds the posted transactions to a memory: the one named,
// or a new one when none is.
func (app *Application) TimelineSaveMemory(c echo.Context) error {
	form, err := c.FormParams()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	ids := uniqueValues(form["tx"])
	if len(ids) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, "Pick at least one purchase for this memory.")
	}

	memory, err := app.addToMemory(c, ids)
	if err != nil {
		return err
	}

	app.Toast(c, "Saved", "Added to “"+memory.Title+"”.")
	return app.renderTimeline(c)
}

// TimelineUpdateMemory renames a memory, or changes its kind or note.
func (app *Application) TimelineUpdateMemory(c echo.Context) error {
	memory, err := app.store.GetMemory(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "That memory no longer exists.")
	}

	memory.Kind = models.MemoryKindByKey(c.FormValue("kind")).Key
	memory.Note = strings.TrimSpace(c.FormValue("note"))
	if title := strings.TrimSpace(c.FormValue("title")); title != "" {
		memory.Title = title
	}

	if err := app.store.SaveMemory(memory); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return app.renderTimeline(c)
}

// TimelineRemoveFromMemory takes one purchase back out of a memory, returning
// it to the timeline. Taking out the last one removes the memory, since an
// empty memory has no date to sit at.
func (app *Application) TimelineRemoveFromMemory(c echo.Context) error {
	memory, err := app.store.GetMemory(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "That memory no longer exists.")
	}

	tx := c.Param("tx")
	memory.TransactionIDs = slices.DeleteFunc(memory.TransactionIDs, func(id string) bool { return id == tx })

	if len(memory.TransactionIDs) == 0 {
		err = app.store.DeleteMemory(memory.Id)
	} else {
		err = app.store.SaveMemory(memory)
	}
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return app.renderTimeline(c)
}

// TimelineDeleteMemory deletes a memory. Its purchases go back on the timeline
// rather than being lost with it.
func (app *Application) TimelineDeleteMemory(c echo.Context) error {
	if err := app.store.DeleteMemory(c.Param("id")); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return app.renderTimeline(c)
}

// timelineHideRequest is what a hide or restore names: transactions, a
// suggestion, or a merchant.
type timelineHideRequest struct {
	Transactions []string
	Suggestion   string
	Merchant     string
}

// TimelineHide hides purchases or a merchant, or dismisses a suggestion, that
// are not worth remembering.
func (app *Application) TimelineHide(c echo.Context) error {
	return app.updateTimelinePreferences(c, func(prefs *models.TimelinePreferences, req timelineHideRequest) {
		prefs.HiddenTransactions = uniqueValues(append(prefs.HiddenTransactions, req.Transactions...))
		prefs.DismissedSuggestions = uniqueValues(append(prefs.DismissedSuggestions, req.Suggestion))
		prefs.HiddenMerchants = uniqueValues(append(prefs.HiddenMerchants, req.Merchant))
	})
}

// TimelineRestore undoes a hide or a dismissal. Dismissed suggestions are only
// brought back all at once, since once dismissed there is nothing left on the
// page to name one by.
func (app *Application) TimelineRestore(c echo.Context) error {
	return app.updateTimelinePreferences(c, func(prefs *models.TimelinePreferences, req timelineHideRequest) {
		prefs.HiddenTransactions = slices.DeleteFunc(prefs.HiddenTransactions, func(id string) bool {
			return slices.Contains(req.Transactions, id)
		})
		prefs.HiddenMerchants = slices.DeleteFunc(prefs.HiddenMerchants, func(merchant string) bool {
			return merchant == req.Merchant
		})
		if req.Suggestion == "all" {
			prefs.DismissedSuggestions = nil
		}
	})
}

func (app *Application) updateTimelinePreferences(c echo.Context, apply func(*models.TimelinePreferences, timelineHideRequest)) error {
	form, err := c.FormParams()
	if err != nil {
		return echo.NewHTTPError(http.StatusBadRequest, err.Error())
	}

	prefs, err := app.store.GetTimelinePreferences()
	if err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	apply(&prefs, timelineHideRequest{
		Transactions: form["tx"],
		Suggestion:   c.FormValue("suggestion"),
		Merchant:     c.FormValue("merchant"),
	})
	if err := app.store.SaveTimelinePreferences(prefs); err != nil {
		return echo.NewHTTPError(http.StatusInternalServerError, err.Error())
	}
	return app.renderTimeline(c)
}

// uniqueValues drops blanks and repeats while keeping the order given.
func uniqueValues(values []string) []string {
	var unique []string
	for _, value := range values {
		if value != "" && !slices.Contains(unique, value) {
			unique = append(unique, value)
		}
	}
	return unique
}
