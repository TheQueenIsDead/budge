package application

import (
	"fmt"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDomesticPlace(t *testing.T) {
	place := func(merchant, description string) string {
		var tx models.Transaction
		tx.Merchant.Name, tx.Description = merchant, description
		return domesticPlace(tx)
	}

	assert.Equal(t, "Motueka", place("Hotel Motueka", ""))
	assert.Equal(t, "Murchison", place("", "RIVERS CAFE MURCHISON"))
	assert.Equal(t, "Lewis Pass", place("", "GAS LEWIS PASS BULLER"), "the first place named wins")
	assert.Equal(t, "Havelock North", place("", "VILLAGE CAFE HAVELOCK NORTH"), "longer names first")
	assert.Equal(t, "", place("Pasta Pasta", "155 HIGH ST"))
	assert.Equal(t, "", place("", "PAY ROSS SMITH"), "surnames that are also towns are left out")
	assert.Equal(t, "", place("", "MOTUEKAS"), "whole words only")
}

func TestDistanceKm(t *testing.T) {
	christchurch := nzPlaceByName["christchurch"]
	assert.InDelta(t, 17, distanceKm(christchurch, nzPlaceByName["kaiapoi"]), 5)
	assert.InDelta(t, 300, distanceKm(christchurch, nzPlaceByName["motueka"]), 30)
}

// homeYear is a year of ordinary spending in Christchurch, which makes it home.
func homeYear() []models.Transaction {
	var txs []models.Transaction
	for month := time.January; month <= time.December; month++ {
		tx := purchase(fmt.Sprintf("home-%d", month), "", 30, month, 5)
		tx.Description = "COFFEE CHRISTCHURCH"
		txs = append(txs, tx)
	}
	return txs
}

func placed(id, merchant, description, category string, amount float64, month time.Month, day int) models.Transaction {
	tx := purchase(id, merchant, amount, month, day)
	tx.Description = description
	tx.Category.Name = category
	return tx
}

func TestHomePlaces(t *testing.T) {
	txs := append(homeYear(), placed("motel", "Hotel Motueka", "", "Accommodation", 180, time.March, 28))
	home := homePlaces(txs, time.UTC)
	assert.True(t, home["Christchurch"])
	assert.False(t, home["Motueka"])
}

func TestSuggestAwayTripsMotueka(t *testing.T) {
	// The real pattern: a road trip up through Lewis Pass and Murchison to
	// Motueka and Golden Bay, home by way of Kaikoura.
	txs := append(homeYear(),
		placed("fuel-1", "Mobil", "", "Fuel stations", 90, time.March, 27),
		placed("fuel-2", "BP", "", "Fuel stations", 60, time.March, 27),
		placed("lewis", "", "GAS LEWIS PASS BULLER", "Fuel stations", 70, time.March, 27),
		placed("cafe", "", "RIVERS CAFE MURCHISON", "Cafes and restaurants", 32, time.March, 27),
		placed("motel", "Hotel Motueka", "", "Accommodation", 180, time.March, 28),
		placed("tavern", "Collingwood Tavern", "", "Bars, pubs, nightclubs", 64, time.March, 28),
		placed("crayfish", "Nins Bin", "", "Specialty food stores", 85, time.March, 29),
		placed("fuel-3", "Z Energy", "", "Fuel stations", 80, time.March, 29),
		placed("fuel-4", "NPD Fuel", "", "Fuel stations", 55, time.March, 29),
		placed("before", "Woolworths", "", "Supermarkets and grocery stores", 140, time.March, 25),
	)

	trips := suggestAwayTrips(txs, homePlaces(txs, time.UTC), nil, time.UTC)
	require.Len(t, trips, 1)
	trip := trips[0]

	assert.Equal(t, "Trip to Motueka", trip.Title, "named for where the night was spent")
	assert.Equal(t, "27 – 29 Mar", trip.Span(), "the drive home is a day on the road")
	assert.Contains(t, trip.TransactionIDs(), "crayfish", "a stop that names no town still comes along")
	assert.NotContains(t, trip.TransactionIDs(), "before")
	assert.Equal(t, "away-2026-03-27", trip.Key)
}

func TestSuggestAwayTripsNearHome(t *testing.T) {
	dayOut := append(homeYear(),
		placed("waikuku", "", "WAIKUKU CAFE WAIKUKU", "Cafes and restaurants", 25, time.September, 14),
		placed("kaiapoi", "", "MR WHIPPY KAIAPOI", "Specialty food stores", 8, time.September, 14),
	)
	assert.Empty(t, suggestAwayTrips(dayOut, homePlaces(dayOut, time.UTC), nil, time.UTC),
		"a day out near home is not a trip")

	weekend := append(homeYear(),
		placed("inn", "Akaroa Village Inn", "", "Accommodation", 260, time.November, 7),
		placed("fish", "", "FISH AND CHIPS AKAROA", "Fast food stores", 40, time.November, 8),
	)
	trips := suggestAwayTrips(weekend, homePlaces(weekend, time.UTC), nil, time.UTC)
	require.Len(t, trips, 1, "a night away counts however close")
	assert.Equal(t, "Trip to Akaroa", trips[0].Title)
}

func TestSuggestAwayTripsNamedForTheFarthestPlace(t *testing.T) {
	txs := append(homeYear(),
		placed("culverden", "", "CAFE CULVERDEN", "Cafes and restaurants", 20, time.October, 10),
		placed("pier", "The Pier Hotel", "PIER HOTEL KAIKOURA", "Bars, pubs, nightclubs", 90, time.October, 10),
	)
	trips := suggestAwayTrips(txs, homePlaces(txs, time.UTC), nil, time.UTC)
	require.Len(t, trips, 1)
	assert.Equal(t, "Trip to Kaikoura", trips[0].Title)
	assert.Equal(t, "spending around Kaikoura and Culverden", trips[0].Reason)
}

func TestBuildTimelineSuggestsTripsAtHome(t *testing.T) {
	txs := append(ordinaryYear(), homeYear()...)
	txs = append(txs,
		placed("motel", "Hotel Motueka", "", "Accommodation", 180, time.March, 28),
		placed("tavern", "Collingwood Tavern", "", "Bars, pubs, nightclubs", 164, time.March, 28),
	)
	txs = append(txs, singaporeTrip()...)

	timeline := BuildTimeline(txs, nil, models.TimelinePreferences{}, timelineStart, timelineEnd)

	var titles []string
	for _, suggestion := range timeline.Suggestions {
		titles = append(titles, suggestion.Title)
	}
	assert.ElementsMatch(t, []string{"Trip to Motueka", "Trip to Singapore & Indonesia"}, titles)
	assert.NotContains(t, entryKeys(timeline.Purchases), "tavern", "in the trip, not listed again")
}
