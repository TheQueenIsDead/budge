package akahu

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetTransactionsStopsOnARepeatedCursor(t *testing.T) {
	var requests int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests++
		// The second page points back at itself.
		_, _ = fmt.Fprint(w, `{"success": true, "items": [{"_id": "tx"}], "cursor": {"next": "page-2"}}`)
	}))
	defer server.Close()

	_, err := NewClient(WithBaseURL(server.URL)).GetTransactions(t.Context(), time.Time{}, true, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "repeated")
	assert.Equal(t, 2, requests, "the first page and the one that repeats")
}

func TestANullResponseIsAnError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = fmt.Fprint(w, `null`)
	}))
	defer server.Close()
	client := NewClient(WithBaseURL(server.URL))

	_, err := client.GetTransactions(t.Context(), time.Time{}, true, nil)
	assert.ErrorContains(t, err, "empty response")
	_, err = client.GetAccounts(t.Context())
	assert.ErrorContains(t, err, "empty response")
	_, err = client.Me(t.Context())
	assert.ErrorContains(t, err, "empty response")
}

func TestGetTransactionsFollowsEveryPage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		next := map[string]string{"": "page-2", "page-2": "page-3", "page-3": ""}[r.URL.Query().Get("cursor")]
		_, _ = fmt.Fprintf(w, `{"success": true, "items": [{"_id": "tx"}], "cursor": {"next": %q}}`, next)
	}))
	defer server.Close()

	items, err := NewClient(WithBaseURL(server.URL)).GetTransactions(t.Context(), time.Time{}, true, nil)
	require.NoError(t, err)
	assert.Len(t, items, 3)
}
