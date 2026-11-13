package test

import (
	"context"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSeedFillsAHousehold(t *testing.T) {
	t.Setenv("BUDGE_BOLT_PATH", t.TempDir())
	store, err := database.NewStore()
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	ctx := context.Background()
	now := time.Date(2026, 11, 6, 9, 30, 0, 0, time.UTC)

	require.NoError(t, Seed(ctx, store, now))

	accounts, err := store.ReadAccounts(ctx)
	require.NoError(t, err)
	assert.Len(t, accounts, 4)

	everyday, err := store.ReadTransactionsByAccount(ctx, EverydayAccount)
	require.NoError(t, err)
	filed := map[string]int{}
	unfiled := 0
	for _, tx := range everyday {
		assert.False(t, tx.Date.After(now), "nothing is dated after now")
		filed[tx.BudgetCategory()]++
		if tx.Type == "PAYMENT" && tx.Category.Name == "" {
			unfiled++
		}
	}
	for _, category := range []string{"groceries", "fuel", "power", "internet-mobile"} {
		assert.Positive(t, filed[category], "some spending is filed under %s", category)
	}
	assert.Equal(t, 2, unfiled, "and a little spending is filed nowhere")

	// The home loan's balance is where its history leads: what it started
	// owing, less every repayment, plus every charge of interest. Each
	// whole month it owes less than the month before; the first and the
	// last are cut short, and may catch the interest without a repayment.
	loan, err := store.GetAccount(ctx, []byte(HomeLoan))
	require.NoError(t, err)
	loanTxs, err := store.ReadTransactionsByAccount(ctx, HomeLoan)
	require.NoError(t, err)
	owed := -loanOwed
	monthly := map[string]float64{}
	for _, tx := range loanTxs {
		owed += tx.Amount
		monthly[tx.Date.Format("2006-01")] += tx.Amount
	}
	assert.InDelta(t, owed, loan.Balance.Current, 0.01)
	assert.Less(t, loan.Balance.Current, 0.0, "a loan owes, so its balance is negative")
	assert.GreaterOrEqual(t, len(monthly), 12, "a year or more of history")
	for month, net := range monthly {
		if month != now.Format("2006-01") && month != now.AddDate(0, -18, 0).Format("2006-01") {
			assert.Positive(t, net, "%s pays down more than it charges", month)
		}
	}

	house, err := store.GetAsset(ctx, House)
	require.NoError(t, err)
	assert.Equal(t, 650_000.0, house.PurchasePrice)
}

func TestSeedIsTheSameGivenTheSameNow(t *testing.T) {
	now := time.Date(2026, 11, 6, 0, 0, 0, 0, time.UTC)
	read := func() []models.Transaction {
		t.Setenv("BUDGE_BOLT_PATH", t.TempDir())
		store, err := database.NewStore()
		require.NoError(t, err)
		defer func() { _ = store.Close() }()
		require.NoError(t, Seed(context.Background(), store, now))
		txs, err := store.ReadTransactions(context.Background())
		require.NoError(t, err)
		return txs
	}
	assert.Equal(t, read(), read())
}
