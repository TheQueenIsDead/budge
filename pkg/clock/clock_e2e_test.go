//go:build e2e

package clock

import (
	"testing"
	"time"
)

func TestNowStandsStill(t *testing.T) {
	first := Now()
	time.Sleep(10 * time.Millisecond)
	if !Now().Equal(first) || !first.Equal(Frozen) {
		t.Fatalf("Now() moved: %v then %v", first, Now())
	}
}
