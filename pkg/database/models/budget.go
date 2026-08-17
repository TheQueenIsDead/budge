package models

import "github.com/TheQueenIsDead/budge/pkg/database/buckets"

type BudgetSubItem struct {
	ID        string
	Name      string
	Amount    float64
	Frequency string
}

type BudgetItem struct {
	ID            string
	Name          string
	Amount        float64
	Frequency     string
	Category      string
	BroadCategory string
	SubItems      []BudgetSubItem
}

func (bi BudgetItem) Key() []byte    { return []byte(bi.ID) }
func (bi BudgetItem) Bucket() []byte { return buckets.BudgetBucket }
