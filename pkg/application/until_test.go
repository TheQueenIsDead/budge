package application

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

func TestFmtUntil(t *testing.T) {
	f := templateFuncs()["fmtUntil"].(func(time.Time) string)
	tests := []struct {
		name string
		at   time.Time
		want string
	}{
		// Padded a little: humanize truncates, so a duration constructed and read
		// microseconds apart would land just under the boundary.
		{"days ahead", time.Now().Add(6*24*time.Hour + time.Minute), "in 6 days"},
		{"an hour ahead", time.Now().Add(time.Hour + time.Minute), "in 1 hour"},
		{"already past reads as imminent", time.Now().Add(-time.Hour), "shortly"},
		{"now reads as imminent", time.Now(), "shortly"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := f(test.at)
			assert.Equal(t, test.want, got)
			// The bug this replaces rendered "6 days ." mid-sentence.
			assert.NotContains(t, got, "  ")
			assert.False(t, got[len(got)-1] == ' ')
		})
	}
}
