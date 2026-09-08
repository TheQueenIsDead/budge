package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestScheduleDue(t *testing.T) {
	now := time.Date(2026, time.September, 9, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name    string
		lastRun time.Time
		every   time.Duration
		due     bool
	}{
		{
			// Switching a job on should do something visible rather than wait a
			// full cycle first.
			name: "never run is due immediately", lastRun: time.Time{}, every: time.Hour, due: true,
		},
		{"a full cycle has passed", now.Add(-time.Hour), time.Hour, true},
		{"more than a cycle has passed", now.Add(-5 * time.Hour), time.Hour, true},
		{"not yet", now.Add(-30 * time.Minute), time.Hour, false},
		{"just short", now.Add(-59 * time.Minute), time.Hour, false},
		{"a zero cadence never fires", now.Add(-100 * time.Hour), 0, false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			assert.Equal(t, test.due, models.Due(test.lastRun, test.every, now))
		})
	}
}

func TestScheduleIntervals(t *testing.T) {
	t.Run("a stored key resolves to its cadence", func(t *testing.T) {
		schedule := models.ScheduleSettings{AkahuInterval: "6h", AssetsInterval: "168h"}
		assert.Equal(t, 6*time.Hour, schedule.AkahuEvery().Every)
		assert.Equal(t, 7*24*time.Hour, schedule.AssetsEvery().Every)
	})

	t.Run("an unknown key falls back rather than disabling the job", func(t *testing.T) {
		// A hand-edited value must not silently stop a sync by resolving to zero.
		schedule := models.ScheduleSettings{AkahuInterval: "banana", AssetsInterval: ""}
		assert.Greater(t, schedule.AkahuEvery().Every, time.Duration(0))
		assert.Greater(t, schedule.AssetsEvery().Every, time.Duration(0))
	})
}

func TestDescribeMove(t *testing.T) {
	sources := []string{"OneRoof $575K", "homes.co.nz $550K"}

	t.Run("a rise reads as a gain", func(t *testing.T) {
		got := describeMove(550000, 562500, sources)
		// The half thousand survives: both sources round to ten thousand, so an
		// average of two lands here routinely.
		assert.Contains(t, got, "up $12.5K")
		assert.Contains(t, got, "from $550K")
		assert.Contains(t, got, "to $562.5K")
		assert.Contains(t, got, "OneRoof $575K · homes.co.nz $550K")
	})

	t.Run("a fall reads as a fall", func(t *testing.T) {
		got := describeMove(600000, 550000, sources)
		assert.Contains(t, got, "down $50K")
	})

	t.Run("millions read as millions", func(t *testing.T) {
		assert.Contains(t, describeMove(1000000, 1250000, sources), "$1.25M")
	})

	t.Run("a round number carries no empty fraction", func(t *testing.T) {
		got := describeMove(500000, 550000, sources)
		assert.Contains(t, got, "to $550K")
		assert.NotContains(t, got, "$550.0K")
		assert.NotContains(t, got, "$1.00M")
	})
}

func TestBuildScheduleView(t *testing.T) {
	t.Run("a job that has never run has no next time", func(t *testing.T) {
		// The page says "runs within the minute" rather than showing a date.
		view := buildScheduleView(models.ScheduleSettings{AkahuEnabled: true})
		assert.True(t, view.AkahuNext.IsZero())
		assert.True(t, view.AssetsNext.IsZero())
	})

	t.Run("next is a cadence on from the last run", func(t *testing.T) {
		last := time.Now().Add(-30 * time.Minute)
		view := buildScheduleView(models.ScheduleSettings{
			AkahuEnabled: true, AkahuInterval: "1h", AkahuLastRun: last,
		})
		assert.WithinDuration(t, last.Add(time.Hour), view.AkahuNext, time.Second)
	})

	t.Run("both interval sets are offered", func(t *testing.T) {
		view := buildScheduleView(models.ScheduleSettings{})
		assert.NotEmpty(t, view.AkahuIntervals)
		assert.NotEmpty(t, view.AssetIntervals)
	})
}

func TestRenderScheduleSettings(t *testing.T) {
	t.Run("off by default, and says so", func(t *testing.T) {
		page := renderTemplate(t, "settings.schedule", buildScheduleView(models.ScheduleSettings{}))
		// Nothing should start reaching out to a bank without being asked.
		assert.NotContains(t, page, "checked")
		assert.Contains(t, page, "Off.")
	})

	t.Run("a running job shows its cadence and what is next", func(t *testing.T) {
		page := renderTemplate(t, "settings.schedule", buildScheduleView(models.ScheduleSettings{
			AkahuEnabled: true, AkahuInterval: "6h", AkahuLastRun: time.Now().Add(-time.Hour),
			AssetsEnabled: true, AssetsInterval: "168h",
		}))

		assert.Contains(t, page, "checked")
		assert.Contains(t, page, "Every 6 hours")
		assert.Contains(t, page, "Once a week")
		assert.Contains(t, page, "Last ran")
		// The assets job has never run, so it is due at the next tick.
		assert.Contains(t, page, "Runs within the minute.")
	})

	t.Run("it posts back over itself", func(t *testing.T) {
		page := renderTemplate(t, "settings.schedule", buildScheduleView(models.ScheduleSettings{}))
		assert.Contains(t, page, `hx-post="/settings/schedule"`)
		assert.Contains(t, page, `hx-target="#schedule"`)
	})
}

func TestSchedulerStartStop(t *testing.T) {
	// A scheduler that never started must still be safe to stop, since Close
	// runs whether or not Start did.
	s := &scheduler{}
	require.NotPanics(t, func() { s.stop() })
}
