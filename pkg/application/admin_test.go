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

func TestChartMarksEachRestart(t *testing.T) {
	at := time.Date(2026, time.November, 3, 9, 0, 0, 0, time.Local)
	point := func(after time.Duration, v float64) telemetry.Point { return telemetry.Point{Time: at.Add(after), Value: v} }
	points := []telemetry.Point{point(0, 1), point(time.Minute, 2), point(10*time.Minute, 3), point(11*time.Minute, 4)}
	starts := []time.Time{at.Add(-time.Hour), at.Add(5 * time.Minute)}

	c := chart(points, time.Hour, starts)
	assert.Equal(t, []int{2}, c.Restarts, "the restart falls before the first point after the gap; one before the chart is not drawn")
	assert.Equal(t, "09:00", c.Labels[0])
	assert.Equal(t, "Tue 09:00", chart(points, 7*24*time.Hour, nil).Labels[0], "a week names the day")
}
