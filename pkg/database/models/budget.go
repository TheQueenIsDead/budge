package models

import "github.com/TheQueenIsDead/budge/pkg/database/buckets"

// BudgetItem is a target for one budge category. A target saved before
// budge had its own categories names none, and is not read.
type BudgetItem struct {
	ID        string
	Name      string
	Amount    float64
	Frequency string

	// BudgetCategory is the key of the budge category or subcategory the
	// target is for.
	BudgetCategory string
	// Split marks a category budgeted by its subcategories instead: its own
	// amount is ignored, and its target is theirs added up.
	Split bool
	// GeneralAmount and GeneralFrequency are the target, while split, for
	// what is filed under the category itself rather than a subcategory. They
	// sit beside its own amount, so switching back and forth loses neither.
	GeneralAmount    float64
	GeneralFrequency string
}

func (bi BudgetItem) Key() []byte    { return []byte(bi.ID) }
func (bi BudgetItem) Bucket() []byte { return buckets.BudgetBucket }
