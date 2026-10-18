package integrations

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/integrations/akahu"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeAkahu serves a full history of transactions across two pages, and
// records the start of the window each sync asked for.
func fakeAkahu(t *testing.T, transactions int, starts *[]string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/accounts":
			// Akahu last fetched from the banks at different times; the
			// closed account's stale refresh must not hold the window back.
			_, _ = fmt.Fprint(w, `{"success": true, "items": [
				{"_id": "acc", "name": "Everyday", "status": "ACTIVE", "refreshed": {"transactions": "2026-10-14T06:00:00Z"}},
				{"_id": "card", "name": "Credit card", "status": "ACTIVE", "refreshed": {"transactions": "2026-10-13T22:00:00Z"}},
				{"_id": "old", "name": "Closed", "status": "INACTIVE", "refreshed": {"transactions": "2025-01-01T00:00:00Z"}}
			]}`)
		case "/transactions":
			*starts = append(*starts, r.URL.Query().Get("start"))
			from, to, next := 0, transactions/2, "page-2"
			if r.URL.Query().Get("cursor") == "page-2" {
				from, to, next = transactions/2, transactions, ""
			}
			var items []map[string]interface{}
			for i := from; i < to; i++ {
				items = append(items, map[string]interface{}{
					"_id": fmt.Sprintf("trans_%d", i), "_account": "acc", "amount": -12.5,
					"type": "EFTPOS", "description": "PAKNSAVE",
					"date":     time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC).AddDate(0, 0, -i%365),
					"merchant": map[string]string{"_id": fmt.Sprintf("merchant_%d", i%40), "name": "PAK'nSAVE"},
				})
			}
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"success": true, "items": items, "cursor": map[string]string{"next": next}})
		default:
			http.NotFound(w, r)
		}
	}))
}

func testIntegrations(t *testing.T, baseURL string) (*Integrations, *database.Store) {
	t.Helper()
	t.Setenv("BUDGE_BOLT_PATH", t.TempDir())
	store, err := database.NewStore()
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })

	return &Integrations{store: store, akahu: akahu.NewClient(akahu.WithBaseURL(baseURL))}, store
}

func TestSyncAkahuFullHistory(t *testing.T) {
	var starts []string
	server := fakeAkahu(t, 4000, &starts)
	defer server.Close()
	integrations, store := testIntegrations(t, server.URL)

	began := time.Now()
	through, err := integrations.SyncAkahu(echo.New().Logger, time.Time{}, nil)
	require.NoError(t, err)
	took := time.Since(began)
	assert.Equal(t, time.Date(2026, time.October, 13, 22, 0, 0, 0, time.UTC), through.UTC(),
		"the account furthest behind sets how fresh the data is")

	count, err := store.CountTransactions()
	require.NoError(t, err)
	assert.Equal(t, 4000, count)
	accounts, err := store.ReadAccounts()
	require.NoError(t, err)
	assert.Len(t, accounts, 3)

	// Written one commit at a time this took close to two minutes, which is
	// what timed the request out. Behind one commit it is a moment.
	assert.Less(t, took, 15*time.Second)

	// A first sync asks for everything.
	assert.Equal(t, []string{"", ""}, starts)
}

func TestSyncAkahuReadsFromAkahusFreshness(t *testing.T) {
	var starts []string
	server := fakeAkahu(t, 10, &starts)
	defer server.Close()
	integrations, _ := testIntegrations(t, server.URL)

	through := time.Date(2026, time.October, 15, 9, 0, 0, 0, time.UTC)
	_, err := integrations.SyncAkahu(echo.New().Logger, through, nil)
	require.NoError(t, err)

	require.NotEmpty(t, starts)
	assert.Equal(t, "2026-10-01T09:00:00Z", starts[0], "a fortnight before Akahu's last refresh")
}

func TestFreshness(t *testing.T) {
	account := func(status string, refreshed time.Time) akahu.Account {
		var a akahu.Account
		a.Status = status
		a.Refreshed.Transactions = refreshed
		return a
	}
	tx := func(date time.Time) akahu.Transaction {
		var t akahu.Transaction
		t.Date = date
		return t
	}
	monday := time.Date(2026, time.October, 12, 0, 0, 0, 0, time.UTC)
	previous := monday.AddDate(0, 0, -3)

	t.Run("Akahu's oldest refresh, ignoring closed accounts", func(t *testing.T) {
		got := freshness([]akahu.Account{
			account("ACTIVE", monday.Add(5*time.Hour)),
			account("ACTIVE", monday.Add(2*time.Hour)),
			account("INACTIVE", monday.AddDate(-1, 0, 0)),
		}, nil, previous)
		assert.Equal(t, monday.Add(2*time.Hour), got)
	})

	t.Run("the newest transaction when Akahu reports no refresh", func(t *testing.T) {
		got := freshness([]akahu.Account{account("ACTIVE", time.Time{})}, []akahu.Transaction{tx(monday), tx(previous)}, previous.AddDate(0, 0, -10))
		assert.Equal(t, monday, got)
	})

	t.Run("never moves on when there is nothing new to go on", func(t *testing.T) {
		// The bug this replaces: an hourly sync that found nothing still
		// moved the start of the next window up to the time it ran, so a
		// purchase posted late, dated before that, was never read.
		got := freshness(nil, nil, previous)
		assert.Equal(t, previous, got)
	})
}

func TestSyncAkahuReportsAkahuErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = fmt.Fprint(w, `{"success": false, "message": "Rate limit exceeded"}`)
	}))
	defer server.Close()
	integrations, _ := testIntegrations(t, server.URL)

	_, err := integrations.SyncAkahu(echo.New().Logger, time.Time{}, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "429")
	assert.Contains(t, err.Error(), "Rate limit exceeded")
}

func TestIdentifyTransfers(t *testing.T) {
	at := func(id, account string, amount float64, hours int) akahu.Transaction {
		var tx akahu.Transaction
		tx.Id, tx.Account, tx.Amount, tx.Type = id, account, amount, "PAYMENT"
		tx.Date = time.Date(2026, time.June, 22, 9, 0, 0, 0, time.UTC).Add(time.Duration(hours) * time.Hour)
		return tx
	}

	// Savings tops up the everyday account, which then pays the roofer.
	transfers := identifyTransfers([]akahu.Transaction{
		at("from-savings", "savings", -1898.20, 0),
		at("into-everyday", "everyday", 1898.20, 0),
		at("roofer", "everyday", -1898.20, 2),
	})

	assert.True(t, transfers["from-savings"])
	assert.True(t, transfers["into-everyday"])
	assert.False(t, transfers["roofer"], "paying the roofer is spending, not a transfer")

	t.Run("an equal and opposite pair in one account is not a transfer", func(t *testing.T) {
		transfers := identifyTransfers([]akahu.Transaction{
			at("refund", "everyday", 120, 0),
			at("purchase", "everyday", -120, 3),
		})
		assert.Empty(t, transfers)
	})

	t.Run("days apart is not a transfer", func(t *testing.T) {
		transfers := identifyTransfers([]akahu.Transaction{
			at("out", "savings", -500, 0),
			at("in", "everyday", 500, 72),
		})
		assert.Empty(t, transfers)
	})
}

func TestSyncAkahuSkipsUntrackedAccounts(t *testing.T) {
	var starts []string
	server := fakeAkahu(t, 10, &starts)
	defer server.Close()
	integrations, store := testIntegrations(t, server.URL)

	_, err := integrations.SyncAkahu(echo.New().Logger, time.Time{}, nil)
	require.NoError(t, err)
	require.NoError(t, store.RemoveAccount("acc"))

	through, err := integrations.SyncAkahu(echo.New().Logger, time.Time{}, nil)
	require.NoError(t, err)

	accounts, err := store.ReadAccounts()
	require.NoError(t, err)
	for _, account := range accounts {
		assert.NotEqual(t, "acc", account.Id, "a sync does not bring back an account that was switched off")
	}
	count, err := store.CountTransactions()
	require.NoError(t, err)
	assert.Zero(t, count, "nor its transactions")
	assert.Equal(t, time.Date(2026, time.October, 13, 22, 0, 0, 0, time.UTC), through.UTC(),
		"and it no longer has a say in how fresh the data is")
}
