//go:build !e2e

// Package clock says what day it is, for everything budge works out from
// today: windows, periods, the week in progress and how long ago something
// was. The browser tests build budge with the e2e tag, which stops the clock
// on a fixed day, so the same pages draw the same way on any day they run.
//
// Only "what day is it" belongs here. Anything that measures time passing,
// such as how long a sync took, when the scheduler is next due, or when a
// record was written, reads time.Now itself, or a stopped clock would stop
// it too.
package clock

import "time"

// Now is the present.
func Now() time.Time { return time.Now() }
