// Package property looks up New Zealand addresses and property valuations.
//
// There is no public API for what a house is worth. The endpoint used here is a
// private one that a property site's own front end calls: undocumented, not
// offered for third party use, and liable to change or start refusing requests
// without notice. Everything here therefore degrades to "no result" rather than
// failing a page, and entering a valuation by hand remains the fallback.
package property

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// The sources an estimate can come from.
const (
	SourceHomes   = "homes.co.nz"
	SourceOneRoof = "OneRoof"
)

// browserUserAgent is sent because both endpoints refuse an empty user agent.
const browserUserAgent = "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36"

// Suggestion is one address match, enough to fill a form and identify a place.
type Suggestion struct {
	Address string  `json:"address"`
	Suburb  string  `json:"suburb"`
	City    string  `json:"city"`
	Lat     float64 `json:"lat"`
	Long    float64 `json:"long"`

	// PropertyID is homes.co.nz's identifier, carried through so an estimate
	// lookup does not have to search for the address a second time.
	PropertyID string `json:"propertyId"`
}

// Client talks to the property sites. The zero value is not usable; use New.
type Client struct {
	http *http.Client
}

func New() *Client {
	return &Client{
		// The lookup sits in front of someone waiting on a form, so it gets a
		// short leash rather than the default of none at all.
		http: &http.Client{Timeout: 8 * time.Second},
	}
}

// homesAddressResponse mirrors only the fields worth reading, so an unrelated
// change to the payload does not break decoding.
type homesAddressResponse struct {
	Results []struct {
		Title      string  `json:"Title"`
		Suburb     string  `json:"Suburb"`
		City       string  `json:"City"`
		Lat        float64 `json:"Lat"`
		Long       float64 `json:"Long"`
		Type       int     `json:"Type"`
		PropertyID string  `json:"PropertyID"`
	} `json:"Results"`
	ErrorMessage string `json:"ErrorMessage"`
}

// homesAddressType denotes a specific property rather than a street or suburb,
// which are the other things this endpoint returns.
const homesAddressType = 4

// SuggestAddresses returns address matches for a partial query. An empty result
// is a normal outcome rather than an error: the field stays a plain text input
// either way and the address can always be typed out.
func (c *Client) SuggestAddresses(ctx context.Context, query string, limit int) ([]Suggestion, error) {
	if query == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 6
	}

	endpoint := fmt.Sprintf(
		"https://gateway.homes.co.nz/address/search?Address=%s&Limit=%d",
		url.QueryEscape(query), limit,
	)

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	// The endpoint refuses an empty user agent.
	request.Header.Set("User-Agent", browserUserAgent)
	request.Header.Set("Accept", "application/json")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("address search returned %d", response.StatusCode)
	}

	var decoded homesAddressResponse
	if err := json.NewDecoder(response.Body).Decode(&decoded); err != nil {
		return nil, err
	}
	if decoded.ErrorMessage != "" {
		return nil, fmt.Errorf("address search: %s", decoded.ErrorMessage)
	}

	suggestions := make([]Suggestion, 0, len(decoded.Results))
	for _, result := range decoded.Results {
		// Streets and suburbs come back from the same endpoint and cannot be
		// valued, so only whole properties are offered.
		if result.Type != homesAddressType {
			continue
		}
		suggestions = append(suggestions, Suggestion{
			Address:    result.Title,
			Suburb:     result.Suburb,
			City:       result.City,
			Lat:        result.Lat,
			Long:       result.Long,
			PropertyID: result.PropertyID,
		})
	}
	return suggestions, nil
}

// Estimate is one source's opinion of what a property is worth.
type Estimate struct {
	Source string  `json:"source"`
	Value  float64 `json:"value"`
}

// Average is the mean of the estimates supplied.
func Average(estimates []Estimate) float64 {
	if len(estimates) == 0 {
		return 0
	}
	var total float64
	for _, estimate := range estimates {
		total += estimate.Value
	}
	return total / float64(len(estimates))
}

// PropertyID identifies a property to homes.co.nz. It comes back with the
// address suggestion, so a lookup never needs a second search.
func (s Suggestion) HasProperty() bool { return s.PropertyID != "" }

// shortValue reads the abbreviated money the property sites publish: "550K",
// "$575K", "1.25M". Both sites round to the nearest ten thousand, so there is no
// finer figure to be had and none is implied.
var shortValuePattern = regexp.MustCompile(`\$?\s*([0-9]+(?:[.,][0-9]+)?)\s*([KkMm])?`)

func parseShortValue(raw string) (float64, bool) {
	match := shortValuePattern.FindStringSubmatch(strings.TrimSpace(raw))
	if match == nil {
		return 0, false
	}

	digits := strings.ReplaceAll(match[1], ",", "")
	value, err := strconv.ParseFloat(digits, 64)
	if err != nil {
		return 0, false
	}

	switch strings.ToUpper(match[2]) {
	case "K":
		value *= 1_000
	case "M":
		value *= 1_000_000
	}

	// A house is not worth two hundred dollars; a bare number that small means
	// the pattern matched something that was not a price.
	if value < 1000 {
		return 0, false
	}
	return value, true
}

type homesPropertiesResponse struct {
	Cards []struct {
		PropertyDetails struct {
			DisplayEstimatedValue string  `json:"display_estimated_value_short"`
			CapitalValue          float64 `json:"capital_value"`
		} `json:"property_details"`
	} `json:"cards"`
}

// HomesEstimate reads the HomesEstimate for a property. This one is a proper
// JSON endpoint, so it is the more dependable of the two sources.
func (c *Client) HomesEstimate(ctx context.Context, propertyID string) (Estimate, error) {
	if propertyID == "" {
		return Estimate{}, errors.New("a homes.co.nz property id is required")
	}

	endpoint := "https://gateway.homes.co.nz/properties?property_ids=" + url.QueryEscape(propertyID)
	body, err := c.get(ctx, endpoint)
	if err != nil {
		return Estimate{}, err
	}

	var decoded homesPropertiesResponse
	if err := json.Unmarshal(body, &decoded); err != nil {
		return Estimate{}, err
	}
	if len(decoded.Cards) == 0 {
		return Estimate{}, errors.New("homes.co.nz returned no property")
	}

	value, ok := parseShortValue(decoded.Cards[0].PropertyDetails.DisplayEstimatedValue)
	if !ok {
		return Estimate{}, errors.New("homes.co.nz gave no estimate for this property")
	}
	return Estimate{Source: SourceHomes, Value: value}, nil
}

// oneRoofEstimatePattern pulls the figure out of the server rendered page.
// OneRoof publishes no usable API for this - their search endpoint demands a
// request signature - but the property page itself is public, and the estimate
// is rendered into its markup. Reading it will break whenever they restyle that
// panel; the failure mode is "no estimate from this source", which the average
// tolerates.
var oneRoofEstimatePattern = regexp.MustCompile(`OneRoof Estimate.{0,600}?>\s*(\$[0-9][0-9.,]*[KkMm]?)\s*<`)

// oneRoofHost is the only host an asset's stored URL is allowed to point at.
// The URL comes from user input and is fetched server side, so without this a
// saved asset could aim the application at anything reachable from the host.
const oneRoofHost = "www.oneroof.co.nz"

// ValidOneRoofURL reports whether a URL is a OneRoof property page.
func ValidOneRoofURL(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	return parsed.Scheme == "https" &&
		parsed.Host == oneRoofHost &&
		strings.HasPrefix(parsed.Path, "/property/")
}

// OneRoofEstimate scrapes the estimate off a OneRoof property page. The page URL
// is stored against the asset rather than looked up, because their search API is
// signature gated and the page is not.
func (c *Client) OneRoofEstimate(ctx context.Context, pageURL string) (Estimate, error) {
	if pageURL == "" {
		return Estimate{}, errors.New("a OneRoof property URL is required")
	}
	if !ValidOneRoofURL(pageURL) {
		return Estimate{}, errors.New("not a OneRoof property URL")
	}

	page, err := c.get(ctx, pageURL)
	if err != nil {
		return Estimate{}, err
	}

	match := oneRoofEstimatePattern.FindSubmatch(page)
	if match == nil {
		return Estimate{}, errors.New("oneroof published no estimate on that page")
	}

	value, ok := parseShortValue(string(match[1]))
	if !ok {
		return Estimate{}, errors.New("oneroof estimate could not be read")
	}
	return Estimate{Source: SourceOneRoof, Value: value}, nil
}

// EstimateResult is the averaged view across every source that answered.
type EstimateResult struct {
	Average   float64    `json:"average"`
	Estimates []Estimate `json:"estimates"`

	// Failed names the sources that did not answer, so the page can say which
	// opinions are missing rather than passing off a partial average as the
	// whole picture.
	Failed []string `json:"failed"`
}

// Estimate asks every configured source at once and averages what comes back.
// A source being unconfigured or unreachable is expected rather than
// exceptional, so it narrows the average instead of failing the lookup.
func (c *Client) Estimate(ctx context.Context, homesPropertyID, oneRoofURL string) EstimateResult {
	type outcome struct {
		estimate Estimate
		err      error
		source   string
	}

	results := make(chan outcome, 2)
	var wg sync.WaitGroup

	if homesPropertyID != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			estimate, err := c.HomesEstimate(ctx, homesPropertyID)
			results <- outcome{estimate, err, SourceHomes}
		}()
	}

	if oneRoofURL != "" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			estimate, err := c.OneRoofEstimate(ctx, oneRoofURL)
			results <- outcome{estimate, err, SourceOneRoof}
		}()
	}

	wg.Wait()
	close(results)

	var result EstimateResult
	for got := range results {
		if got.err != nil || got.estimate.Value <= 0 {
			result.Failed = append(result.Failed, got.source)
			continue
		}
		result.Estimates = append(result.Estimates, got.estimate)
	}

	// Sources answer in whatever order they finish, so order them by name to
	// keep the rendered list stable between lookups.
	sort.Slice(result.Estimates, func(i, j int) bool {
		return result.Estimates[i].Source < result.Estimates[j].Source
	})
	sort.Strings(result.Failed)

	result.Average = Average(result.Estimates)
	return result
}

// get performs a plain GET and returns the body. Both sites refuse an empty
// user agent but neither needs credentials.
func (c *Client) get(ctx context.Context, endpoint string) ([]byte, error) {
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("User-Agent", browserUserAgent)
	request.Header.Set("Accept", "*/*")

	response, err := c.http.Do(request)
	if err != nil {
		return nil, err
	}
	defer func() { _ = response.Body.Close() }()

	if response.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("%s returned %d", endpoint, response.StatusCode)
	}
	// A property page is a few hundred kilobytes; anything far past that is not
	// something worth reading into memory.
	return io.ReadAll(io.LimitReader(response.Body, 4<<20))
}
