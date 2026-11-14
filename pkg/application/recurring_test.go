package application

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// recurringNow is a Monday morning, as the browser tests' is.
var recurringNow = time.Date(2026, time.June, 15, 9, 0, 0, 0, time.UTC)

// paid is a payment to merchant, filed by Akahu's specific category, on
// date at midnight UTC as Akahu dates them.
func paid(merchant, specific string, amount float64, date time.Time) models.Transaction {
	tx := spendTx(fmt.Sprintf("%s-%s-%d", merchant, date.Format(time.DateOnly), len(merchant)), "EFTPOS", "", specific, merchant, "", -amount, date)
	tx.Id += fmt.Sprintf("-%.2f", amount)
	return tx
}

// payments is the same payment on each of dates.
func payments(merchant, specific string, amount float64, dates ...time.Time) []models.Transaction {
	var txs []models.Transaction
	for _, date := range dates {
		txs = append(txs, paid(merchant, specific, amount, date))
	}
	return txs
}

// every is count dates, step days apart, the last on last.
func every(step, count int, last time.Time) []time.Time {
	dates := make([]time.Time, count)
	for i := range dates {
		dates[i] = last.AddDate(0, 0, -step*(count-1-i))
	}
	return dates
}

// monthly is count dates a month apart, on day of the month, the last
// in the month back months before now.
func monthly(day, count, back int) []time.Time {
	dates := make([]time.Time, count)
	for i := range dates {
		dates[i] = time.Date(2026, time.June-time.Month(back+count-1-i), day, 0, 0, 0, 0, time.UTC)
	}
	return dates
}

// history starts the transactions long ago, as a real account's do, so a
// payment that starts later is known to be new.
func history() models.Transaction {
	return spendTx("salary-2024", "DIRECT CREDIT", "", "", "", "ACME LTD SALARY", 3250, recurringNow.AddDate(-2, 0, 0))
}

func days(n int) time.Time {
	return time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC).AddDate(0, 0, n)
}

// findOne finds the recurring payments in txs, and the one for merchant.
func findOne(t *testing.T, txs []models.Transaction, merchant string) (RecurringPayment, bool) {
	t.Helper()
	for _, payment := range FindRecurring(append(txs, history()), recurringNow) {
		if payment.Name == merchant {
			return payment, true
		}
	}
	return RecurringPayment{}, false
}

func TestFindRecurringCadences(t *testing.T) {
	tests := []struct {
		name    string
		dates   []time.Time
		cadence string
		next    time.Time
	}{
		{"weekly", every(7, 8, days(-2)), "weekly", days(5)},
		{"fortnightly", every(14, 6, days(-6)), "fortnightly", days(8)},
		{"monthly", monthly(5, 12, 0), "monthly", time.Date(2026, time.July, 5, 0, 0, 0, 0, time.UTC)},
		{"quarterly", []time.Time{
			time.Date(2025, time.June, 1, 0, 0, 0, 0, time.UTC), time.Date(2025, time.September, 1, 0, 0, 0, 0, time.UTC),
			time.Date(2025, time.December, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
		}, "quarterly", time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)},
		{"yearly", []time.Time{days(-365 - 361), days(-361)}, "yearly", days(4)},
		{"monthly a few days either way", []time.Time{
			days(-120), days(-92), days(-60), days(-31), days(-1),
		}, "monthly", days(29)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payment, ok := findOne(t, payments("Merchant", "Insurance", 50, test.dates...), "Merchant")
			require.True(t, ok, "it is found")
			assert.Equal(t, test.cadence, payment.Cadence.Key)
			assert.Equal(t, test.next.Format(time.DateOnly), payment.Next.Format(time.DateOnly), "it is next due a period after the last")
			assert.Equal(t, len(test.dates), payment.Payments)
		})
	}
}

func TestFindRecurringRejects(t *testing.T) {
	tests := []struct {
		name string
		txs  []models.Transaction
	}{
		{"a supermarket visited every few days", payments("Pak'nSave", "Supermarkets and grocery stores", 60,
			days(-30), days(-27), days(-23), days(-20), days(-16), days(-13), days(-9), days(-6), days(-2))},
		{"a supermarket visited twice a week", payments("Pak'nSave", "Supermarkets and grocery stores", 60,
			days(-56), days(-53), days(-49), days(-46), days(-42), days(-39), days(-35), days(-28), days(-25), days(-21), days(-14), days(-11), days(-7), days(-4), days(0))},
		// Most gaps are a week, but a shop visited again the next day now and
		// then is visited more than once a week.
		{"a supermarket visited weekly with trips between", payments("Pak'nSave", "Supermarkets and grocery stores", 60,
			days(-44), days(-37), days(-30), days(-23), days(-22), days(-15), days(-8), days(-1), days(0))},
		{"too few weekly payments", payments("Merchant", "", 10, every(7, 2, days(-1))...)},
		{"too few monthly payments", payments("Merchant", "", 10, monthly(3, 2, 0)...)},
		{"one yearly payment", payments("Merchant", "", 10, days(-30))},
		{"gaps no cadence has", payments("Z Energy", "Fuel stations", 90, every(10, 9, days(-1))...)},
		{"gaps too uneven", payments("Merchant", "", 30, days(-200), days(-170), days(-150), days(-100), days(-90), days(-60), days(-35), days(-1))},
		{"a transfer between accounts", func() []models.Transaction {
			txs := payments("Transfer", "", 250, monthly(1, 6, 0)...)
			for i := range txs {
				txs[i].Type = "TRANSFER"
			}
			return txs
		}()},
		{"a credit card repayment", func() []models.Transaction {
			txs := payments("Visa", "", 600, monthly(1, 6, 0)...)
			for i := range txs {
				txs[i].Type = "CREDIT CARD"
			}
			return txs
		}()},
		{"loan interest", func() []models.Transaction {
			var txs []models.Transaction
			for _, date := range monthly(28, 6, 1) {
				txs = append(txs, spendTx("interest-"+date.Format(time.DateOnly), "LOAN", "", "", "", "LOAN INTEREST", -1900, date))
			}
			return txs
		}()},
		{"money coming in", func() []models.Transaction {
			txs := payments("ACME", "", 3250, every(14, 6, days(-3))...)
			for i := range txs {
				txs[i].Amount = -txs[i].Amount
			}
			return txs
		}()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Empty(t, FindRecurring(test.txs, recurringNow))
		})
	}
}

func TestFindRecurringGroupsByMerchant(t *testing.T) {
	t.Run("a description with no merchant, cleaned of its card time", func(t *testing.T) {
		var txs []models.Transaction
		for i, date := range monthly(9, 4, 0) {
			txs = append(txs, spendTx(fmt.Sprintf("gym-%d", i), "EFTPOS", "", "", "", fmt.Sprintf("EFTPOS LOCAL GYM 4421-10:%02d", 10+i), -30, date))
		}
		found := FindRecurring(txs, recurringNow)
		require.Len(t, found, 1)
		assert.Equal(t, "monthly", found[0].Cadence.Key)
		assert.Equal(t, "local gym 4421", found[0].Key)
		assert.Equal(t, "LOCAL GYM 4421", found[0].Name)
	})

	t.Run("two charges on one day are one payment", func(t *testing.T) {
		txs := payments("Neon", "", 10, monthly(20, 4, 1)...)
		txs = append(txs, paid("Neon", "", 10, monthly(20, 1, 1)[0]))
		payment, ok := findOne(t, txs, "Neon")
		require.True(t, ok)
		assert.Equal(t, "monthly", payment.Cadence.Key)
		assert.Equal(t, 4, payment.Payments)
		assert.Equal(t, 20.0, payment.Latest, "the day's charges together")
	})

	t.Run("a merchant with a quote in its name is named", func(t *testing.T) {
		payment, ok := findOne(t, payments("Pak'nSave Online", "Supermarkets and grocery stores", 180, every(7, 6, days(-1))...), "Pak'nSave Online")
		require.True(t, ok)
		assert.Equal(t, "pak'nsave online", payment.Key)
		assert.Equal(t, "/transactions?search=Pak%27nSave+Online", payment.Link())
	})
}

func TestFindRecurringAmounts(t *testing.T) {
	t.Run("a subscription is a fixed price", func(t *testing.T) {
		payment, ok := findOne(t, payments("Spark", "Internet services", 89.99, monthly(17, 8, 1)...), "Spark")
		require.True(t, ok)
		assert.True(t, payment.Fixed)
		assert.False(t, payment.PriceChanged)
		assert.Equal(t, 89.99, payment.Usual)
		assert.InDelta(t, 89.99*12, payment.Yearly(), 0.001)
		assert.Equal(t, "bi-wifi", payment.Icon, "a subcategory's own icon")
		assert.Equal(t, "plum", payment.Accent, "in its category's colour")
	})

	t.Run("a bill varies", func(t *testing.T) {
		var txs []models.Transaction
		for i, date := range monthly(16, 8, 0) {
			txs = append(txs, paid("Mercury", "Electricity services", []float64{120, 135, 150, 210, 236, 248, 190, 160}[i], date))
		}
		payment, ok := findOne(t, txs, "Mercury")
		require.True(t, ok)
		assert.False(t, payment.Fixed)
		assert.False(t, payment.PriceChanged, "a bill moving is not a price change")
		assert.Equal(t, 200.0, payment.Usual, "the middle of the last six")
	})

	t.Run("a card charged in another currency moves by cents", func(t *testing.T) {
		var txs []models.Transaction
		for i, date := range monthly(2, 5, 0) {
			txs = append(txs, paid("GitHub", "", []float64{25.31, 25.87, 25.12, 25.66, 25.98}[i], date))
		}
		payment, ok := findOne(t, txs, "GitHub")
		require.True(t, ok)
		assert.True(t, payment.Fixed)
		assert.False(t, payment.PriceChanged)
	})

	t.Run("a price rise is reported at the payment it came in", func(t *testing.T) {
		txs := payments("Neon", "", 17.99, monthly(20, 5, 1)...)
		txs = append(txs, paid("Neon", "", 19.99, monthly(20, 1, 0)[0]))
		payment, ok := findOne(t, txs, "Neon")
		require.True(t, ok)
		assert.True(t, payment.Fixed, "one rise leaves a fixed price fixed")
		assert.True(t, payment.PriceChanged)
		assert.Equal(t, 19.99, payment.Usual, "it costs what it was last charged")
		assert.Equal(t, 17.99, payment.Previous)
		assert.InDelta(t, 0.111, payment.Change(), 0.001)
	})

	t.Run("but not again at the next", func(t *testing.T) {
		txs := payments("Neon", "", 17.99, monthly(20, 4, 2)...)
		txs = append(txs, payments("Neon", "", 19.99, monthly(20, 2, 0)...)...)
		payment, ok := findOne(t, txs, "Neon")
		require.True(t, ok)
		assert.True(t, payment.Fixed)
		assert.False(t, payment.PriceChanged)
		assert.Equal(t, 19.99, payment.Usual)
	})

	t.Run("a few cents is not a price change", func(t *testing.T) {
		txs := payments("Neon", "", 17.99, monthly(20, 5, 1)...)
		txs = append(txs, paid("Neon", "", 18.39, monthly(20, 1, 0)[0]))
		payment, ok := findOne(t, txs, "Neon")
		require.True(t, ok)
		assert.False(t, payment.PriceChanged)
	})
}

func TestFindRecurringStatus(t *testing.T) {
	t.Run("a payment a little late is still active", func(t *testing.T) {
		payment, ok := findOne(t, payments("Neon", "", 18, monthly(1, 6, 1)...), "Neon")
		require.True(t, ok)
		assert.False(t, payment.Stopped)
	})

	t.Run("one unpaid for two periods past its due date has stopped", func(t *testing.T) {
		// Last paid on 1 Feb, so due on 1 Mar, and two months past that by 1 May.
		payment, ok := findOne(t, payments("Lightbox", "", 13.99, monthly(1, 8, 4)...), "Lightbox")
		require.True(t, ok)
		assert.True(t, payment.Stopped)
		assert.False(t, payment.ComingUp)
	})

	t.Run("a payment that has just become regular is new", func(t *testing.T) {
		payment, ok := findOne(t, payments("CityFitness", "Gyms, fitness, aquatic facilities, yoga, pilates", 24.5, every(14, 3, days(-2))...), "CityFitness")
		require.True(t, ok)
		assert.True(t, payment.New)
		assert.Equal(t, days(-2), payment.Recognised)
	})

	t.Run("but not once a period has gone by", func(t *testing.T) {
		payment, ok := findOne(t, payments("CityFitness", "", 24.5, every(14, 4, days(-2))...), "CityFitness")
		require.True(t, ok)
		assert.False(t, payment.New)
	})

	t.Run("nor when the history starts with it", func(t *testing.T) {
		found := FindRecurring(payments("CityFitness", "", 24.5, every(14, 3, days(-2))...), recurringNow)
		require.Len(t, found, 1)
		assert.False(t, found[0].New, "it may have been paid long before the history starts")
	})

	t.Run("a yearly payment renewing is not new", func(t *testing.T) {
		payment, ok := findOne(t, payments("AA Insurance", "Insurance", 1140, days(-365*2), days(-365), days(-1)), "AA Insurance")
		require.True(t, ok)
		assert.False(t, payment.New)
	})
}

func TestFindRecurringComingUp(t *testing.T) {
	tests := []struct {
		name              string
		txs               []models.Transaction
		comingUp, dueSoon bool
	}{
		{"large and yearly, due within the week", payments("AA Insurance", "Insurance", 1140, days(-365-361), days(-361)), true, true},
		{"large and monthly, due within the week", payments("Rent", "", 2200, every(30, 4, days(-25))...), true, true},
		{"small and monthly, due within the week", payments("Neon", "", 19.99, every(30, 4, days(-27))...), true, false},
		{"quarterly over the smaller amount", payments("Rates", "Local government", 120, every(91, 3, days(-86))...), true, true},
		{"large, but due later in the fortnight", payments("Rent", "", 2200, every(30, 4, days(-20))...), true, false},
		{"large, but due after the fortnight", payments("Rent", "", 2200, every(30, 4, days(-10))...), false, false},
		{"large, but overdue", payments("Rent", "", 2200, every(30, 4, days(-32))...), false, false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			found := FindRecurring(test.txs, recurringNow)
			require.Len(t, found, 1)
			assert.Equal(t, test.comingUp, found[0].ComingUp, "coming up")
			assert.Equal(t, test.dueSoon, found[0].DueSoon, "due soon")
		})
	}
}

func TestRecurringCadenceAfter(t *testing.T) {
	cadence := recurringCadences[2]
	assert.Equal(t, "2026-02-28", cadence.after(time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC), 1).Format(time.DateOnly),
		"the 31st falls on the last day of a shorter month")
	assert.Equal(t, "2025-12-31", cadence.after(time.Date(2026, time.January, 31, 0, 0, 0, 0, time.UTC), -1).Format(time.DateOnly))
	assert.Equal(t, "2027-02-28", recurringCadences[4].after(time.Date(2026, time.February, 28, 0, 0, 0, 0, time.UTC), 1).Format(time.DateOnly))
	assert.Equal(t, "2026-06-29", recurringCadences[1].after(time.Date(2026, time.June, 15, 0, 0, 0, 0, time.UTC), 1).Format(time.DateOnly))
}

func TestBuildRecurringView(t *testing.T) {
	var txs []models.Transaction
	txs = append(txs, payments("Spark", "Internet services", 90, every(30, 6, days(-20))...)...)
	txs = append(txs, payments("Little Bird Cafe", "Cafes and restaurants", 6, every(7, 10, days(-1))...)...)
	txs = append(txs, payments("Neon", "", 20, every(30, 6, days(-5))...)...)
	txs = append(txs, payments("Lightbox", "", 14, every(30, 6, days(-150))...)...)
	txs = append(txs, payments("AA Insurance", "Insurance", 1140, days(-365-361), days(-361))...)
	found := FindRecurring(txs, recurringNow)
	require.Len(t, found, 5)

	view := BuildRecurringView(found, map[string]string{
		"little bird cafe": models.RecurringNotRecurring,
		"neon":             models.RecurringCancelled,
		"elsewhere":        models.RecurringCancelled,
	})

	names := func(payments []RecurringPayment) (names []string) {
		for _, p := range payments {
			names = append(names, p.Name)
		}
		return names
	}
	assert.Equal(t, []string{"Little Bird Cafe"}, names(view.Hidden), "marked not recurring")
	assert.Equal(t, []string{"Lightbox", "Neon"}, names(view.Stopped), "gone quiet, or marked cancelled")
	assert.Equal(t, models.RecurringCancelled, view.Stopped[1].Mark, "so it can be undone")
	assert.Empty(t, view.Stopped[0].Mark, "while one that went quiet has nothing to undo")

	require.Len(t, view.Groups, 2)
	assert.Equal(t, "monthly", view.Groups[0].Cadence.Key, "weekly first, and there is none")
	assert.Equal(t, []string{"Spark"}, names(view.Groups[0].Payments))
	assert.Equal(t, "yearly", view.Groups[1].Cadence.Key)

	assert.Equal(t, 2, view.Count)
	assert.InDelta(t, 90*12+1140, view.Yearly, 0.001, "only what is active counts")
	assert.InDelta(t, (90*12+1140)/12.0, view.Monthly(), 0.001)
	assert.Equal(t, []string{"AA Insurance", "Spark"}, names(view.ComingUp), "soonest first")
	assert.False(t, view.Empty())
	assert.True(t, BuildRecurringView(nil, nil).Empty())
}

func TestRecurringMarksSilence(t *testing.T) {
	txs := payments("AA Insurance", "Insurance", 1140, days(-365-361), days(-361))
	found := FindRecurring(txs, recurringNow)
	require.Len(t, found, 1)
	require.True(t, found[0].DueSoon)

	for _, mark := range []string{models.RecurringNotRecurring, models.RecurringCancelled} {
		marked := applyRecurringMarks(found, map[string]string{"aa insurance": mark})
		assert.False(t, marked[0].DueSoon, "%s raises nothing", mark)
		assert.False(t, marked[0].ComingUp, "%s is not expected", mark)
	}
	assert.True(t, found[0].DueSoon, "what was found is left as it was")
}

func TestRecurringDetail(t *testing.T) {
	nb := func(s string) string { return strings.ReplaceAll(s, " ", "\u00a0") }
	power := RecurringPayment{Usual: 148.5, Cadence: recurringCadences[2], Next: days(30), DaysAhead: 30}
	assert.Equal(t, nb("usual $148.50")+" · monthly · varies · "+nb("next Wed 15 Jul"), power.Detail(), "each part keeps together")

	power.Fixed, power.DaysAhead = true, 1
	assert.Equal(t, nb("usual $148.50")+" · monthly · "+nb("next tomorrow"), power.Detail())

	power.DaysAhead, power.Next = -3, days(-3)
	assert.Equal(t, nb("usual $148.50")+" · monthly · "+nb("expected 12 Jun"), power.Detail(), "a late one says when it was expected")

	power.Stopped, power.Last = true, days(-100)
	assert.Equal(t, nb("usual $148.50")+" · monthly · "+nb("last paid 7 Mar 2026"), power.Detail())
}
