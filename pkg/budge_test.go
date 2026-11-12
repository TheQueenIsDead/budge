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
	for _, value := range []string{"week", "-1h", "1.5d", "", "106752d"} {
		_, err := parseRetention(value)
		assert.Error(t, err, value)
	}
	most, err := parseRetention("106751d")
	require.NoError(t, err, "the most days a duration holds")
	assert.Positive(t, most)
}

func TestAnEmptyRetentionKeepsTheDefault(t *testing.T) {
	t.Setenv("BUDGE_TELEMETRY_RETENTION", " ")
	opts, err := telemetryOptions("")
	require.NoError(t, err)
	assert.Equal(t, 7*24*time.Hour, opts.Retention)
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

func TestTelemetryToggle(t *testing.T) {
	for _, c := range []struct {
		budge, sdkDisabled string
		want               bool
	}{
		{"", "", true},
		{"true", "", true},
		{"on", "", true},
		{"false", "", false},
		{"off", "", false},
		{"0", "", false},
		{"", "true", false},
		{"true", "true", false},
		{"", "false", true},
	} {
		t.Setenv("BUDGE_TELEMETRY", c.budge)
		t.Setenv("OTEL_SDK_DISABLED", c.sdkDisabled)
		got, err := telemetryEnabled()
		require.NoError(t, err)
		assert.Equal(t, c.want, got, "BUDGE_TELEMETRY=%q OTEL_SDK_DISABLED=%q", c.budge, c.sdkDisabled)
	}

	t.Setenv("BUDGE_TELEMETRY", "maybe")
	t.Setenv("OTEL_SDK_DISABLED", "")
	_, err := telemetryEnabled()
	assert.Error(t, err, "a value that is neither is a mistake worth stopping for")
}
