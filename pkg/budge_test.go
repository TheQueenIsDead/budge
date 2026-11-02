package pkg

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseRetention(t *testing.T) {
	for value, want := range map[string]time.Duration{
		"7d":   7 * 24 * time.Hour,
		"36h":  36 * time.Hour,
		"90m":  90 * time.Minute,
		"0":    0,
		"0d":   0,
		" 2d ": 48 * time.Hour,
	} {
		got, err := parseRetention(value)
		require.NoError(t, err, value)
		assert.Equal(t, want, got, value)
	}
	for _, value := range []string{"week", "-1h", "1.5d", ""} {
		_, err := parseRetention(value)
		assert.Error(t, err, value)
	}
}

func TestTelemetryKeptBesideTheDatabase(t *testing.T) {
	t.Setenv("BUDGE_BOLT_PATH", "/data")
	t.Setenv("BUDGE_TELEMETRY_RETENTION", "14d")
	opts, err := telemetryOptions("1.19.0")
	require.NoError(t, err)
	assert.Equal(t, "/data/budge.telemetry.db", opts.Path)
	assert.Equal(t, 14*24*time.Hour, opts.Retention)
	assert.Equal(t, "1.19.0", opts.Version)
}
