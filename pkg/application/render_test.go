package application

import (
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFmtCurrency(t *testing.T) {
	format := templateFuncs()["fmtCurrency"].(func(float64) string)

	tests := []struct {
		name     string
		input    float64
		expected string
	}{
		{"positive", 1365.70, "$1,365.70"},
		{"negative puts the sign before the symbol", -1365.70, "-$1,365.70"},
		{"zero", 0, "$0.00"},
		{"large negative groups thousands", -295841, "-$295,841.00"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, format(test.input))
		})
	}
}

func TestRenderDashboard(t *testing.T) {

	page := renderTemplate(t, "dashboard", DashboardData{
		BalanceCard: CardData{Total: 12400, PreviousTotal: 11800, Delta: 0.0508},
		SpendCard:   CardData{Total: -2310.55, PreviousTotal: -2050.10, Delta: 0.127},
		IncomeCard:  CardData{Total: 4200, PreviousTotal: 4200},
		SavingsCard: CardData{Total: 1889.45, PreviousTotal: 2149.90, Delta: -0.121},
		SpendTimeseries: TimeseriesData{
			Labels: []string{"Apr 26", "May 26"},
			Data:   []int{2050, 2310},
		},
		SpendDoughnut: DoughnutData{
			Labels: []string{"Food 40%", "Transport 25%"},
			Data:   []int{40, 25},
		},
		TopMerchants: []models.MerchantTotal{
			{Merchant: "PAKnSAVE", Total: 322.50, PreviousTotal: 280, Delta: 0.152},
			{Merchant: "Z Energy", Total: 95},
		},
		HighestOutgoingTransactions: []OutgoingTransaction{
			{Description: "Mercury", Amount: -186.40, Date: time.Now().AddDate(0, 0, -5)},
		},
	})

	t.Run("shows the headline metrics", func(t *testing.T) {
		assert.Contains(t, page, "Total Balance")
		assert.Contains(t, page, "$12,400.00")
		// Spend is shown as a magnitude, not as a negative balance movement.
		assert.Contains(t, page, "$2,310.55")
		assert.NotContains(t, page, "-$2,310.55")
	})

	t.Run("ranks merchants with a share bar", func(t *testing.T) {
		assert.Contains(t, page, "Top Merchants")
		assert.Contains(t, page, "b-rank-fill")
		// The leader fills the bar completely.
		assert.Contains(t, page, "width: 100.0%")
	})

	t.Run("shows biggest transactions as positive magnitudes", func(t *testing.T) {
		assert.Contains(t, page, "Biggest Transactions")
		assert.Contains(t, page, "$186.40")
		assert.NotContains(t, page, "-$186.40")
	})

	t.Run("no longer offers a share button", func(t *testing.T) {
		assert.NotContains(t, page, ">Share<")
	})
}

func TestRenderTransactions(t *testing.T) {

	tx := func(merchant string, amount float64, offset int) models.Transaction {
		var t models.Transaction
		t.Merchant.Name = merchant
		t.Amount = amount
		t.Date = time.Date(2026, time.September, 8, 12, 0, 0, 0, time.UTC).AddDate(0, 0, offset)
		return t
	}

	transactions := []models.Transaction{
		tx("PAKnSAVE", -142.50, -1),
		tx("Z Energy", -95.00, -1),
		tx("", 2100.00, -2),
	}
	transactions[2].Description = "PAYROLL"

	render := func(page []models.Transaction, total int) string {
		return renderTemplate(t, "transactions", map[string]interface{}{
			"accounts":  []models.Account{account("everyday", "Everyday", "Kiwibank", "CHECKING", 1100)},
			"days":      GroupTransactionsByDay(page, time.UTC),
			"search":    "",
			"account":   "",
			"took":      3 * time.Millisecond,
			"in":        2100.00,
			"out":       -237.50,
			"total":     total,
			"showing":   len(page),
			"hasMore":   total > len(page),
			"nextLimit": 200,
		})
	}

	page := render(transactions, 3)

	t.Run("groups transactions under their day", func(t *testing.T) {
		assert.Contains(t, page, "Mon 7 Sep 2026")
		assert.Contains(t, page, "Sun 6 Sep 2026")
	})

	t.Run("renders a card list for phones and a table for desktop", func(t *testing.T) {
		assert.Contains(t, page, "b-tx-list")
		assert.Contains(t, page, "table-container")
		assert.Contains(t, page, "d-md-none")
		assert.Contains(t, page, "d-none d-md-block")
	})

	t.Run("falls back to the description when there is no merchant", func(t *testing.T) {
		assert.Contains(t, page, "PAYROLL")
	})

	t.Run("hides the load more control when everything is shown", func(t *testing.T) {
		assert.NotContains(t, page, "Load more")
	})

	t.Run("offers load more when the result set is truncated", func(t *testing.T) {
		truncated := render(transactions, 5356)
		assert.Contains(t, truncated, "Load more")
		assert.Contains(t, truncated, "limit=200")
		assert.Contains(t, truncated, "3 of 5356")
	})
}

func TestGroupTransactionsByDay(t *testing.T) {

	at := func(d time.Time, amount float64) models.Transaction {
		return models.Transaction{Account: "a", Amount: amount, Date: d}
	}
	day1 := time.Date(2026, time.September, 7, 12, 0, 0, 0, time.UTC)
	day2 := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)

	days := GroupTransactionsByDay([]models.Transaction{
		at(day1, -100),
		at(day1.Add(-2*time.Hour), -50),
		at(day2, 1000),
	}, time.UTC)
	require.Len(t, days, 2)

	t.Run("keeps the order it was given", func(t *testing.T) {
		assert.True(t, days[0].Date.After(days[1].Date))
	})

	t.Run("nets off the day", func(t *testing.T) {
		assert.Len(t, days[0].Rows, 2)
		assert.Equal(t, -150.0, days[0].Net)
		assert.Equal(t, 1000.0, days[1].Net)
	})

	t.Run("reads UTC dates in the given location", func(t *testing.T) {
		// 23:00 UTC on the 7th is 11:00 on the 8th in +12, so in that zone this
		// belongs to the 8th, not the 7th.
		nz := time.FixedZone("NZST", 12*60*60)
		late := []models.Transaction{at(time.Date(2026, time.September, 7, 23, 0, 0, 0, time.UTC), -60)}

		assert.Equal(t, 7, GroupTransactionsByDay(late, time.UTC)[0].Date.Day())
		assert.Equal(t, 8, GroupTransactionsByDay(late, nz)[0].Date.Day())
	})
}

func TestRenderAccountsStyling(t *testing.T) {
	loan := account("loan", "Mortgage", "Kiwibank", "LOAN", -250000)
	loan.FormattedAccount = "38-9000-0000000-00"

	accounts := []models.Account{
		account("everyday", "Everyday", "Kiwibank", "CHECKING", 1100),
		loan,
	}
	transactions := []models.Transaction{transaction("everyday", 100)}

	refreshed, hasRefreshed := OldestRefresh(accounts)
	page := renderTemplate(t, "accounts", AccountsListProps{
		Portfolio:     BuildPortfolio(accounts),
		Groups:        BuildAccountGroups(accounts, transactions, hasTransactions(transactions)),
		LastRefreshed: refreshed,
		HasRefreshed:  hasRefreshed,
	})

	t.Run("uses the shared component language", func(t *testing.T) {
		for _, class := range []string{"b-page-head", "b-stat", "b-conn", "b-acct", "b-delta"} {
			assert.Contains(t, page, class)
		}
	})

	t.Run("marks each account with an icon for its type", func(t *testing.T) {
		assert.Contains(t, page, "bi-wallet2") // checking
		assert.Contains(t, page, "bi-house")   // loan
	})

	t.Run("links an account that has history", func(t *testing.T) {
		assert.Contains(t, page, `href="/accounts/everyday"`)
	})

	t.Run("does not link an account with nothing to chart", func(t *testing.T) {
		// The detail page would be an empty chart above six zeroed statistics.
		assert.NotContains(t, page, `href="/accounts/loan"`)
		assert.Contains(t, page, "b-acct-static")
	})
}

func TestAccountTypeIcon(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"mapped type", "SAVINGS", "bi-piggy-bank"},
		{"case insensitive", "creditcard", "bi-credit-card"},
		{"unmapped type falls back", "SOMETHING", "bi-bank2"},
		{"empty falls back", "", "bi-bank2"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, AccountTypeIcon(test.input))
		})
	}
}

// TestRenderLayout guards the navigation shell, which every page depends on.
func TestRenderLayout(t *testing.T) {
	page := renderTemplate(t, "layout", map[string]interface{}{"content": ""})

	t.Run("carries a mobile tab bar and a desktop nav", func(t *testing.T) {
		assert.Contains(t, page, "b-tabbar")
		assert.Contains(t, page, "b-nav-link")
	})

	t.Run("links every destination", func(t *testing.T) {
		for _, href := range []string{"/", "/transactions", "/accounts", "/budget", "/settings"} {
			assert.Contains(t, page, `href="`+href+`"`)
		}
	})

	t.Run("marks the active destination from the url", func(t *testing.T) {
		assert.Contains(t, page, "markActiveNav")
		// The old click-only hyperscript could not survive a reload.
		assert.NotContains(t, page, "on click remove .active")
	})

	t.Run("does not double count the tab bar height", func(t *testing.T) {
		assert.Equal(t, 1, strings.Count(page, `class="b-tabbar`))
	})

	t.Run("tapping the current destination scrolls to the top", func(t *testing.T) {
		assert.Contains(t, page, "htmx:beforeRequest")
		assert.Contains(t, page, "window.scrollTo")
		assert.Contains(t, page, "window.location.pathname")
	})

	t.Run("resets scroll only when the path changes", func(t *testing.T) {
		// A query-only swap, such as a filter or another page of results, must
		// leave the reader where they are.
		assert.Contains(t, page, "lastPath")
		assert.Contains(t, page, "if (path === lastPath)")
	})
}
