//go:build !e2e

package clock

import (
	"testing"
	"time"
)

func TestNowIsThePresent(t *testing.T) {
	before := time.Now()
	now := Now()
	if now.Before(before) || now.After(time.Now()) {
		t.Fatalf("Now() = %v, not between the times either side of it", now)
	}
}
