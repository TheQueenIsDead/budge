package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
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
