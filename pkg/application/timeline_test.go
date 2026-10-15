package application

import (
	"fmt"
	"net/url"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	timelineStart = time.Date(2026, time.January, 1, 0, 0, 0, 0, time.UTC)
	timelineEnd   = time.Date(2027, time.January, 1, 0, 0, 0, 0, time.UTC)
)

// purchase builds a debit on a given day of 2026.
func purchase(id, merchant string, amount float64, month time.Month, day int) models.Transaction {
	var tx models.Transaction
	tx.Id = id
	tx.Merchant.Name = merchant
	tx.Description = merchant
	tx.Amount = -amount
	tx.Type = "EFTPOS"
	tx.Date = time.Date(2026, month, day, 12, 0, 0, 0, time.UTC)
	return tx
}

// ordinaryYear is a year of small, everyday spending, which sets the bar a
// purchase has to clear to be notable. Ninety coffees at $5 put the notable
// threshold at the $100 floor.
func ordinaryYear() []models.Transaction {
	var txs []models.Transaction
	for i := 0; i < 90; i++ {
		txs = append(txs, purchase(fmt.Sprintf("coffee-%d", i), "Cafe", 5, time.Month(i%12+1), i%28+1))
	}
	return txs
}

func entryKeys(entries []TimelineEntry) []string {
	keys := make([]string, len(entries))
	for i, entry := range entries {
		keys[i] = entry.Key
	}
	return keys
}

func TestBuildTimelineNotablePurchases(t *testing.T) {
	txs := ordinaryYear()

	// Rent is big but paid every month, so it is routine.
	for month := time.January; month <= time.June; month++ {
		txs = append(txs, purchase(fmt.Sprintf("rent-%d", month), "Landlord", 600, month, 1))
	}
	txs = append(txs,
		purchase("couch", "Furniture Co", 1400, time.March, 14),
		purchase("boots", "Shoe Shop", 180, time.April, 2),
		// Twice the usual rent is a different thing from rent.
		purchase("bond", "Landlord", 2400, time.July, 1),
	)

	transfer := purchase("savings", "Savings", 5000, time.May, 1)
	transfer.Type = "TRANSFER"
	txs = append(txs, transfer)

	timeline := BuildTimeline(txs, nil, models.TimelinePreferences{}, timelineStart, timelineEnd)

	assert.Equal(t, notableFloor, timeline.Threshold)
	assert.ElementsMatch(t, []string{"couch", "boots", "bond"}, entryKeys(timeline.Purchases))

	outliers := map[string]bool{}
	for _, entry := range timeline.Purchases {
		outliers[entry.Key] = entry.Outlier
	}
	assert.True(t, outliers["couch"], "14x the threshold is an outlier")
	assert.False(t, outliers["boots"], "1.8x the threshold is notable but ordinary")
}

func TestNotableThresholdScalesWithSpending(t *testing.T) {
	var txs []models.Transaction
	for i := 0; i < 100; i++ {
		txs = append(txs, purchase(fmt.Sprintf("tx-%d", i), "Shop", float64(i+1)*10, time.March, 1))
	}
	// The 90th percentile of $10..$1000 sits well above the floor.
	assert.InDelta(t, 900, notableThreshold(txs), 10)
}

func TestTransactionPlace(t *testing.T) {
	hawker := purchase("a", "", 12, time.July, 1)
	hawker.Description = "MAXWELL FOOD CENTRE SINGAPORE SG"
	assert.Equal(t, "Singapore", transactionPlace(hawker))

	euro := purchase("b", "Cafe", 12, time.July, 1)
	euro.Meta.Conversion.Currency = "EUR"
	assert.Equal(t, "Europe", transactionPlace(euro), "currency is the fallback")

	home := purchase("c", "", 12, time.July, 1)
	home.Description = "COUNTDOWN PONSONBY AUCKLAND NZ"
	assert.Equal(t, "", transactionPlace(home))

	// Some banks write the conversion into the description instead.
	proton := purchase("e", "", 64, time.July, 1)
	proton.Description = "PROTON AG GENEVA CH119.88EUR @ 0.4954 conversion rate (INC. $4.48 INTERNATIONAL TRANSACTION FEE) PROTON AG GENEVA"
	assert.Equal(t, "Switzerland", transactionPlace(proton))
	assert.False(t, overseasFee(proton), "a purchase with the fee folded in is still a purchase")

	amazon := purchase("f", "", 343, time.July, 1)
	amazon.Description = "399.19AUD @ 0.8606 conversion rate (INC. $8.58 INTERNATIONAL TRANSACTION FEE) AMAZON MARKETPLACE AU SYDNEY"
	assert.Equal(t, "Australia", transactionPlace(amazon))

	// Lower case words are not country codes.
	word := purchase("d", "", 12, time.July, 1)
	word.Description = "Payment to us"
	assert.Equal(t, "", transactionPlace(word))
}

func singaporeTrip() []models.Transaction {
	flights := purchase("flights", "Singapore Airlines", 2100, time.March, 3)
	flights.Category.Name = "Airlines"

	fee := purchase("fee", "", 4.20, time.July, 16)
	fee.Description = "OFFSHORE SERVICE MARGINS"
	fee.Type = "FEE"

	txs := []models.Transaction{flights, fee}
	for i, day := range []int{10, 11, 11, 13, 15} {
		tx := purchase(fmt.Sprintf("sg-%d", i), "", 40, time.July, day)
		tx.Description = "HAWKER STALL SINGAPORE SG"
		txs = append(txs, tx)
	}
	// Two days in Bali on the way home are the same holiday.
	for i, day := range []int{17, 18} {
		tx := purchase(fmt.Sprintf("bali-%d", i), "", 30, time.July, day)
		tx.Meta.Conversion.Currency = "IDR"
		txs = append(txs, tx)
	}
	return txs
}

func TestBuildTimelineSuggestsTrips(t *testing.T) {
	txs := append(ordinaryYear(), singaporeTrip()...)

	// A monthly USD subscription is overseas spending but not a trip.
	for month := time.January; month <= time.December; month += 3 {
		sub := purchase(fmt.Sprintf("sub-%d", month), "Streaming", 15, month, 20)
		sub.Meta.Conversion.Currency = "USD"
		txs = append(txs, sub)
	}

	timeline := BuildTimeline(txs, nil, models.TimelinePreferences{}, timelineStart, timelineEnd)

	require.Len(t, timeline.Suggestions, 1)
	trip := timeline.Suggestions[0]

	assert.Equal(t, "Trip to Singapore & Indonesia", trip.Title)
	assert.Equal(t, "trip", trip.Kind.Key)
	assert.ElementsMatch(t,
		[]string{"flights", "fee", "sg-0", "sg-1", "sg-2", "sg-3", "sg-4", "bali-0", "bali-1"},
		trip.TransactionIDs())
	assert.InDelta(t, 2100+4.20+5*40+2*30, trip.Total, 0.001)
	assert.Contains(t, trip.Reason, "7 purchases in Singapore & Indonesia")

	// The trip runs over the time spent away, not back to when the flights
	// were booked.
	assert.Equal(t, "10 – 18 Jul", trip.Span())

	// The flights belong to the trip, so they are not listed a second time.
	assert.NotContains(t, entryKeys(timeline.Purchases), "flights")
}

func TestBuildTimelineMemoriesAndHiding(t *testing.T) {
	txs := append(ordinaryYear(), singaporeTrip()...)
	txs = append(txs,
		purchase("couch", "Furniture Co", 1400, time.March, 14),
		purchase("ird", "Inland Revenue", 900, time.May, 7),
		purchase("ring", "Jeweller", 3000, time.September, 9),
	)

	memories := []models.Memory{{
		Id:             "engaged",
		Title:          "Engaged!",
		Kind:           "goal",
		TransactionIDs: []string{"ring"},
	}}
	prefs := models.TimelinePreferences{HiddenTransactions: []string{"ird"}}

	timeline := BuildTimeline(txs, memories, prefs, timelineStart, timelineEnd)

	t.Run("a memory leaves the pool", func(t *testing.T) {
		require.Len(t, timeline.Memories, 1)
		memory := timeline.Memories[0]
		assert.Equal(t, "Engaged!", memory.Title)
		assert.Equal(t, "Goal reached", memory.Kind.Label)
		assert.Equal(t, 3000.0, memory.Total)
		assert.NotContains(t, entryKeys(timeline.Purchases), "ring")
	})

	t.Run("hidden purchases are listed apart", func(t *testing.T) {
		assert.NotContains(t, entryKeys(timeline.Purchases), "ird")
		require.Len(t, timeline.Hidden, 1)
		assert.Equal(t, "ird", timeline.Hidden[0].Id)
	})

	t.Run("months run newest first", func(t *testing.T) {
		months := timeline.Months(TimelineViewAll, time.UTC)
		require.NotEmpty(t, months)
		assert.Equal(t, time.September, months[0].Month.Month())
		assert.Equal(t, time.March, months[len(months)-1].Month.Month())

		memoriesOnly := timeline.Months(TimelineViewMemories, time.UTC)
		require.Len(t, memoriesOnly, 1)
		assert.Equal(t, "engaged", memoriesOnly[0].Entries[0].Key)
	})

	t.Run("a dismissed suggestion falls back to its notable purchases", func(t *testing.T) {
		key := timeline.Suggestions[0].Key
		dismissed := BuildTimeline(txs, memories, models.TimelinePreferences{DismissedSuggestions: []string{key}}, timelineStart, timelineEnd)

		assert.Empty(t, dismissed.Suggestions)
		assert.Equal(t, 1, dismissed.Dismissed)
		assert.Contains(t, entryKeys(dismissed.Purchases), "flights")
		// The fee was only ever interesting as part of the trip.
		assert.NotContains(t, entryKeys(dismissed.Purchases), "fee")
	})

	t.Run("a memory outside the window is left off", func(t *testing.T) {
		later := BuildTimeline(txs, memories, prefs, timelineEnd, timelineEnd.AddDate(1, 0, 0))
		assert.Empty(t, later.Memories)
	})
}

func TestTimelineEntrySpan(t *testing.T) {
	day := func(month time.Month, d int) time.Time { return time.Date(2026, month, d, 0, 0, 0, 0, time.UTC) }

	assert.Equal(t, "Tue 14 Jul", TimelineEntry{Start: day(time.July, 14), End: day(time.July, 14)}.Span())
	assert.Equal(t, "28 Jun – 4 Jul", TimelineEntry{Start: day(time.June, 28), End: day(time.July, 4)}.Span())
	assert.Equal(t, "28 Dec 2025 – 4 Jan 2026", TimelineEntry{
		Start: time.Date(2025, time.December, 28, 0, 0, 0, 0, time.UTC),
		End:   day(time.January, 4),
	}.Span())
}

func TestTimelinePeriods(t *testing.T) {
	now := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	txs := []models.Transaction{
		purchase("a", "Shop", 10, time.March, 1),
		{Id: "b", Amount: -10, Date: time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC)},
	}

	periods := timelinePeriods(txs, now)
	require.Len(t, periods, 3)
	assert.Equal(t, "Last 12 months", periods[0].Label)
	assert.Equal(t, time.Date(2025, time.October, 16, 0, 0, 0, 0, time.UTC), periods[0].Start)
	assert.Equal(t, time.Date(2026, time.October, 16, 0, 0, 0, 0, time.UTC), periods[0].End)
	assert.Equal(t, []string{"2026", "2024"}, []string{periods[1].Key, periods[2].Key})
}

func TestRenderTimeline(t *testing.T) {
	txs := append(ordinaryYear(), singaporeTrip()...)
	txs = append(txs,
		purchase("couch", "Furniture Co", 1400, time.March, 14),
		purchase("ring", "Jeweller", 3000, time.September, 9),
	)
	memories := []models.Memory{{Id: "engaged", Title: "Engaged!", Kind: "goal", Note: "She said yes", TransactionIDs: []string{"ring"}}}
	timeline := BuildTimeline(txs, memories, models.TimelinePreferences{}, timelineStart, timelineEnd)

	render := func(view string) string {
		return renderTemplate(t, "timeline", TimelineData{
			Timeline: timeline,
			Months:   timeline.Months(view, time.UTC),
			View:     view,
			Period:   TimelinePeriod{Key: "2026", Label: "2026"},
			Periods:  []TimelinePeriod{{Key: "", Label: "Last 12 months"}, {Key: "2026", Label: "2026"}},
			Kinds:    models.MemoryKinds,
		})
	}

	page := render(TimelineViewAll)

	t.Run("summarises the memories", func(t *testing.T) {
		assert.Contains(t, page, "Engaged!")
		assert.Contains(t, page, "She said yes")
		assert.Contains(t, page, "$3,000.00")
	})

	t.Run("offers the trip as a suggestion to save", func(t *testing.T) {
		assert.Contains(t, page, "Trip to Singapore &amp; Indonesia")
		assert.Contains(t, page, "Suggested")
		assert.Contains(t, page, `name="tx" value="flights"`)
		assert.Contains(t, page, "Not a trip")
	})

	t.Run("flags outliers and offers to tag, hide or group", func(t *testing.T) {
		assert.Contains(t, page, "Outlier")
		assert.Contains(t, page, `form="timeline-group"`)
		assert.Contains(t, page, `id="timeline-group"`)
		assert.Contains(t, page, `hx-post="/timeline/hide"`)
		assert.Contains(t, page, `<option value="goal"`)
	})

	t.Run("carries the period through actions", func(t *testing.T) {
		assert.Contains(t, page, `&#34;period&#34;:&#34;2026&#34;`)
	})

	t.Run("the memories view leaves out everything else", func(t *testing.T) {
		memoriesOnly := render(TimelineViewMemories)
		assert.Contains(t, memoriesOnly, "Engaged!")
		assert.NotContains(t, memoriesOnly, "Furniture Co")
		assert.NotContains(t, memoriesOnly, "Suggested")
	})
}

func TestTimelineHandlers(t *testing.T) {
	app := newTestApp(t)
	for _, tx := range append(ordinaryYear(), purchase("couch", "Furniture Co", 1400, time.March, 14), purchase("rug", "Rug Shop", 400, time.March, 15)) {
		require.NoError(t, app.store.CreateTransaction(tx))
	}

	t.Run("grouping purchases saves a memory", func(t *testing.T) {
		c, rec := postForm(t, app, url.Values{"tx": {"couch", "rug"}, "title": {"New lounge"}, "kind": {"home"}})
		require.NoError(t, app.TimelineSaveMemory(c))
		assert.Contains(t, rec.Body.String(), "New lounge")

		memories, err := app.store.ReadMemories()
		require.NoError(t, err)
		require.Len(t, memories, 1)
		assert.Equal(t, []string{"couch", "rug"}, memories[0].TransactionIDs)
		assert.Equal(t, "home", memories[0].Kind)
	})

	t.Run("a purchase moves to the newest memory that claims it", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"tx": {"rug"}, "title": {"Rug"}})
		require.NoError(t, app.TimelineSaveMemory(c))

		memories, err := app.store.ReadMemories()
		require.NoError(t, err)
		require.Len(t, memories, 2)
		for _, memory := range memories {
			if memory.Title == "New lounge" {
				assert.Equal(t, []string{"couch"}, memory.TransactionIDs)
			}
		}
	})

	t.Run("a memory needs something in it", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"title": {"Empty"}})
		assert.Error(t, app.TimelineSaveMemory(c))
	})

	t.Run("hiding and restoring round trips", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"tx": {"couch"}})
		require.NoError(t, app.TimelineHide(c))
		prefs, err := app.store.GetTimelinePreferences()
		require.NoError(t, err)
		assert.Equal(t, []string{"couch"}, prefs.HiddenTransactions)

		c, _ = postForm(t, app, url.Values{"tx": {"couch"}})
		require.NoError(t, app.TimelineRestore(c))
		prefs, err = app.store.GetTimelinePreferences()
		require.NoError(t, err)
		assert.Empty(t, prefs.HiddenTransactions)
	})
}

func TestBuildTimelineRoutineMerchants(t *testing.T) {
	txs := ordinaryYear()
	for month := time.January; month <= time.December; month++ {
		txs = append(txs, purchase(fmt.Sprintf("shop-%d", month), "PAK'nSAVE", 60, month, 3))
	}
	txs = append(txs,
		// Well above the usual shop, but an ordinary amount to spend: still
		// just groceries.
		purchase("big-shop", "PAK'nSAVE", 150, time.August, 20),
		// Big enough to be an outlier as well: catering for a wedding.
		purchase("wedding-shop", "PAK'nSAVE", 900, time.October, 20),
	)

	timeline := BuildTimeline(txs, nil, models.TimelinePreferences{}, timelineStart, timelineEnd)
	assert.Equal(t, []string{"wedding-shop"}, entryKeys(timeline.Purchases))
}

func TestBuildTimelineHiddenMerchants(t *testing.T) {
	txs := append(ordinaryYear(),
		purchase("fund-1", "Fisher Funds", 500, time.March, 1),
		purchase("fund-2", "Fisher Funds", 750, time.June, 1),
		purchase("couch", "Furniture Co", 1400, time.March, 14),
	)

	prefs := models.TimelinePreferences{HiddenMerchants: []string{"Fisher Funds"}}
	timeline := BuildTimeline(txs, nil, prefs, timelineStart, timelineEnd)

	assert.Equal(t, []string{"couch"}, entryKeys(timeline.Purchases))
	assert.Equal(t, []string{"Fisher Funds"}, timeline.HiddenMerchants)
	assert.Equal(t, "Furniture Co", timeline.Purchases[0].Merchant)
}

func TestTimelineTitle(t *testing.T) {
	row := func(merchant, description string) TransactionRow {
		var tx models.Transaction
		tx.Merchant.Name = merchant
		tx.Description = description
		return BuildTransactionRow(tx)
	}

	assert.Equal(t, "BURWOOD CYCLE", timelineTitle(row("", "POS W/D BURWOOD CYCLE-11:14")))
	assert.Equal(t, "Bike Barn", timelineTitle(row("Bike Barn", "POS W/D BIKE BARN-11:14")))
	assert.Equal(t, "Council rates", timelineTitle(row("", "Council rates")))
	assert.Equal(t, "PROTON AG GENEVA", timelineTitle(row("", "PROTON AG GENEVA CH119.88EUR @ 0.4954 conversion rate (INC. $4.48 INTERNATIONAL TRANSACTION FEE) PROTON AG GENEVA")))
	assert.Equal(t, "AMAZON MARKETPLACE AU SYDNEY", timelineTitle(row("", "399.19AUD @ 0.8606 conversion rate (INC. $8.58 INTERNATIONAL TRANSACTION FEE) AMAZON MARKETPLACE AU SYDNEY")))
}

func TestRenderTimelineEscapesMerchantsInActions(t *testing.T) {
	txs := append(ordinaryYear(), purchase("tickets", `The "Big" Show`, 400, time.May, 2))
	prefs := models.TimelinePreferences{HiddenMerchants: []string{`Joe's "Bar"`}}
	timeline := BuildTimeline(txs, nil, prefs, timelineStart, timelineEnd)

	for _, view := range []string{TimelineViewAll, TimelineViewHidden} {
		page := renderTemplate(t, "timeline", TimelineData{
			Timeline: timeline,
			Months:   timeline.Months(view, time.UTC),
			View:     view,
			Period:   TimelinePeriod{Label: "Last 12 months"},
			Kinds:    models.MemoryKinds,
		})

		// The quotes are escaped once as JSON and once for the attribute, so a
		// browser hands htmx valid JSON.
		if view == TimelineViewAll {
			assert.Contains(t, page, `{&#34;merchant&#34;:&#34;The \&#34;Big\&#34; Show&#34;}`)
		} else {
			assert.Contains(t, page, `{&#34;merchant&#34;:&#34;Joe&#39;s \&#34;Bar\&#34;&#34;}`)
			assert.Contains(t, page, "Hidden merchants")
		}
	}
}

func TestSuggestTripsNamesTheMainDestination(t *testing.T) {
	var txs []models.Transaction
	for i := 0; i < 8; i++ {
		tx := purchase(fmt.Sprintf("sg-%d", i), "", 40, time.September, 25+i%5)
		tx.Description = "HAWKER STALL SINGAPORE SG"
		txs = append(txs, tx)
	}
	online := purchase("app", "App Store", 12, time.September, 27)
	online.Meta.Conversion.Currency = "USD"
	txs = append(txs, online)

	suggestions := suggestTrips(txs, nil, time.UTC)
	require.Len(t, suggestions, 1)
	assert.Equal(t, "Trip to Singapore", suggestions[0].Title)
	assert.Equal(t, "9 purchases overseas, mostly in Singapore", suggestions[0].Reason)
	assert.Contains(t, suggestions[0].TransactionIDs(), "app")
}

func TestBuildTimelineLeavesOutBills(t *testing.T) {
	bill := func(id, merchant, category string, amount float64, month time.Month) models.Transaction {
		tx := purchase(id, merchant, amount, month, 15)
		tx.Category.Name = category
		return tx
	}

	// Rates paid three times a year at no fixed interval never look routine.
	txs := append(ordinaryYear(),
		bill("rates-1", "Christchurch City Council", "Local government", 793, time.February),
		bill("rates-2", "Christchurch City Council", "Local government", 799, time.June),
		bill("rates-3", "Christchurch City Council", "Local government", 862, time.August),
		bill("insurance", "AA Insurance", "Insurance", 493, time.April),
		bill("power", "Mercury", "Electricity services", 310, time.July),
		bill("tv", "JB Hi-Fi", "Electronic and appliance stores", 1800, time.May),
	)

	timeline := BuildTimeline(txs, nil, models.TimelinePreferences{}, timelineStart, timelineEnd)
	assert.Equal(t, []string{"tv"}, entryKeys(timeline.Purchases))
}

func TestBuildTimelineSameDayContext(t *testing.T) {
	txs := append(ordinaryYear(),
		purchase("timber", "Bunnings", 640, time.November, 8),
		purchase("screws", "Mitre 10", 45, time.November, 8),
		purchase("stain", "Resene", 80, time.November, 8),
		purchase("lunch", "Bakery", 12, time.November, 8),
	)
	memories := []models.Memory{{Id: "m", Title: "Lunch", TransactionIDs: []string{"lunch"}}}

	timeline := BuildTimeline(txs, memories, models.TimelinePreferences{}, timelineStart, timelineEnd)

	var deck TimelineEntry
	for _, entry := range timeline.Purchases {
		if entry.Key == "timber" {
			deck = entry
		}
	}
	require.Equal(t, "timber", deck.Key)

	var names []string
	for _, row := range deck.SameDay {
		names = append(names, row.Title)
		assert.Equal(t, row.Id == "lunch", row.Claimed, "only what is already in a memory is claimed")
	}
	// Biggest first, and never the purchase itself.
	assert.Equal(t, []string{"Resene", "Mitre 10", "Bakery"}, names)
}

func TestBuildTimelineSameDayIsCapped(t *testing.T) {
	txs := []models.Transaction{purchase("tv", "JB Hi-Fi", 2000, time.May, 1)}
	for i := 0; i < sameDayLimit+3; i++ {
		txs = append(txs, purchase(fmt.Sprintf("o-%d", i), "Shop", float64(10+i), time.May, 1))
	}
	timeline := BuildTimeline(txs, nil, models.TimelinePreferences{}, timelineStart, timelineEnd)

	require.Len(t, timeline.Purchases, 1)
	assert.Len(t, timeline.Purchases[0].SameDay, sameDayLimit)
	assert.Equal(t, 3, timeline.Purchases[0].SameDayMore)
}

func TestBuildTimelineOffersEveryMemory(t *testing.T) {
	txs := []models.Transaction{
		purchase("old", "Shop", 500, time.March, 1),
		{Id: "older", Amount: -500, Date: time.Date(2024, time.June, 1, 0, 0, 0, 0, time.UTC)},
	}
	memories := []models.Memory{
		{Id: "a", Title: "Back then", TransactionIDs: []string{"older"}},
		{Id: "b", Title: "This year", TransactionIDs: []string{"old"}},
	}

	timeline := BuildTimeline(txs, memories, models.TimelinePreferences{}, timelineStart, timelineEnd)

	// Only this year's memory is shown, but either can be added to.
	assert.Len(t, timeline.Memories, 1)
	require.Len(t, timeline.MemoryOptions, 2)
	assert.Equal(t, "This year · Mar 2026", timeline.MemoryOptions[0].Label)
	assert.Equal(t, "Back then · Jun 2024", timeline.MemoryOptions[1].Label)
}

func TestRenderTimelineMemoryActions(t *testing.T) {
	txs := append(ordinaryYear(),
		purchase("couch", "Furniture Co", 1400, time.March, 14),
		purchase("rug", "Rug Shop", 120, time.March, 14),
		purchase("ring", "Jeweller", 3000, time.September, 9),
	)
	memories := []models.Memory{{Id: "engaged", Title: "Engaged!", Kind: "goal", TransactionIDs: []string{"ring"}}}
	timeline := BuildTimeline(txs, memories, models.TimelinePreferences{}, timelineStart, timelineEnd)

	page := renderTemplate(t, "timeline", TimelineData{
		Timeline: timeline,
		Months:   timeline.Months(TimelineViewAll, time.UTC),
		View:     TimelineViewAll,
		Kinds:    models.MemoryKinds,
		Open:     "engaged",
	})

	t.Run("one verb for adding, alone or together", func(t *testing.T) {
		assert.Contains(t, page, "Add to memory")
		assert.NotContains(t, page, ">Tag<")
		assert.NotContains(t, page, "Group</button>")
		assert.Contains(t, page, "New memory&hellip;")
		assert.Contains(t, page, `<option value="engaged">Engaged! · Sep 2026</option>`)
	})

	t.Run("a memory can be edited and trimmed", func(t *testing.T) {
		assert.Contains(t, page, `hx-put="/timeline/memories/engaged"`)
		assert.Contains(t, page, `hx-delete="/timeline/memories/engaged/transactions/ring"`)
		assert.Contains(t, page, `<details class="b-tl-rows" open>`, "the memory being trimmed stays open")
	})

	t.Run("shows the rest of the day and links the store", func(t *testing.T) {
		assert.Contains(t, page, "Same day")
		assert.Contains(t, page, `value="rug"`)
		assert.Contains(t, page, `href="/transactions?search=Furniture%20Co"`)
	})
}

func TestTimelineMemoryEditing(t *testing.T) {
	app := newTestApp(t)
	for _, tx := range append(ordinaryYear(),
		purchase("flights", "Air NZ", 1400, time.March, 1),
		purchase("hotel", "Hotel", 900, time.March, 10),
		purchase("tour", "Tours", 300, time.March, 11),
	) {
		require.NoError(t, app.store.CreateTransaction(tx))
	}

	c, _ := postForm(t, app, url.Values{"tx": {"flights"}, "title": {"Japan"}, "kind": {"trip"}})
	require.NoError(t, app.TimelineSaveMemory(c))
	memories, err := app.store.ReadMemories()
	require.NoError(t, err)
	require.Len(t, memories, 1)
	id := memories[0].Id

	t.Run("purchases can be added to an existing memory", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"tx": {"hotel", "tour"}, "memory": {id}, "title": {"ignored"}})
		require.NoError(t, app.TimelineSaveMemory(c))

		memory, err := app.store.GetMemory(id)
		require.NoError(t, err)
		assert.Equal(t, []string{"flights", "hotel", "tour"}, memory.TransactionIDs)
		assert.Equal(t, "Japan", memory.Title, "adding does not rename")
	})

	t.Run("a memory can be edited", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"title": {"Japan 2026"}, "kind": {"experience"}, "note": {"Cherry blossoms"}})
		c.SetParamNames("id")
		c.SetParamValues(id)
		require.NoError(t, app.TimelineUpdateMemory(c))

		memory, err := app.store.GetMemory(id)
		require.NoError(t, err)
		assert.Equal(t, "Japan 2026", memory.Title)
		assert.Equal(t, "experience", memory.Kind)
		assert.Equal(t, "Cherry blossoms", memory.Note)
		assert.Len(t, memory.TransactionIDs, 3, "editing keeps the purchases")
	})

	t.Run("purchases can be taken out, and the last takes the memory", func(t *testing.T) {
		remove := func(tx string) {
			c, _ := postForm(t, app, url.Values{})
			c.SetParamNames("id", "tx")
			c.SetParamValues(id, tx)
			require.NoError(t, app.TimelineRemoveFromMemory(c))
		}

		remove("hotel")
		memory, err := app.store.GetMemory(id)
		require.NoError(t, err)
		assert.Equal(t, []string{"flights", "tour"}, memory.TransactionIDs)

		remove("flights")
		remove("tour")
		memories, err := app.store.ReadMemories()
		require.NoError(t, err)
		assert.Empty(t, memories)
	})

	t.Run("adding to a memory that has gone is an error", func(t *testing.T) {
		c, _ := postForm(t, app, url.Values{"tx": {"hotel"}, "memory": {"missing"}})
		assert.Error(t, app.TimelineSaveMemory(c))
	})
}

func TestBuildTimelineNearbyPurchases(t *testing.T) {
	flights := purchase("flights", "Air NZ", 1400, time.March, 1)
	txs := append(ordinaryYear(),
		flights,
		purchase("hotel", "Hotel", 900, time.July, 10),
		purchase("taxi", "Taxi", 60, time.July, 11),            // the day after: offered
		purchase("museum", "Museum", 25, time.July, 9),         // the day before: offered
		purchase("far", "Shop", 500, time.July, 20),            // well after: not offered
		purchase("groceries", "PAK'nSAVE", 120, time.April, 2), // between bookings and trip: not offered
		purchase("claimed", "Souvenirs", 40, time.July, 10),
	)
	memories := []models.Memory{
		{Id: "japan", Title: "Japan", Kind: "trip", TransactionIDs: []string{"flights", "hotel"}},
		{Id: "gifts", Title: "Gifts", TransactionIDs: []string{"claimed"}},
	}

	timeline := BuildTimeline(txs, memories, models.TimelinePreferences{}, timelineStart, timelineEnd)

	var japan TimelineEntry
	for _, memory := range timeline.Memories {
		if memory.Key == "japan" {
			japan = memory
		}
	}
	require.Equal(t, "japan", japan.Key)

	var ids []string
	claimed := map[string]bool{}
	for _, row := range japan.Nearby {
		ids = append(ids, row.Id)
		claimed[row.Id] = row.Claimed
	}

	assert.Contains(t, ids, "taxi")
	assert.Contains(t, ids, "museum")
	assert.Contains(t, ids, "claimed")
	assert.True(t, claimed["claimed"], "already in another memory")
	assert.NotContains(t, ids, "hotel", "never what the memory already holds")
	assert.NotContains(t, ids, "far")
	assert.NotContains(t, ids, "groceries", "only the memory's own days, not the months between")

	for i := 1; i < len(japan.Nearby); i++ {
		assert.False(t, japan.Nearby[i].Date.Before(japan.Nearby[i-1].Date), "listed by date")
	}

	assert.Equal(t, "/transactions?from=2026-02-28&to=2026-07-11", japan.TransactionsLink())
}

func TestRenderTimelineMemoryEditShowsNearby(t *testing.T) {
	txs := append(ordinaryYear(),
		purchase("ring", "Jeweller", 3000, time.September, 9),
		purchase("dinner", "Restaurant", 180, time.September, 9),
	)
	memories := []models.Memory{{Id: "engaged", Title: "Engaged!", Kind: "goal", TransactionIDs: []string{"ring"}}}
	timeline := BuildTimeline(txs, memories, models.TimelinePreferences{}, timelineStart, timelineEnd)

	page := renderTemplate(t, "timeline", TimelineData{
		Timeline: timeline,
		Months:   timeline.Months(TimelineViewMemories, time.UTC),
		View:     TimelineViewMemories,
		Kinds:    models.MemoryKinds,
		Editing:  "engaged",
	})

	assert.Contains(t, page, "Bought around then")
	assert.Contains(t, page, `class="form-check-input b-tl-pick" name="tx" value="dinner"`)
	assert.Contains(t, page, `name="memory" value="engaged"`)
	assert.Contains(t, page, `<details class="b-tl-tag" open>`, "the memory being edited stays open")
	assert.Contains(t, page, `href="/transactions?from=2026-09-08&amp;to=2026-09-10"`)
	// Nearby ticks must not summon the floating group bar.
	assert.NotContains(t, page, `class="form-check-input b-tl-select" name="tx" value="dinner"`)
}
