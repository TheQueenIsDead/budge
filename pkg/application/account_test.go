package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// account builds a minimal account for the list page tests.
func account(id, name, connection, accountType string, balance float64) models.Account {
	var a models.Account
	a.Id = id
	a.Name = name
	a.Type = accountType
	a.Status = "ACTIVE"
	a.Connection.Name = connection
	a.Balance.Current = balance
	return a
}

func transaction(accountId string, amount float64) models.Transaction {
	return models.Transaction{Account: accountId, Amount: amount}
}

func TestAccountTypeLabel(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"mapped multi word type", "CREDITCARD", "Credit Card"},
		{"mapped type is case insensitive", "creditcard", "Credit Card"},
		{"unmapped type is title cased", "CHECKING", "Checking"},
		{"empty type falls back", "", "Account"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, AccountTypeLabel(test.input))
		})
	}
}

func TestBuildPortfolio(t *testing.T) {
	tests := []struct {
		name     string
		accounts []models.Account
		expected Portfolio
	}{
		{
			"no accounts",
			nil,
			Portfolio{},
		},
		{
			"assets only",
			[]models.Account{
				account("a", "Everyday", "Bank", "CHECKING", 1000),
				account("b", "Savings", "Bank", "SAVINGS", 500),
			},
			Portfolio{Assets: 1500, Liabilities: 0, NetWorth: 1500, Accounts: 2},
		},
		{
			"debt is split out by the sign of the balance",
			[]models.Account{
				account("a", "Everyday", "Bank", "CHECKING", 1000),
				account("b", "Mortgage", "Bank", "LOAN", -250000),
				account("c", "Credit", "Bank", "CREDITCARD", -1200),
			},
			Portfolio{Assets: 1000, Liabilities: -251200, NetWorth: -250200, Accounts: 3},
		},
		{
			"a zero balance counts as an asset and not a liability",
			[]models.Account{account("a", "Empty", "Bank", "SAVINGS", 0)},
			Portfolio{Assets: 0, Liabilities: 0, NetWorth: 0, Accounts: 1},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, BuildPortfolio(test.accounts))
		})
	}
}

func TestBuildAccountGroups(t *testing.T) {

	t.Run("groups accounts under their connection", func(t *testing.T) {
		groups := BuildAccountGroups([]models.Account{
			account("a", "Everyday", "Kiwibank", "CHECKING", 100),
			account("b", "Savings", "ANZ", "SAVINGS", 900),
			account("c", "Bills", "Kiwibank", "CHECKING", 200),
		}, nil)

		assert.Len(t, groups, 2)
		// ANZ leads on a total of 900, ahead of Kiwibank's 300.
		assert.Equal(t, "ANZ", groups[0].Connection)
		assert.Equal(t, 900.0, groups[0].Total)
		assert.Equal(t, "Kiwibank", groups[1].Connection)
		assert.Equal(t, 300.0, groups[1].Total)
		// Within a group the larger balance sorts first.
		assert.Equal(t, "Bills", groups[1].Accounts[0].Account.Name)
		assert.Equal(t, "Everyday", groups[1].Accounts[1].Account.Name)
	})

	t.Run("attributes balance movement to the right account", func(t *testing.T) {
		groups := BuildAccountGroups(
			[]models.Account{
				account("a", "Everyday", "Bank", "CHECKING", 1100),
				account("b", "Savings", "Bank", "SAVINGS", 2000),
			},
			[]models.Transaction{
				transaction("a", 300),
				transaction("a", -200),
				transaction("b", 500),
				transaction("unknown", 9999),
			},
		)

		accounts := groups[0].Accounts
		assert.Equal(t, "Savings", accounts[0].Account.Name)
		assert.Equal(t, 500.0, accounts[0].Change)
		assert.Equal(t, 1500.0, accounts[0].PreviousBalance)

		assert.Equal(t, "Everyday", accounts[1].Account.Name)
		assert.Equal(t, 100.0, accounts[1].Change)
		assert.Equal(t, 1000.0, accounts[1].PreviousBalance)
		assert.InDelta(t, 0.1, accounts[1].Delta, 0.0001)
		assert.True(t, accounts[1].HasHistory)
	})

	t.Run("an account without transactions reports no history", func(t *testing.T) {
		groups := BuildAccountGroups(
			[]models.Account{account("a", "Dormant", "Bank", "SAVINGS", 50)},
			[]models.Transaction{transaction("b", 100)},
		)

		summary := groups[0].Accounts[0]
		assert.False(t, summary.HasHistory)
		assert.Zero(t, summary.Change)
		assert.Zero(t, summary.Delta)
		assert.Equal(t, 50.0, summary.PreviousBalance)
	})

	t.Run("a zero previous balance does not divide by zero", func(t *testing.T) {
		groups := BuildAccountGroups(
			// The account opened during the window: it holds 500 and all of it arrived.
			[]models.Account{account("a", "New", "Bank", "SAVINGS", 500)},
			[]models.Transaction{transaction("a", 500)},
		)

		summary := groups[0].Accounts[0]
		assert.Equal(t, 0.0, summary.PreviousBalance)
		assert.Zero(t, summary.Delta)
		assert.True(t, summary.HasHistory)
	})

	t.Run("annotates status and type", func(t *testing.T) {
		closed := account("a", "Old Card", "Bank", "CREDITCARD", -40)
		closed.Status = "INACTIVE"
		loan := account("b", "Mortgage", "Bank", "LOAN", -1000)

		groups := BuildAccountGroups([]models.Account{closed, loan}, nil)

		byName := map[string]AccountSummary{}
		for _, summary := range groups[0].Accounts {
			byName[summary.Account.Name] = summary
		}

		assert.False(t, byName["Old Card"].IsActive)
		assert.Equal(t, "Credit Card", byName["Old Card"].TypeLabel)
		assert.False(t, byName["Old Card"].IsLoan)
		assert.True(t, byName["Mortgage"].IsActive)
		assert.True(t, byName["Mortgage"].IsLoan)
	})

	t.Run("accounts without a connection fall into Other", func(t *testing.T) {
		groups := BuildAccountGroups([]models.Account{account("a", "Cash", "", "WALLET", 20)}, nil)

		assert.Len(t, groups, 1)
		assert.Equal(t, "Other", groups[0].Connection)
	})

	t.Run("no accounts yields no groups", func(t *testing.T) {
		assert.Empty(t, BuildAccountGroups(nil, nil))
	})
}

func TestOldestRefresh(t *testing.T) {
	withRefresh := func(id string, refreshed time.Time) models.Account {
		a := account(id, id, "Bank", "CHECKING", 0)
		a.Refreshed.Balance = refreshed
		return a
	}

	older := time.Date(2026, time.August, 1, 9, 0, 0, 0, time.UTC)
	newer := time.Date(2026, time.August, 18, 9, 0, 0, 0, time.UTC)

	t.Run("returns the least recently refreshed balance", func(t *testing.T) {
		refreshed, ok := OldestRefresh([]models.Account{
			withRefresh("a", newer),
			withRefresh("b", older),
		})

		assert.True(t, ok)
		assert.Equal(t, older, refreshed)
	})

	t.Run("ignores accounts that have never been refreshed", func(t *testing.T) {
		refreshed, ok := OldestRefresh([]models.Account{
			withRefresh("a", time.Time{}),
			withRefresh("b", newer),
		})

		assert.True(t, ok)
		assert.Equal(t, newer, refreshed)
	})

	t.Run("reports when nothing has been refreshed", func(t *testing.T) {
		refreshed, ok := OldestRefresh([]models.Account{withRefresh("a", time.Time{})})

		assert.False(t, ok)
		assert.True(t, refreshed.IsZero())
	})
}

func TestWalkAccount(t *testing.T) {
	tests := []struct {
		name     string
		balance  float64
		deltas   map[string]float64
		expected map[string]float64
	}{

		{"simple",
			99,
			map[string]float64{
				"2023-03": 33,
				"2023-02": 33,
				"2023-01": 33,
				"2022-12": 0,
			},
			map[string]float64{
				"2023-03": 99,
				"2023-02": 66,
				"2023-01": 33,
				"2022-12": 0,
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			balances := WalkAccount(test.balance, test.deltas)
			assert.Equal(t, test.expected, balances)
		})
	}
}

// TestBuildAccountBalanceHistory covers the two defects the account page showed:
// a history anchored to the wrong balance field, and a highest balance that no
// negative account could ever beat.
func TestBuildAccountBalanceHistory(t *testing.T) {

	viewDate := time.Date(2026, time.June, 1, 0, 0, 0, 0, time.UTC)

	month := func(m time.Month, amount float64) models.Transaction {
		return models.Transaction{
			Account: "acct",
			Amount:  amount,
			Date:    time.Date(2026, m, 15, 0, 0, 0, 0, time.UTC),
		}
	}

	t.Run("anchors on the current balance, not available credit", func(t *testing.T) {
		// A KiwiSaver holds a real balance while the feed leaves Available at
		// zero. Anchoring on Available charted a balance the account never had.
		var kiwisaver models.Account
		kiwisaver.Id = "acct"
		kiwisaver.Type = "KIWISAVER"
		kiwisaver.Balance.Current = 39105.70
		kiwisaver.Balance.Available = 0

		graph, stats := BuildAccountBalanceHistory(kiwisaver, []models.Transaction{
			month(time.April, 500),
			month(time.May, 500),
		}, viewDate)

		require.Len(t, graph.Data, 2)
		assert.InDelta(t, 39105.70, graph.Data[len(graph.Data)-1], 0.001)
		assert.InDelta(t, 38605.70, graph.Data[0], 0.001)
		assert.InDelta(t, 39105.70, stats.HighestBalance, 0.001)
	})

	t.Run("reports a real high for an account that is never in credit", func(t *testing.T) {
		var loan models.Account
		loan.Id = "acct"
		loan.Type = "LOAN"
		loan.Balance.Current = -190000

		_, stats := BuildAccountBalanceHistory(loan, []models.Transaction{
			month(time.April, -1000),
			month(time.May, -1000),
		}, viewDate)

		// The high is the least negative balance, never a zero the account
		// never held.
		assert.InDelta(t, -189000, stats.HighestBalance, 0.001)
		assert.InDelta(t, -190000, stats.LowestBalance, 0.001)
	})

	t.Run("reports zero for a year with no transactions", func(t *testing.T) {
		var acct models.Account
		acct.Id = "acct"
		acct.Balance.Current = 500

		graph, stats := BuildAccountBalanceHistory(acct, nil, viewDate)

		assert.Empty(t, graph.Data)
		assert.Equal(t, 0.0, stats.HighestBalance)
		assert.Equal(t, 0.0, stats.LowestBalance)
	})
}

func TestBuildTopMerchantsRanking(t *testing.T) {
	spend := func(merchant string, amount float64) models.Transaction {
		var tx models.Transaction
		tx.Merchant.Name = merchant
		tx.Amount = amount
		return tx
	}

	current := []models.Transaction{
		spend("PAK'nSAVE", -120),
		spend("PAK'nSAVE", -60),
		spend("Z Energy", -95),
		spend("Mercury", -186),
		// A refund is not a payment.
		spend("Briscoes", 40),
		// An unnamed merchant cannot be ranked.
		spend("", -500),
	}
	past := []models.Transaction{spend("PAK'nSAVE", -150)}

	t.Run("ranks by spend as a positive magnitude", func(t *testing.T) {
		top := BuildTopMerchants(past, current, 10)
		require.Len(t, top, 3)
		assert.Equal(t, "Mercury", top[0].Merchant)
		assert.Equal(t, 186.0, top[0].Total)
		assert.Equal(t, "PAK'nSAVE", top[1].Merchant)
		assert.Equal(t, 180.0, top[1].Total)
	})

	t.Run("does not pad the result with blank merchants", func(t *testing.T) {
		// Asking for more than exist used to return zero valued entries, which
		// rendered as empty rows on the dashboard.
		top := BuildTopMerchants(past, current, 10)
		for _, entry := range top {
			assert.NotEmpty(t, entry.Merchant)
		}
	})

	t.Run("compares against the previous window", func(t *testing.T) {
		top := BuildTopMerchants(past, current, 10)
		var paknsave models.MerchantTotal
		for _, entry := range top {
			if entry.Merchant == "PAK'nSAVE" {
				paknsave = entry
			}
		}
		assert.Equal(t, 150.0, paknsave.PreviousTotal)
		assert.InDelta(t, 0.2, paknsave.Delta, 0.0001)
	})

	t.Run("orders ties by name so renders are stable", func(t *testing.T) {
		tied := []models.Transaction{spend("Zeta", -50), spend("Alpha", -50)}
		top := BuildTopMerchants(nil, tied, 10)
		require.Len(t, top, 2)
		assert.Equal(t, "Alpha", top[0].Merchant)
	})
}

func TestAggregateMonthlyTransactionsLocation(t *testing.T) {
	nz := time.FixedZone("NZST", 12*60*60)

	// 31 Aug 23:00 UTC is 1 Sep 11:00 in +12, so it belongs to September there.
	transactions := []models.Transaction{
		{Date: time.Date(2026, time.August, 31, 23, 0, 0, 0, time.UTC), Amount: -50},
	}

	inUTC := AggregateMonthlyTransactions(transactions, time.UTC)
	assert.Len(t, inUTC["Aug 26"], 1)

	inNZ := AggregateMonthlyTransactions(transactions, nz)
	assert.Len(t, inNZ["Sep 26"], 1)
	assert.Empty(t, inNZ["Aug 26"])
}
