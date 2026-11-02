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
