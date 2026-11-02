package models

import "github.com/TheQueenIsDead/budge/pkg/database/buckets"

// How a budget item's transactions are treated. The zero value is spending,
// so items saved before kinds existed keep counting exactly as they did.
const (
	BudgetKindSpending = ""
	BudgetKindSavings  = "savings"
	BudgetKindIgnore   = "ignore"
)

// BudgetItem is a target for one budge category.
//
// Targets were once set against Akahu's categories, named by Category and
// BroadCategory, and could be marked as savings or ignored by Kind. Those are
// kept, unread, rather than deleted, so nothing is lost; a target counts only
// once it names a budge category.
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

	Category      string
	BroadCategory string
	Kind          string
}

// IsSpending reports whether the item's transactions count against the budget.
func (bi BudgetItem) IsSpending() bool { return bi.Kind == BudgetKindSpending }

func (bi BudgetItem) Key() []byte    { return []byte(bi.ID) }
func (bi BudgetItem) Bucket() []byte { return buckets.BudgetBucket }
