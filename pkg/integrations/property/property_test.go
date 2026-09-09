package property

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAverage(t *testing.T) {
	tests := []struct {
		name      string
		estimates []Estimate
		expected  float64
	}{
		{"no sources answered", nil, 0},
		{"one source", []Estimate{{"homes", 900000}}, 900000},
		// Averaging is still the shape, even with one source configured today.
		{"two sources are averaged", []Estimate{{"homes", 900000}, {"other", 940000}}, 920000},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.expected, Average(test.estimates))
		})
	}
}

func TestSuggestAddresses(t *testing.T) {
	t.Run("keeps only whole properties", func(t *testing.T) {
		// Type 4 is a property; the others are streets and suburbs, which
		// cannot be valued and must not be offered.
		body := `{"Results":[
			{"Title":"12 Bealey Avenue, Merivale, Christchurch","Suburb":"Merivale","City":"Christchurch","Lat":-43.52,"Long":172.62,"Type":4},
			{"Title":"Bealey Avenue, Christchurch Central","Suburb":"Christchurch Central","City":"Christchurch","Type":3}
		],"ErrorMessage":""}`

		client, teardown := stubClient(t, http.StatusOK, body)
		defer teardown()

		suggestions, err := client.SuggestAddresses(context.Background(), "bealey", 5)
		require.NoError(t, err)
		require.Len(t, suggestions, 1)
		assert.Equal(t, "12 Bealey Avenue, Merivale, Christchurch", suggestions[0].Address)
		assert.Equal(t, "Merivale", suggestions[0].Suburb)
	})

	t.Run("an empty query does not call out", func(t *testing.T) {
		client := New()
		suggestions, err := client.SuggestAddresses(context.Background(), "", 5)
		assert.NoError(t, err)
		assert.Empty(t, suggestions)
	})

	t.Run("surfaces an upstream error rather than pretending there are no matches", func(t *testing.T) {
		client, teardown := stubClient(t, http.StatusTooManyRequests, "")
		defer teardown()

		_, err := client.SuggestAddresses(context.Background(), "bealey", 5)
		assert.Error(t, err)
	})

	t.Run("reports an error message in the payload", func(t *testing.T) {
		client, teardown := stubClient(t, http.StatusOK, `{"Results":[],"ErrorMessage":"nope"}`)
		defer teardown()

		_, err := client.SuggestAddresses(context.Background(), "bealey", 5)
		assert.ErrorContains(t, err, "nope")
	})
}

// stubClient points the client at a local server standing in for the upstream.
func stubClient(t *testing.T, status int, body string) (*Client, func()) {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}))

	client := New()
	// Redirect every request to the stub, whatever URL the code builds.
	client.http.Transport = rewriteTo(server.URL)
	return client, server.Close
}

type rewriteTransport struct{ base string }

func (t rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	target, err := http.NewRequest(r.Method, t.base+r.URL.Path+"?"+r.URL.RawQuery, r.Body)
	if err != nil {
		return nil, err
	}
	target.Header = r.Header
	return http.DefaultTransport.RoundTrip(target)
}

func rewriteTo(base string) http.RoundTripper { return rewriteTransport{base: base} }

func TestParseShortValue(t *testing.T) {
	tests := []struct {
		raw      string
		expected float64
		ok       bool
	}{
		{"550K", 550000, true},
		{"$575K", 575000, true},
		{"1.25M", 1250000, true},
		{"$1.25M", 1250000, true},
		{"550,000", 550000, true},
		{"  $520K  ", 520000, true},
		// A bare small number is not a house price; it means the pattern caught
		// something that was not money.
		{"12", 0, false},
		{"", 0, false},
		{"High Accuracy", 0, false},
	}

	for _, test := range tests {
		t.Run(test.raw, func(t *testing.T) {
			value, ok := parseShortValue(test.raw)
			assert.Equal(t, test.ok, ok)
			assert.Equal(t, test.expected, value)
		})
	}
}

func TestEstimateAveraging(t *testing.T) {
	t.Run("an unidentified property is not attempted", func(t *testing.T) {
		// Nothing to ask, so nothing is tried and nothing counts as failed.
		result := New().Estimate(context.Background(), "")
		assert.Empty(t, result.Estimates)
		assert.Empty(t, result.Failed)
		assert.Equal(t, 0.0, result.Average)
	})
}
