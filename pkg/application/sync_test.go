package application

import (
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSyncRunnerAllowsOneAtATime(t *testing.T) {
	var runner syncRunner

	require.True(t, runner.begin())
	assert.False(t, runner.begin(), "a second sync joins the first rather than starting")

	runner.progress(300)
	assert.True(t, runner.status().Running)
	assert.Equal(t, 300, runner.status().Fetched)

	runner.end(errors.New("akahu /transactions returned 429: Rate limit exceeded"))
	status := runner.status()
	assert.False(t, status.Running)
	assert.Contains(t, status.Error, "429")

	require.True(t, runner.begin(), "a finished sync frees the runner")
	assert.Empty(t, runner.status().Error, "a new run starts clean")
	assert.Zero(t, runner.status().Fetched)
}

func TestRenderSyncProgress(t *testing.T) {
	started := time.Date(2026, time.October, 15, 23, 0, 0, 0, time.UTC)

	running := renderTemplate(t, "settings.integrations.sync", SyncStatus{Running: true, Fetched: 2300, Started: started})
	assert.Contains(t, running, `hx-trigger="every 1s"`)
	assert.Contains(t, running, "started="+SyncStatus{Started: started}.StartedKey())
	assert.Contains(t, running, "Fetched 2300 transactions")
	assert.Contains(t, running, "disabled")

	// The finished view carries no trigger, which is what stops the polling.
	done := renderTemplate(t, "settings.integrations.sync", SyncStatus{Started: started, LastSync: time.Now().Add(-time.Minute)})
	assert.NotContains(t, done, "hx-trigger")
	assert.Contains(t, done, "Sync now")
	assert.Contains(t, done, "Last sync")
	assert.Contains(t, done, `hx-post="/integrations/akahu/resync"`)

	failed := renderTemplate(t, "settings.integrations.sync", SyncStatus{Error: "boom"})
	assert.Contains(t, failed, "Last sync failed")
	assert.Contains(t, failed, "Never synced")
	assert.NotContains(t, failed, "resync", "a full resync is the first sync until one has run")
}
