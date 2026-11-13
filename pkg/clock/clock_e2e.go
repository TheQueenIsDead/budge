//go:build e2e

package clock

import "time"

// Frozen is the day the browser tests' budge, and the data seeded into it,
// believe it to be: a Monday morning. It is in UTC, the zone the test
// containers run in, so no zone data has to be shipped to read it.
var Frozen = time.Date(2026, time.June, 15, 9, 0, 0, 0, time.UTC)

// Now is Frozen, whenever it is asked.
func Now() time.Time { return Frozen }
