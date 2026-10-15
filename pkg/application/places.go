package application

import (
	"math"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
)

// A holiday at home leaves no foreign currency behind, but it does leave place
// names: card descriptions and merchant names carry the town a purchase was
// made in often enough ("RIVERS CAFE MURCHISON", "Hotel Motueka") to place a
// trip by. These are matched against a list of New Zealand towns, with home
// worked out from where money is spent month after month.

// nzPlace is a town and roughly where it is. The coordinates only need to be
// good to a few kilometres: they decide whether somewhere is near home, not
// which street it is on.
type nzPlace struct {
	Name     string
	Lat, Lon float64
}

// nzPlaces are the towns, localities and stops a card description is likely
// to name. Names that are also common street names or surnames, such as
// Oxford, Richmond, Ross and Lincoln, are left out: a cafe on Oxford Terrace is
// not a trip to Oxford, and a payment to Ross is not a trip to Ross.
var nzPlaces = []nzPlace{
	// Northland and Auckland
	{"Kaitaia", -35.11, 173.26}, {"Ahipara", -35.17, 173.15}, {"Mangonui", -34.99, 173.53},
	{"Kerikeri", -35.23, 173.95}, {"Paihia", -35.28, 174.09}, {"Kawakawa", -35.38, 174.07},
	{"Tutukaka", -35.61, 174.53}, {"Whangarei", -35.73, 174.32}, {"Dargaville", -35.94, 173.87},
	{"Waipu", -35.98, 174.45}, {"Mangawhai", -36.13, 174.58}, {"Matakana", -36.35, 174.72},
	{"Warkworth", -36.40, 174.66}, {"Orewa", -36.59, 174.69}, {"Helensville", -36.68, 174.45},
	{"Auckland", -36.85, 174.76}, {"Waiheke", -36.80, 175.08}, {"Piha", -36.95, 174.47},
	{"Pukekohe", -37.20, 174.90},
	// Coromandel, Waikato and Bay of Plenty
	{"Coromandel", -36.76, 175.50}, {"Whitianga", -36.83, 175.70}, {"Hahei", -36.84, 175.80},
	{"Tairua", -37.00, 175.85}, {"Pauanui", -37.02, 175.86}, {"Whangamata", -37.21, 175.87},
	{"Waihi", -37.39, 175.84}, {"Katikati", -37.55, 175.92}, {"Tauranga", -37.69, 176.17},
	{"Mount Maunganui", -37.64, 176.19}, {"Te Puke", -37.78, 176.32}, {"Whakatane", -37.95, 176.99},
	{"Ohope", -37.97, 177.05}, {"Opotiki", -38.01, 177.29}, {"Huntly", -37.56, 175.16},
	{"Ngaruawahia", -37.67, 175.15}, {"Hamilton", -37.79, 175.28}, {"Raglan", -37.80, 174.87},
	{"Morrinsville", -37.66, 175.53}, {"Matamata", -37.81, 175.77}, {"Te Awamutu", -38.01, 175.32},
	{"Putaruru", -38.05, 175.78}, {"Rotorua", -38.14, 176.25}, {"Otorohanga", -38.19, 175.21},
	{"Tokoroa", -38.22, 175.87}, {"Waitomo", -38.26, 175.11}, {"Te Kuiti", -38.33, 175.16},
	{"Taupo", -38.69, 176.07}, {"Turangi", -38.99, 175.81},
	// East coast, Taranaki and Manawatu
	{"Gisborne", -38.66, 178.02}, {"Wairoa", -39.04, 177.42}, {"Napier", -39.49, 176.91},
	{"Hastings", -39.64, 176.84}, {"Havelock North", -39.67, 176.88}, {"Waipukurau", -39.99, 176.56},
	{"Dannevirke", -40.21, 176.10}, {"New Plymouth", -39.06, 174.08}, {"Stratford", -39.34, 174.28},
	{"Opunake", -39.46, 173.86}, {"Hawera", -39.59, 174.28}, {"Whanganui", -39.93, 175.05},
	{"Wanganui", -39.93, 175.05}, {"National Park", -39.18, 175.40}, {"Ohakune", -39.42, 175.40},
	{"Raetihi", -39.43, 175.28}, {"Taihape", -39.68, 175.80}, {"Feilding", -40.23, 175.57},
	{"Palmerston North", -40.36, 175.61}, {"Foxton", -40.47, 175.28}, {"Levin", -40.62, 175.29},
	{"Otaki", -40.76, 175.15}, {"Waikanae", -40.88, 175.06}, {"Paraparaumu", -40.91, 175.01},
	{"Masterton", -40.95, 175.66}, {"Carterton", -41.03, 175.53}, {"Greytown", -41.08, 175.46},
	{"Featherston", -41.12, 175.33}, {"Martinborough", -41.22, 175.46}, {"Castlepoint", -40.90, 176.22},
	// Wellington
	{"Wellington", -41.29, 174.78}, {"Petone", -41.23, 174.87}, {"Lower Hutt", -41.21, 174.90},
	{"Upper Hutt", -41.12, 175.07}, {"Porirua", -41.13, 174.84}, {"Eastbourne", -41.29, 174.90},
	// Nelson, Tasman and Marlborough
	{"Nelson", -41.27, 173.28}, {"Brightwater", -41.38, 173.11}, {"Mapua", -41.25, 173.10},
	{"Motueka", -41.11, 173.01}, {"Riwaka", -41.08, 172.99}, {"Kaiteriteri", -41.04, 173.02},
	{"Marahau", -41.00, 173.00}, {"Takaka", -40.86, 172.81}, {"Collingwood", -40.68, 172.68},
	{"Golden Bay", -40.80, 172.80}, {"Tapawera", -41.39, 172.82}, {"St Arnaud", -41.80, 172.85},
	{"Murchison", -41.80, 172.33}, {"Picton", -41.29, 174.00}, {"Havelock", -41.28, 173.77},
	{"Renwick", -41.51, 173.83}, {"Blenheim", -41.51, 173.96}, {"Kekerengu", -42.00, 173.98},
	{"Kaikoura", -42.40, 173.68}, {"Hanmer Springs", -42.52, 172.83}, {"Cheviot", -42.81, 173.27},
	// West Coast
	{"Karamea", -41.25, 172.11}, {"Westport", -41.75, 171.60}, {"Buller", -41.75, 171.60},
	{"Punakaiki", -42.11, 171.33}, {"Reefton", -42.12, 171.86}, {"Maruia", -42.19, 172.22},
	{"Springs Junction", -42.33, 172.18}, {"Lewis Pass", -42.39, 172.40}, {"Blackball", -42.36, 171.41},
	{"Greymouth", -42.45, 171.21}, {"Hokitika", -42.72, 170.97}, {"Arthurs Pass", -42.94, 171.56},
	{"Harihari", -43.15, 170.56}, {"Whataroa", -43.26, 170.36}, {"Franz Josef", -43.39, 170.18},
	{"Fox Glacier", -43.46, 170.02}, {"Haast", -43.88, 169.04},
	// Canterbury
	{"Christchurch", -43.53, 172.64}, {"Lyttelton", -43.60, 172.72}, {"Diamond Harbour", -43.63, 172.73},
	{"Kaiapoi", -43.38, 172.66}, {"Woodend", -43.32, 172.67}, {"Waikuku", -43.29, 172.70},
	{"Rangiora", -43.30, 172.59}, {"Amberley", -43.16, 172.73}, {"Waikari", -42.97, 172.69},
	{"Culverden", -42.77, 172.85}, {"Prebbleton", -43.58, 172.51}, {"Rolleston", -43.59, 172.38},
	{"Leeston", -43.76, 172.30}, {"Darfield", -43.49, 172.11}, {"Hororata", -43.54, 171.95},
	{"Little River", -43.77, 172.79}, {"Akaroa", -43.80, 172.97}, {"Castle Hill", -43.21, 171.71},
	{"Methven", -43.63, 171.65}, {"Mt Hutt", -43.47, 171.53}, {"Mount Hutt", -43.47, 171.53},
	{"Rakaia", -43.75, 172.02}, {"Ashburton", -43.90, 171.75}, {"Geraldine", -44.09, 171.24},
	{"Temuka", -44.24, 171.28}, {"Pleasant Point", -44.26, 171.13}, {"Fairlie", -44.10, 170.83},
	{"Timaru", -44.40, 171.25}, {"Lake Tekapo", -44.00, 170.48}, {"Tekapo", -44.00, 170.48},
	{"Twizel", -44.26, 170.10}, {"Mount Cook", -43.73, 170.10}, {"Mt Cook", -43.73, 170.10},
	{"Aoraki", -43.73, 170.10}, {"Waimate", -44.73, 171.05}, {"Kurow", -44.73, 170.47},
	{"Omarama", -44.49, 169.97},
	// Otago and Southland
	{"Oamaru", -45.10, 170.97}, {"Moeraki", -45.36, 170.86}, {"Middlemarch", -45.51, 170.12},
	{"Ranfurly", -45.13, 170.10}, {"Naseby", -45.02, 170.14}, {"Port Chalmers", -45.82, 170.62},
	{"Dunedin", -45.87, 170.50}, {"Portobello", -45.85, 170.65}, {"Mosgiel", -45.88, 170.35},
	{"Balclutha", -46.24, 169.74}, {"Kaka Point", -46.39, 169.78}, {"Owaka", -46.45, 169.66},
	{"Roxburgh", -45.54, 169.31}, {"Alexandra", -45.25, 169.38}, {"Cromwell", -45.04, 169.20},
	{"Bannockburn", -45.08, 169.16}, {"Wanaka", -44.70, 169.13}, {"Hawea", -44.61, 169.27},
	{"Cardrona", -44.87, 168.95}, {"Arrowtown", -44.94, 168.83}, {"Queenstown", -45.03, 168.66},
	{"Frankton", -45.02, 168.73}, {"Glenorchy", -44.85, 168.39}, {"Te Anau", -45.41, 167.72},
	{"Manapouri", -45.56, 167.61}, {"Milford Sound", -44.67, 167.93}, {"Gore", -46.10, 168.94},
	{"Mataura", -46.19, 168.86}, {"Invercargill", -46.41, 168.35}, {"Riverton", -46.35, 168.02},
	{"Tuatapere", -46.13, 167.69}, {"Stewart Island", -46.90, 168.13},
}

// nzPlaceByName maps a place name, lower cased, back to the place.
var nzPlaceByName = func() map[string]nzPlace {
	places := make(map[string]nzPlace, len(nzPlaces))
	for _, place := range nzPlaces {
		places[strings.ToLower(place.Name)] = place
	}
	return places
}()

// nzPlacePattern finds a place name as whole words. Longer names are tried
// first, so "Havelock North" is not read as Havelock.
var nzPlacePattern = func() *regexp.Regexp {
	names := make([]string, len(nzPlaces))
	for i, place := range nzPlaces {
		names[i] = place.Name
	}
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })
	for i, name := range names {
		names[i] = strings.ReplaceAll(regexp.QuoteMeta(name), `\ `, `\s+`)
	}
	return regexp.MustCompile(`(?i)\b(` + strings.Join(names, "|") + `)\b`)
}()

// distanceKm is the distance between two places as the crow flies.
func distanceKm(a, b nzPlace) float64 {
	const earthRadiusKm = 6371
	toRad := func(deg float64) float64 { return deg * math.Pi / 180 }
	dLat, dLon := toRad(b.Lat-a.Lat), toRad(b.Lon-a.Lon)
	h := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(a.Lat))*math.Cos(toRad(b.Lat))*math.Sin(dLon/2)*math.Sin(dLon/2)
	return 2 * earthRadiusKm * math.Asin(math.Sqrt(h))
}

// domesticPlace names the New Zealand town a purchase mentions, or nothing.
// The merchant name is read before the description, since it is the tidier of
// the two.
func domesticPlace(tx models.Transaction) string {
	for _, text := range []string{tx.Merchant.Name, tx.Description} {
		if match := nzPlacePattern.FindString(text); match != "" {
			return nzPlaceByName[strings.ToLower(strings.Join(strings.Fields(match), " "))].Name
		}
	}
	return ""
}

const (
	// homeMonthsMin is the fewest separate months a place must be spent in to
	// count as home, however short the history.
	homeMonthsMin = 4

	// homeMonthsShare is the share of the months with any placed spending a
	// place must appear in to count as home. A spot holidayed in each summer
	// shows up in a few months of a long history; home shows up in most.
	homeMonthsShare = 0.25

	// awayGapDays is the longest run of days without a purchase somewhere
	// else that still counts as one trip away.
	awayGapDays = 2

	// awayMinPurchases is how many purchases away from home make a trip,
	// unless one of them is accommodation.
	awayMinPurchases = 2

	// nearHomeKm is how close to home a place can be and still be part of
	// ordinary life: a drive out to the beach or the outlet mall is a day out,
	// not a trip. Somewhere this close only counts when the night was spent
	// there.
	nearHomeKm = 60.0

	// roadTripFuelStops is how many fuel stops in one day mark it as a day on
	// the road. Nobody fills up three times on a day spent at home.
	roadTripFuelStops = 2
)

// homePlaces works out which places are home from the whole history: the ones
// money is spent in month after month.
func homePlaces(transactions []models.Transaction, location *time.Location) map[string]bool {
	months := map[string]map[time.Time]bool{}
	placed := map[time.Time]bool{}
	for _, tx := range transactions {
		if !timelineSpend(tx) {
			continue
		}
		place := domesticPlace(tx)
		if place == "" {
			continue
		}
		month := periodStart(tx.Date.In(location), CadenceMonthly)
		if months[place] == nil {
			months[place] = map[time.Time]bool{}
		}
		months[place][month] = true
		placed[month] = true
	}

	threshold := float64(len(placed)) * homeMonthsShare
	if threshold < homeMonthsMin {
		threshold = homeMonthsMin
	}

	home := map[string]bool{}
	for place, seen := range months {
		if float64(len(seen)) >= threshold {
			home[place] = true
		}
	}
	return home
}

// awayPlaces decides, for a set of purchases, which places they mention are
// far enough from home to be a trip.
func awayPlaces(txs []models.Transaction, home map[string]bool) map[string]bool {
	away := map[string]bool{}
	for _, tx := range txs {
		place := domesticPlace(tx)
		if _, seen := away[place]; seen || place == "" {
			continue
		}
		away[place] = !home[place] && distanceFromHome(place, home) >= nearHomeKm
	}
	return away
}

// distanceFromHome is how far a place is from the nearest of the home places.
func distanceFromHome(place string, home map[string]bool) float64 {
	here := nzPlaceByName[strings.ToLower(place)]
	nearest := math.Inf(1)
	for other := range home {
		nearest = math.Min(nearest, distanceKm(here, nzPlaceByName[strings.ToLower(other)]))
	}
	return nearest
}

// fuelStop reports whether a purchase was at a fuel station.
func fuelStop(tx models.Transaction) bool {
	return strings.EqualFold(tx.Category.Name, "Fuel stations")
}

// accommodation reports whether a purchase was somewhere to stay. Flights are
// left out: they are booked weeks ahead and say nothing about where the owner
// was that day.
func accommodation(tx models.Transaction) bool {
	return travelBooking(tx) && !strings.Contains(strings.ToLower(tx.Category.Name), "air")
}

// suggestAwayTrips finds holidays at home: runs of days spent somewhere other
// than home. Once the days of a trip are known, everything bought on them comes
// along, since the bakery and the crayfish caravan seldom say which town they
// are in. Routine merchants stay out, as they do for overseas trips.
func suggestAwayTrips(pool []models.Transaction, home map[string]bool, routine map[string]float64, location *time.Location) []TimelineEntry {
	dayOf := func(tx models.Transaction) time.Time {
		local := tx.Date.In(location)
		return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, location)
	}

	// A purchase is away when it is somewhere far from home, or when it is a
	// night's stay anywhere that is not home: a weekend in Akaroa is a trip
	// even though Akaroa is close.
	far := awayPlaces(pool, home)
	isAway := func(tx models.Transaction) (string, bool) {
		place := domesticPlace(tx)
		return place, far[place] || (place != "" && !home[place] && accommodation(tx))
	}

	byDay := map[time.Time][]models.Transaction{}
	awayDays := map[time.Time]bool{}
	for _, tx := range pool {
		day := dayOf(tx)
		byDay[day] = append(byDay[day], tx)
		if _, away := isAway(tx); away {
			awayDays[day] = true
		}
	}

	roadDay := func(day time.Time) bool {
		stops := 0
		for _, tx := range byDay[day] {
			if fuelStop(tx) {
				stops++
			}
		}
		return stops >= roadTripFuelStops
	}

	var days []time.Time
	for day := range awayDays {
		days = append(days, day)
	}
	sort.Slice(days, func(i, j int) bool { return days[i].Before(days[j]) })

	// Days away a little apart are one trip; a day on the road either side is
	// the drive there or back.
	var spans [][2]time.Time
	for _, day := range days {
		if n := len(spans); n > 0 && day.Sub(spans[n-1][1]) <= awayGapDays*24*time.Hour {
			spans[n-1][1] = day
			continue
		}
		spans = append(spans, [2]time.Time{day, day})
	}

	kind := models.MemoryKindByKey("trip")
	var suggestions []TimelineEntry
	for _, span := range spans {
		first, last := span[0], span[1]
		for roadDay(first.AddDate(0, 0, -1)) {
			first = first.AddDate(0, 0, -1)
		}
		for roadDay(last.AddDate(0, 0, 1)) {
			last = last.AddDate(0, 0, 1)
		}

		var txs []models.Transaction
		counts := map[string]int{}
		var places []string
		stayedIn := ""
		awayCount := 0
		for day := first; !day.After(last); day = day.AddDate(0, 0, 1) {
			for _, tx := range byDay[day] {
				place, away := isAway(tx)
				if _, regular := routine[merchantKey(tx)]; regular && !away {
					continue
				}
				txs = append(txs, tx)
				if !away {
					continue
				}
				awayCount++
				if counts[place] == 0 {
					places = append(places, place)
				}
				counts[place]++
				if stayedIn == "" && accommodation(tx) {
					stayedIn = place
				}
			}
		}

		if awayCount < awayMinPurchases && stayedIn == "" {
			continue
		}

		// Named for where the nights were spent if that is known, and
		// otherwise for the place farthest from home: on a road trip that is
		// the destination, and the towns along the way come before it.
		sort.SliceStable(places, func(i, j int) bool {
			if len(home) == 0 {
				return counts[places[i]] > counts[places[j]]
			}
			return distanceFromHome(places[i], home) > distanceFromHome(places[j], home)
		})
		name := places[0]
		if stayedIn != "" {
			name = stayedIn
		}

		entry := TimelineEntry{
			Type:   TimelineSuggestion,
			Key:    "away-" + first.Format(time.DateOnly),
			Title:  "Trip to " + name,
			Kind:   kind,
			Icon:   kind.Icon,
			Accent: kind.Accent,
			Start:  first,
			End:    last,
		}
		entry.addRows(txs)

		shown := places
		if len(shown) > 3 {
			shown = shown[:3]
		}
		entry.Reason = "spending around " + joinPlaces(shown)
		suggestions = append(suggestions, entry)
	}
	return suggestions
}

// joinPlaces lists places as a sentence would: "A", "A and B", "A, B and C".
func joinPlaces(places []string) string {
	if len(places) < 2 {
		return strings.Join(places, "")
	}
	return strings.Join(places[:len(places)-1], ", ") + " and " + places[len(places)-1]
}
