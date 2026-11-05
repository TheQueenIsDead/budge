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
	assert.Len(t, accounts, 3)

	everyday, err := store.ReadTransactionsByAccount(ctx, EverydayAccount)
	require.NoError(t, err)
	filed := map[string]int{}
	for _, tx := range everyday {
		assert.False(t, tx.Date.After(now), "nothing is dated after now")
		filed[tx.BudgetCategory()]++
	}
	for _, category := range []string{"groceries", "fuel", "power", "internet-mobile"} {
		assert.Positive(t, filed[category], "some spending is filed under %s", category)
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
		defer store.Close()
		require.NoError(t, Seed(context.Background(), store, now))
		txs, err := store.ReadTransactions(context.Background())
		require.NoError(t, err)
		return txs
	}
	assert.Equal(t, read(), read())
}
