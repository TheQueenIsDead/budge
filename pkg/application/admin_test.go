package application

import (
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSpentSplitsATraceByWhatItWaitedOn(t *testing.T) {
	row := func(kind string, d time.Duration, attrs map[string]string) AdminTraceRow {
		return AdminTraceRow{Span: telemetry.Span{Kind: kind, Duration: d, Attrs: attrs}}
	}
	query := map[string]string{"db.system.name": "bbolt"}
	akahu := map[string]string{"http.request.method": "GET", "server.address": "api.akahu.io"}
	rows := []AdminTraceRow{
		row("internal", 10*time.Second, nil),
		row("internal", 6*time.Second, nil),
		row("client", 3*time.Second, akahu),
		row("client", 3*time.Second, akahu),
		row("client", time.Second, query),
		row("client", time.Second, query),
	}

	shares := spent(rows, 10*time.Second)
	require.Len(t, shares, 3)
	assert.Equal(t, AdminShare{Kind: "call", Label: "api.akahu.io", Detail: "2 calls", Duration: 6 * time.Second, Share: 0.6}, shares[0], "the biggest share first")
	assert.Equal(t, AdminShare{Kind: "database", Label: "Database", Detail: "2 queries", Duration: 2 * time.Second, Share: 0.2}, shares[1])
	assert.Equal(t, AdminShare{Kind: "own", Label: "Budge", Detail: "its own work", Duration: 2 * time.Second, Share: 0.2}, shares[2],
		"budge's own work is what is left, and the phases holding the rest are not counted again")
}

func TestChartSpansItsRangeAndBreaksAtGaps(t *testing.T) {
	at := time.Date(2026, time.November, 3, 9, 0, 0, 0, time.Local)
	point := func(after time.Duration, v float64) telemetry.Point {
		return telemetry.Point{Time: at.Add(after), Value: v}
	}
	points := []telemetry.Point{point(0, 1), point(time.Minute, 2), point(10*time.Minute, 3), point(11*time.Minute, 4)}
	axis := AdminAxis{
		From: at.Add(-time.Hour), To: at.Add(time.Hour),
		Expected: func(time.Time) time.Duration { return time.Minute },
	}
	starts := []time.Time{at.Add(-2 * time.Hour), at.Add(5 * time.Minute)}

	c := chart(points, axis, starts)
	assert.Equal(t, axis.From.UnixMilli(), c.From, "the axis starts where the range does, not at the first sample")
	assert.Equal(t, axis.To.UnixMilli(), c.To)
	require.Len(t, c.Points, 5, "nine minutes without a sample is a gap")
	assert.Nil(t, c.Points[2].Y, "the line breaks between the samples either side")
	assert.Equal(t, at.Add(5*time.Minute+30*time.Second).UnixMilli(), c.Points[2].X)
	assert.Equal(t, 3.0, *c.Points[3].Y)
	assert.Equal(t, []int64{at.Add(5 * time.Minute).UnixMilli()}, c.Restarts, "only restarts within the range are marked, where they happened")
}

func TestTicksFallOnTheClock(t *testing.T) {
	from := time.Date(2026, time.November, 3, 9, 17, 0, 0, time.Local)

	hour := ticks(from, from.Add(time.Hour), time.Hour)
	require.NotEmpty(t, hour)
	assert.Equal(t, time.Date(2026, time.November, 3, 9, 20, 0, 0, time.Local), hour[0], "every ten minutes, from the first after the start")
	assert.Len(t, hour, 6)

	week := ticks(from, from.Add(7*24*time.Hour), 7*24*time.Hour)
	require.Len(t, week, 7)
	for _, tick := range week {
		assert.Zero(t, tick.Hour(), "a week's ticks are at midnight")
	}
}
