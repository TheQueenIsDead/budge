package models

import (
	"encoding/json"
	"sort"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
)

// AssetValuation is what an asset was judged to be worth on a given date.
// Valuations are entered by hand: there is no automated feed for what a house
// is worth, and a rateable value or an agent's appraisal is a real data point
// worth recording as one.
type AssetValuation struct {
	ID    string    `json:"id"`
	Date  time.Time `json:"date"`
	Value float64   `json:"value"`
	Note  string    `json:"note"`
}

// Asset is something owned whose value is tracked by hand, rather than pulled
// from a bank feed. A house is the motivating case, but the shape suits a car
// or anything else that is worth something and drifts over time.
type Asset struct {
	Id          string    `json:"_id"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"created_at"`

	// Purchase is the first point on the curve. It is kept separate from the
	// valuations so that "what I paid" survives even after a revaluation, which
	// is what any gain has to be measured against.
	PurchasePrice float64   `json:"purchase_price"`
	PurchaseDate  time.Time `json:"purchase_date"`

	Valuations []AssetValuation `json:"valuations"`
}

func (a Asset) Key() []byte    { return []byte(a.Id) }
func (a Asset) Bucket() []byte { return buckets.AssetBucket }

func (a *Asset) Value() ([]byte, error) {
	return json.Marshal(a)
}

// SortedValuations returns the valuations oldest first, which is the order a
// chart needs and not an order the caller can rely on the stored slice being in.
func (a Asset) SortedValuations() []AssetValuation {
	sorted := make([]AssetValuation, len(a.Valuations))
	copy(sorted, a.Valuations)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i].Date.Before(sorted[j].Date)
	})
	return sorted
}

// CurrentValue is the most recent valuation, falling back to the purchase price
// when nothing has been recorded since.
func (a Asset) CurrentValue() float64 {
	sorted := a.SortedValuations()
	if len(sorted) == 0 {
		return a.PurchasePrice
	}
	return sorted[len(sorted)-1].Value
}
