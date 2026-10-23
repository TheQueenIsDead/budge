package models

import "github.com/TheQueenIsDead/budge/pkg/database/buckets"

type BudgetSubItem struct {
	ID        string
	Name      string
	Amount    float64
	Frequency string
}

// How a budget item's transactions are treated. The zero value is spending,
// so items saved before kinds existed keep counting exactly as they did.
const (
	BudgetKindSpending = ""
	BudgetKindSavings  = "savings"
	BudgetKindIgnore   = "ignore"
)

type BudgetItem struct {
	ID            string
	Name          string
	Amount        float64
	Frequency     string
	Category      string
	BroadCategory string
	SubItems      []BudgetSubItem
	Kind          string
}

// IsSpending reports whether the item's transactions count against the budget.
func (bi BudgetItem) IsSpending() bool { return bi.Kind == BudgetKindSpending }

func (bi BudgetItem) Key() []byte    { return []byte(bi.ID) }
func (bi BudgetItem) Bucket() []byte { return buckets.BudgetBucket }
