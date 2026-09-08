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

	// HomesPropertyID and OneRoofURL identify the property to the valuation
	// sources. Homes is looked up by id from the address autocomplete; OneRoof
	// gates its search behind a request signature, so the public property page
	// URL is stored instead and read directly.
	HomesPropertyID string `json:"homes_property_id"`
	OneRoofURL      string `json:"oneroof_url"`

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

// SameDay reports whether two valuation dates fall on the same calendar day.
// Dates are compared in local time because that is the day the owner means: a
// valuation recorded at 11pm belongs to the day it felt like.
func SameDay(a, b time.Time) bool {
	ay, am, ad := a.Local().Date()
	by, bm, bd := b.Local().Date()
	return ay == by && am == bm && ad == bd
}

// UpsertValuation replaces the valuation held for the same day, or appends when
// that day has none. Two valuations on one day are not two readings: re-fetching
// an estimate corrects today's figure rather than stacking another copy of it,
// and a curve with two points on one date says nothing extra.
func UpsertValuation(valuations []AssetValuation, next AssetValuation) []AssetValuation {
	for i, existing := range valuations {
		if SameDay(existing.Date, next.Date) {
			valuations[i] = next
			return valuations
		}
	}
	return append(valuations, next)
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

// Trackable reports whether an automated estimate can be fetched for this
// asset, which needs at least one source to be identified.
func (a Asset) Trackable() bool {
	return a.HomesPropertyID != "" || a.OneRoofURL != ""
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
