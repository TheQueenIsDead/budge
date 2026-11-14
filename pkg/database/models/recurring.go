package models

import "github.com/TheQueenIsDead/budge/pkg/database/buckets"

// What the owner can say about a payment budge found recurring. Recurring
// payments are found afresh from the transactions each time, so the owner's
// word is all there is to keep about them.
const (
	// RecurringNotRecurring hides a payment that only looks regular, such as
	// a cafe on the way to work: it is left out of the totals and raises
	// nothing.
	RecurringNotRecurring = "not-recurring"

	// RecurringCancelled stops expecting a payment the owner has cancelled,
	// without waiting for it to go quiet: it moves to the stopped list and
	// raises nothing.
	RecurringCancelled = "cancelled"
)

// ValidRecurringMark reports whether mark is one the owner can make.
func ValidRecurringMark(mark string) bool {
	return mark == RecurringNotRecurring || mark == RecurringCancelled
}

// RecurringMarks is what the owner has said about the recurring payments,
// by merchant key. A payment with no mark is taken as found.
type RecurringMarks struct {
	Marks map[string]string `json:"marks"`
}

func (m RecurringMarks) Key() []byte    { return []byte("recurring-marks") }
func (m RecurringMarks) Bucket() []byte { return buckets.SettingsBucket }
