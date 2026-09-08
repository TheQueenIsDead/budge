package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// now is a fixed Tuesday, so the tests can reason about which Monday a
// transaction falls behind without depending on the day they are run.
var now = time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC)

// spendTransaction builds the minimum a transaction needs to be classified.
func spendTransaction(category, merchant string, amount float64, date time.Time) models.Transaction {
	var tx models.Transaction
	tx.Category.Name = category
	tx.Merchant.Name = merchant
	tx.Amount = amount
	tx.Date = date
	return tx
}

func day(offset int) time.Time {
	return now.AddDate(0, 0, offset)
}

func TestClassifySpend(t *testing.T) {
	tests := []struct {
		name     string
		tx       models.Transaction
		expected string
		matched  bool
	}{
		{
			name:     "supermarket is groceries",
			tx:       spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -120, now),
			expected: "groceries",
			matched:  true,
		},
		{
			name:     "dairy runs count as groceries",
			tx:       spendTransaction("Convenience stores", "Parnwell Superette", -12, now),
			expected: "groceries",
			matched:  true,
		},
		{
			name:     "fuel station is petrol",
			tx:       spendTransaction("Fuel stations", "Z Energy", -90, now),
			expected: "petrol",
			matched:  true,
		},
		{
			name:     "electricity is power",
			tx:       spendTransaction("Electricity services", "Mercury", -180, now),
			expected: "power",
			matched:  true,
		},
		{
			name:     "broadband is connectivity",
			tx:       spendTransaction("Internet services", "Voyager Internet", -89, now),
			expected: "connectivity",
			matched:  true,
		},
		{
			name:     "category casing does not matter",
			tx:       spendTransaction("FUEL STATIONS", "BP", -70, now),
			expected: "petrol",
			matched:  true,
		},
		{
			name:    "untracked category matches nothing",
			tx:      spendTransaction("Building supplies", "Bunnings Warehouse", -240, now),
			matched: false,
		},
		{
			name:    "money coming in is not spend",
			tx:      spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", 40, now),
			matched: false,
		},
		{
			name: "transfers are excluded even when categorised",
			tx: func() models.Transaction {
				tx := spendTransaction("Fuel stations", "Z Energy", -90, now)
				tx.Type = "TRANSFER"
				return tx
			}(),
			matched: false,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			key, ok := ClassifySpend(test.tx)
			assert.Equal(t, test.matched, ok)
			if test.matched {
				assert.Equal(t, test.expected, key)
			}
		})
	}
}

func TestPeriodStart(t *testing.T) {
	tests := []struct {
		name     string
		cadence  Cadence
		input    time.Time
		expected time.Time
	}{
		{
			name:     "weekly winds back to monday",
			cadence:  CadenceWeekly,
			input:    now, // Tuesday 8 Sep 2026
			expected: time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "weekly treats sunday as the end of its week",
			cadence:  CadenceWeekly,
			input:    time.Date(2026, time.September, 13, 23, 0, 0, 0, time.UTC),
			expected: time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "weekly leaves monday alone",
			cadence:  CadenceWeekly,
			input:    time.Date(2026, time.September, 7, 9, 0, 0, 0, time.UTC),
			expected: time.Date(2026, time.September, 7, 0, 0, 0, 0, time.UTC),
		},
		{
			name:     "monthly winds back to the first",
			cadence:  CadenceMonthly,
			input:    now,
			expected: time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, periodStart(test.input, test.cadence))
		})
	}
}

func TestBuildSpendBuckets(t *testing.T) {
	groceries, ok := SpendGroupByKey("groceries")
	require.True(t, ok)

	transactions := []models.Transaction{
		// This week (Mon 7 Sep onwards).
		spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -100, day(0)),
		spendTransaction("Convenience stores", "Lake Terrace Dairy", -20, day(0)),
		// Last week.
		spendTransaction("Supermarkets and grocery stores", "New World", -180, day(-7)),
		// Three weeks back, leaving the week between empty.
		spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -150, day(-21)),
		// Petrol must not leak into the groceries series.
		spendTransaction("Fuel stations", "Z Energy", -90, day(0)),
		// Older than the window.
		spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -999, day(-200)),
	}

	buckets := BuildSpendBuckets(transactions, groceries, CadenceWeekly, 4, now)
	require.Len(t, buckets, 4)

	t.Run("totals the current period as a positive amount", func(t *testing.T) {
		assert.Equal(t, 120.0, buckets[3].Total)
		assert.Equal(t, 2, buckets[3].Count)
	})

	t.Run("marks only the in progress period as partial", func(t *testing.T) {
		assert.True(t, buckets[3].Partial)
		assert.False(t, buckets[0].Partial)
		assert.False(t, buckets[1].Partial)
		assert.False(t, buckets[2].Partial)
	})

	t.Run("keeps empty periods in the series", func(t *testing.T) {
		assert.Equal(t, 150.0, buckets[0].Total)
		assert.Equal(t, 0.0, buckets[1].Total)
		assert.Equal(t, 180.0, buckets[2].Total)
	})

	t.Run("excludes transactions older than the window", func(t *testing.T) {
		var total float64
		for _, bucket := range buckets {
			total += bucket.Total
		}
		assert.Equal(t, 450.0, total)
	})

	t.Run("labels weeks by their monday", func(t *testing.T) {
		assert.Equal(t, "7 Sep", buckets[3].Label)
		assert.Equal(t, "31 Aug", buckets[2].Label)
	})
}

func TestBuildSpendBucketsMonthly(t *testing.T) {
	power, ok := SpendGroupByKey("power")
	require.True(t, ok)

	transactions := []models.Transaction{
		spendTransaction("Electricity services", "Mercury", -210, now),
		spendTransaction("Electricity services", "Mercury", -195, now.AddDate(0, -1, 0)),
		spendTransaction("Electricity services", "Mercury", -160, now.AddDate(0, -2, 0)),
	}

	buckets := BuildSpendBuckets(transactions, power, CadenceMonthly, 3, now)
	require.Len(t, buckets, 3)

	assert.Equal(t, 160.0, buckets[0].Total)
	assert.Equal(t, 195.0, buckets[1].Total)
	assert.Equal(t, 210.0, buckets[2].Total)
	assert.Equal(t, "Sep 26", buckets[2].Label)
}

func TestBuildSpendSummary(t *testing.T) {
	groceries, ok := SpendGroupByKey("groceries")
	require.True(t, ok)

	t.Run("compares against the last completed period", func(t *testing.T) {
		transactions := []models.Transaction{
			spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -120, day(0)),
			spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -100, day(-7)),
			spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -200, day(-14)),
		}

		summary := BuildSpendSummary(transactions, groceries, CadenceWeekly, now)

		assert.Equal(t, 120.0, summary.Current)
		assert.Equal(t, 100.0, summary.Previous)
		assert.True(t, summary.HasPrevious)
		assert.InDelta(t, 0.2, summary.Delta, 0.0001)
	})

	t.Run("averages only completed periods", func(t *testing.T) {
		transactions := []models.Transaction{
			// 120 this week must not drag the average of the completed weeks.
			spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -120, day(0)),
			spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -100, day(-7)),
			spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -200, day(-14)),
		}

		summary := BuildSpendSummary(transactions, groceries, CadenceWeekly, now)

		// 13 weekly buckets, 12 of them completed, holding 300 in total.
		assert.InDelta(t, 25.0, summary.Average, 0.0001)
	})

	t.Run("reports no comparison when the previous period is empty", func(t *testing.T) {
		transactions := []models.Transaction{
			spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -120, day(0)),
		}

		summary := BuildSpendSummary(transactions, groceries, CadenceWeekly, now)

		// The period exists, so a comparison is possible, but a zero previous
		// total leaves the delta at zero rather than dividing by it.
		assert.True(t, summary.HasPrevious)
		assert.Equal(t, 0.0, summary.Previous)
		assert.Equal(t, 0.0, summary.Delta)
	})

	t.Run("falls back to the group cadence when given a bad one", func(t *testing.T) {
		summary := BuildSpendSummary(nil, groceries, Cadence("fortnightly"), now)
		assert.Equal(t, CadenceWeekly, summary.Cadence)
		assert.Len(t, summary.Buckets, CadenceWeekly.Periods())
	})
}

func TestSpendSummaryBars(t *testing.T) {
	groceries, ok := SpendGroupByKey("groceries")
	require.True(t, ok)

	t.Run("scales against the tallest bucket", func(t *testing.T) {
		transactions := []models.Transaction{
			spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -50, day(0)),
			spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -200, day(-7)),
		}

		summary := BuildSpendSummary(transactions, groceries, CadenceWeekly, now)
		bars := summary.Bars()
		require.Len(t, bars, CadenceWeekly.Periods())

		assert.InDelta(t, 25.0, bars[len(bars)-1].Pct, 0.0001)
		assert.InDelta(t, 100.0, bars[len(bars)-2].Pct, 0.0001)
		assert.Equal(t, 0.0, bars[0].Pct)
	})

	t.Run("does not divide by zero when nothing was spent", func(t *testing.T) {
		summary := BuildSpendSummary(nil, groceries, CadenceWeekly, now)
		for _, bar := range summary.Bars() {
			assert.Equal(t, 0.0, bar.Pct)
		}
	})
}

func TestBuildSpendMerchants(t *testing.T) {
	groceries, ok := SpendGroupByKey("groceries")
	require.True(t, ok)

	transactions := []models.Transaction{
		spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -100, day(-1)),
		spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -80, day(-2)),
		spendTransaction("Convenience stores", "Parnwell Superette", -30, day(-3)),
		// No merchant name, so the description has to stand in.
		spendTransaction("Supermarkets and grocery stores", "", -25, day(-4)),
		// Outside the window.
		spendTransaction("Supermarkets and grocery stores", "New World", -500, day(-40)),
		// Wrong group.
		spendTransaction("Fuel stations", "Z Energy", -90, day(-1)),
	}
	transactions[3].Description = "COUNTDOWN 4321"

	start := day(-30)
	end := day(1)
	merchants := BuildSpendMerchants(transactions, groceries, start, end, 0)

	require.Len(t, merchants, 3)

	t.Run("ranks merchants by spend", func(t *testing.T) {
		assert.Equal(t, "PAK'nSAVE", merchants[0].Merchant)
		assert.Equal(t, 180.0, merchants[0].Total)
		assert.Equal(t, "Parnwell Superette", merchants[1].Merchant)
		assert.Equal(t, "COUNTDOWN 4321", merchants[2].Merchant)
	})

	t.Run("limits the results", func(t *testing.T) {
		assert.Len(t, BuildSpendMerchants(transactions, groceries, start, end, 2), 2)
	})
}

// TestBuildSpendBucketsAcrossLocations guards the bug that left every chart on
// the insights page reading zero. Akahu stores transaction dates in UTC while
// time.Now() is local, and bucket lookup is by time.Time map key, which compares
// location as well as instant. Both sides must be read in one location.
func TestBuildSpendBucketsAcrossLocations(t *testing.T) {

	// A +12 zone, as far from UTC as the app is ever likely to run.
	nz := time.FixedZone("NZST", 12*60*60)
	reference := time.Date(2026, time.September, 8, 12, 0, 0, 0, nz)

	power, ok := SpendGroupByKey("power")
	require.True(t, ok)

	transactions := []models.Transaction{
		// Stored as UTC, exactly as the feed delivers it.
		spendTransaction("Electricity services", "Mercury", -186.40, time.Date(2026, time.September, 3, 11, 0, 0, 0, time.UTC)),
		spendTransaction("Electricity services", "Mercury", -172.10, time.Date(2026, time.August, 4, 11, 0, 0, 0, time.UTC)),
	}

	buckets := BuildSpendBuckets(transactions, power, CadenceMonthly, 3, reference)
	require.Len(t, buckets, 3)

	t.Run("totals transactions stored in another location", func(t *testing.T) {
		assert.Equal(t, 172.10, buckets[1].Total)
		assert.Equal(t, 186.40, buckets[2].Total)
	})

	t.Run("the summary is not left empty", func(t *testing.T) {
		summary := BuildSpendSummary(transactions, power, CadenceMonthly, reference)
		assert.Equal(t, 186.40, summary.Current)
		assert.Equal(t, 172.10, summary.Previous)

		// A chart of nothing but zeroes is the shape the bug produced.
		var total float64
		for _, value := range summary.Totals() {
			total += value
		}
		assert.Greater(t, total, 0.0)
	})

	t.Run("a late evening purchase stays in its local month", func(t *testing.T) {
		// 31 Aug 23:00 UTC is 1 Sep 11:00 in +12, so this belongs to September.
		late := []models.Transaction{
			spendTransaction("Electricity services", "Mercury", -50, time.Date(2026, time.August, 31, 23, 0, 0, 0, time.UTC)),
		}
		buckets := BuildSpendBuckets(late, power, CadenceMonthly, 3, reference)
		assert.Equal(t, 0.0, buckets[1].Total)
		assert.Equal(t, 50.0, buckets[2].Total)
	})
}

// TestSpendSummaryHeadline covers the dashboard tile leading with a real
// reading. A group billed once a month sits at zero until the bill lands, and
// "$0.00, down 100%" is a wrong answer to "what is my power bill?".
func TestSpendSummaryHeadline(t *testing.T) {
	power, ok := SpendGroupByKey("power")
	require.True(t, ok)

	t.Run("falls back to the last reading while the month is unbilled", func(t *testing.T) {
		transactions := []models.Transaction{
			spendTransaction("Electricity services", "Mercury", -245.83, now.AddDate(0, -1, 0)),
			spendTransaction("Electricity services", "Mercury", -209.32, now.AddDate(0, -2, 0)),
		}

		summary := BuildSpendSummary(transactions, power, CadenceMonthly, now)

		assert.Equal(t, 0.0, summary.Current)
		assert.True(t, summary.Awaiting())

		assert.True(t, summary.HeadlineIsPrevious)
		assert.Equal(t, 245.83, summary.Headline)
		assert.Equal(t, "Last month", summary.HeadlineLabel())

		// Compared against the bill before it, not against the empty month.
		assert.True(t, summary.HasHeadlineDelta)
		assert.Equal(t, 209.32, summary.HeadlinePrevious)
		assert.InDelta(t, (245.83-209.32)/209.32, summary.HeadlineDelta, 0.0001)
		assert.Equal(t, "the month before", summary.HeadlineNote())
	})

	t.Run("leads with the current period once it has a reading", func(t *testing.T) {
		transactions := []models.Transaction{
			spendTransaction("Electricity services", "Mercury", -186.40, now),
			spendTransaction("Electricity services", "Mercury", -245.83, now.AddDate(0, -1, 0)),
		}

		summary := BuildSpendSummary(transactions, power, CadenceMonthly, now)

		assert.False(t, summary.HeadlineIsPrevious)
		assert.Equal(t, 186.40, summary.Headline)
		assert.Equal(t, "This month", summary.HeadlineLabel())
		assert.Equal(t, 245.83, summary.HeadlinePrevious)
	})

	t.Run("offers no delta when there is only one reading", func(t *testing.T) {
		transactions := []models.Transaction{
			spendTransaction("Electricity services", "Mercury", -245.83, now.AddDate(0, -1, 0)),
		}

		summary := BuildSpendSummary(transactions, power, CadenceMonthly, now)

		assert.Equal(t, 245.83, summary.Headline)
		assert.False(t, summary.HasHeadlineDelta)
	})

	t.Run("a rolling window always leads with its current window", func(t *testing.T) {
		groceries, ok := SpendGroupByKey("groceries")
		require.True(t, ok)

		transactions := []models.Transaction{
			spendTransaction("Supermarkets and grocery stores", "PAK'nSAVE", -140, day(-2)),
			spendTransaction("Supermarkets and grocery stores", "New World", -190, day(-9)),
		}

		summary := BuildRollingSummary(transactions, groceries, 7, 4, now)

		assert.False(t, summary.HeadlineIsPrevious)
		assert.Equal(t, 140.0, summary.Headline)
		assert.Equal(t, "Last 7 days", summary.HeadlineLabel())
		assert.Equal(t, 190.0, summary.HeadlinePrevious)
	})
}
