package application

import (
	"cmp"
	"math"
	"net/http"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/telemetry"
	"github.com/dustin/go-humanize"
	"github.com/labstack/echo/v5"
	otellog "go.opentelemetry.io/otel/log"
)

// The admin page shows what budge has recorded about itself since it
// started: how the process is doing, how long requests and queries take and
// where the queries come from, and what it has logged.

const (
	requestDuration = "http.server.request.duration"
	queryDuration   = "db.client.operation.duration"

	adminListLimit = 50
	adminLogLimit  = 200
)

var adminTabs = []AdminTab{
	{Key: "overview", Label: "Overview", Icon: "bi-speedometer2"},
	{Key: "requests", Label: "Requests", Icon: "bi-arrow-left-right"},
	{Key: "database", Label: "Database", Icon: "bi-database"},
	{Key: "logs", Label: "Logs", Icon: "bi-journal-text"},
	{Key: "errors", Label: "Errors", Icon: "bi-exclamation-triangle"},
}

type AdminTab struct {
	Key   string
	Label string
	Icon  string
}

// AdminPage is everything the admin page shows. Only the selected tab's part
// is filled in.
type AdminPage struct {
	Tabs     []AdminTab
	Tab      string
	Process  AdminProcess
	Overview AdminOverview
	Requests AdminRequests
	Database AdminDatabase
	Logs     AdminLogs
	Errors   AdminErrors
}

// AdminProcess is the header: what is running and for how long.
type AdminProcess struct {
	Started   time.Time
	Uptime    time.Duration
	Version   string
	GoVersion string
	Revision  string
	CPUs      int
	Exporting bool
	Retention time.Duration
}

// AdminChart is a series ready for Chart.js: labels and values side by side.
// AdminChart is a series ready for Chart.js, on a time axis from From to
// To in milliseconds. A point with no value breaks the line where samples
// are missing, rather than drawing across the gap as if they were not.
type AdminChart struct {
	Points   []AdminPoint `json:"points"`
	From     int64        `json:"from"`
	To       int64        `json:"to"`
	Ticks    []int64      `json:"ticks"`
	Days     bool         `json:"days"`
	Restarts []int64      `json:"restarts"`
}

type AdminPoint struct {
	X int64    `json:"x"`
	Y *float64 `json:"y"`
}

// AdminAxis is the time a chart spans, where its ticks fall, and how far
// apart samples are expected, so a longer gap can be drawn as one.
type AdminAxis struct {
	From, To time.Time
	Ticks    []time.Time
	Days     bool
	// Expected is how far apart samples are at a time: the sampling
	// interval in memory, the write interval on disk, or the thinning, if
	// that is wider.
	Expected func(at time.Time) time.Duration
}

type AdminOverview struct {
	Requests  uint64
	ErrorRate float64
	Queries   uint64
	QueryTime time.Duration
	Errors    int
	Memory    float64

	Ranges  []AdminRange
	Range   AdminRange
	Runs    []telemetry.Run
	Storage telemetry.Storage

	Goroutines float64
	CPU        float64

	CPUChart        AdminChart
	MemoryChart     AdminChart
	GCGoalChart     AdminChart
	GoroutineChart  AdminChart
	RequestChart    AdminChart
	QueryChart      AdminChart
	SampleEvery     string
	AwaitingSamples bool
}

// AdminTiming is a histogram summed up for a table row.
type AdminTiming struct {
	Count uint64
	Mean  time.Duration
	P50   time.Duration
	P95   time.Duration
	Max   time.Duration
	Total time.Duration
}

func timing(h telemetry.Histogram) AdminTiming {
	return AdminTiming{
		Count: h.Count,
		Mean:  seconds(h.Mean()),
		P50:   seconds(h.Quantile(0.5)),
		P95:   seconds(h.Quantile(0.95)),
		Max:   seconds(h.Max),
		Total: seconds(h.Sum),
	}
}

func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

type AdminRoute struct {
	Method string
	Route  string
	Errors uint64
	AdminTiming
}

// AdminRequest is one recent request, with what its queries cost it.
type AdminRequest struct {
	telemetry.Span
	Method    string
	Route     string
	Status    string
	Queries   int
	QueryTime time.Duration
}

type AdminRequests struct {
	Routes []AdminRoute
	Recent []AdminRequest
	Slow   []AdminRequest
	Jobs   []telemetry.Span
}

type AdminOperation struct {
	Operation  string
	Collection string
	Kind       string
	AdminTiming
}

// AdminCaller is a function outside the store and every query it made.
type AdminCaller struct {
	Caller     string
	Operations []string
	AdminTiming
}

// AdminQuery is one recent query and where it came from.
type AdminQuery struct {
	telemetry.Span
	Operation  string
	Collection string
	Caller     string
	Location   string
}

type AdminDatabase struct {
	Operations []AdminOperation
	Callers    []AdminCaller
	Slow       []AdminQuery
}

type AdminLogs struct {
	Level   string
	Search  string
	Levels  []string
	Records []telemetry.LogRecord
	Total   int
}

type AdminErrors struct {
	Logs     []telemetry.LogRecord
	Failures []telemetry.Span
}

// AdminTrace is a trace drawn as a waterfall.
type AdminTrace struct {
	ID       string
	Root     telemetry.Span
	Duration time.Duration
	Rows     []AdminTraceRow
	Logs     []telemetry.LogRecord
	Spent    []AdminShare
}

// AdminShare is part of a trace's time and what it went on.
type AdminShare struct {
	Kind     string
	Label    string
	Detail   string
	Duration time.Duration
	Share    float64
}

// spent splits a trace's time between the store, each service called out
// to, and budge's own work, which is whatever is left. Queries and calls out
// never nest inside one another, so their times add up without counting
// anything twice.
func spent(rows []AdminTraceRow, total time.Duration) []AdminShare {
	var queries time.Duration
	var count int
	calls := map[string]time.Duration{}
	callCounts := map[string]int{}
	for _, row := range rows {
		switch {
		case isQuery(row.Span):
			queries += row.Duration
			count++
		case row.Kind == "client" && row.Attrs["http.request.method"] != "":
			host := cmp.Or(row.Attrs["server.address"], "elsewhere")
			calls[host] += row.Duration
			callCounts[host]++
		}
	}
	var shares []AdminShare
	own := total
	if count > 0 {
		shares = append(shares, AdminShare{Kind: "database", Label: "Database", Detail: counted(count, "query", "queries"), Duration: queries})
		own -= queries
	}
	for host, d := range calls {
		shares = append(shares, AdminShare{Kind: "call", Label: host, Detail: counted(callCounts[host], "call", "calls"), Duration: d})
		own -= d
	}
	slices.SortFunc(shares, func(a, b AdminShare) int { return cmp.Compare(b.Duration, a.Duration) })
	shares = append(shares, AdminShare{Kind: "own", Label: "Budge", Detail: "its own work", Duration: max(0, own)})
	for i := range shares {
		if total > 0 {
			shares[i].Share = float64(shares[i].Duration) / float64(total)
		}
	}
	return shares
}

func counted(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

type AdminTraceRow struct {
	telemetry.Span
	// Bar is the kind of work, which colours its bar: a query, a call out
	// ("client"), a request ("server") or budge's own ("internal").
	Bar      string
	Depth    int
	Offset   float64
	Width    float64
	Caller   string
	Location string
}

// Admin shows the admin page, opened on the tab asked for.
func (app *Application) Admin(c *echo.Context) error {
	ctx := c.Request().Context()
	tel := app.telemetry
	// Histogram totals are otherwise only as fresh as the last sample.
	_ = tel.Metrics.Refresh(ctx)

	page := AdminPage{Tabs: adminTabs, Tab: c.QueryParam("tab"), Process: process(tel)}
	switch page.Tab {
	case "requests":
		page.Requests = adminRequests(tel)
	case "database":
		page.Database = adminDatabase(tel)
	case "logs":
		page.Logs = adminLogs(tel, c.QueryParam("level"), c.QueryParam("q"))
	case "errors":
		page.Errors = adminErrors(tel)
	default:
		page.Tab = "overview"
		page.Overview = adminOverview(tel, c.QueryParam("range"))
	}
	return c.Render(http.StatusOK, "admin", page)
}

// AdminCompact packs the telemetry file down now, rather than waiting for
// enough of it to be free.
func (app *Application) AdminCompact(c *echo.Context) error {
	result, err := app.telemetry.Compact()
	if err != nil {
		app.Toast(c, "Error", "Could not compact: "+err.Error())
	} else {
		app.Toast(c, "Success", "Compacted, giving back "+humanize.IBytes(uint64(result.Freed()))+".")
	}
	return c.Render(http.StatusOK, "admin.storage", app.telemetry.Storage())
}

// AdminTrace shows one trace as a waterfall.
func (app *Application) AdminTrace(c *echo.Context) error {
	trace, ok := buildTrace(app.telemetry, c.Param("id"))
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "that trace is no longer kept")
	}
	return c.Render(http.StatusOK, "admin.trace", trace)
}

func process(tel *telemetry.Telemetry) AdminProcess {
	return AdminProcess{
		Started:   tel.Started,
		Uptime:    tel.Uptime().Round(time.Second),
		Version:   tel.Run.Version,
		GoVersion: runtime.Version(),
		Revision:  tel.Run.Revision,
		CPUs:      runtime.NumCPU(),
		Exporting: tel.Exporting,
		Retention: tel.Retention,
	}
}

// AdminRange is a span of time the charts can show.
type AdminRange struct {
	Key   string
	Label string
	Span  time.Duration
}

var adminRanges = []AdminRange{
	{"1h", "1 hour", time.Hour},
	{"6h", "6 hours", 6 * time.Hour},
	{"24h", "24 hours", 24 * time.Hour},
	{"7d", "7 days", 7 * 24 * time.Hour},
	{"30d", "30 days", 30 * 24 * time.Hour},
}

// chartPoints is as many points as a chart draws legibly.
const chartPoints = 360

// ranges are the spans of time there is something kept for: what memory
// holds, and what the file holds besides. The one asked for is chosen, or six
// hours.
func ranges(tel *telemetry.Telemetry, key string) ([]AdminRange, AdminRange) {
	keeps := max(tel.Window, tel.Retention)
	var out []AdminRange
	for _, r := range adminRanges {
		// The first range past what is kept still shows all of it.
		out = append(out, r)
		if r.Span >= keeps {
			break
		}
	}
	chosen := out[min(1, len(out)-1)]
	for _, r := range out {
		if r.Key == key {
			chosen = r
		}
	}
	return out, chosen
}

func adminOverview(tel *telemetry.Telemetry, rangeKey string) AdminOverview {
	requests := telemetry.Merge(tel.Metrics.Histograms(requestDuration)...)
	queries := telemetry.Merge(tel.Metrics.Histograms(queryDuration)...)
	var failed uint64
	for _, h := range tel.Metrics.Histograms(requestDuration) {
		if serverError(h.Attrs["http.response.status_code"]) {
			failed += h.Count
		}
	}

	o := AdminOverview{
		Requests:    requests.Count,
		Queries:     queries.Count,
		QueryTime:   seconds(queries.Sum),
		SampleEvery: tel.SampleEvery.String(),
		Runs:        tel.Runs(),
		Storage:     tel.Storage(),
	}
	o.Ranges, o.Range = ranges(tel, rangeKey)
	_, o.Errors = tel.Logs.Search(otellog.SeverityError, nil, 0)
	if requests.Count > 0 {
		o.ErrorRate = float64(failed) / float64(requests.Count)
	}

	now := time.Now()
	since := now.Add(-o.Range.Span)
	var starts []time.Time
	for _, run := range o.Runs {
		starts = append(starts, run.Start.Time)
	}
	thinned := o.Range.Span / (chartPoints - 1)
	inMemory := tel.Metrics.Oldest()
	axis := AdminAxis{
		From: since, To: now, Ticks: ticks(since, now, o.Range.Span), Days: o.Range.Span > 24*time.Hour,
		Expected: func(at time.Time) time.Duration {
			step := tel.SampleEvery
			if inMemory.IsZero() || at.Before(inMemory) {
				step = max(step, tel.PersistEvery)
			}
			return max(step, thinned)
		},
	}
	history := func(name string) []telemetry.Point {
		return telemetry.Resample(tel.Metrics.History(name, since), chartPoints)
	}
	// Rates are taken after thinning, across each stretch, so a week's
	// chart is as smooth as an hour's.
	rate := func(name string, scale float64) []telemetry.Point {
		points := telemetry.Rate(history(name))
		for i := range points {
			points[i].Value *= scale
		}
		return points
	}
	draw := func(points []telemetry.Point) AdminChart { return chart(points, axis, starts) }

	// process.cpu.time is seconds of CPU spent, so its rate is how many cores
	// were busy; as a percentage, 100% is one core, the way top reads.
	cpu := rate("process.cpu.time", 100)
	memory := history("go.memory.used")
	goroutines := history("go.goroutine.count")
	o.CPUChart = draw(cpu)
	o.MemoryChart = draw(memory)
	o.GCGoalChart = draw(history("go.memory.gc.goal"))
	o.GoroutineChart = draw(goroutines)
	o.RequestChart = draw(rate(requestDuration+telemetry.CountSuffix, 60))
	o.QueryChart = draw(rate(queryDuration+telemetry.CountSuffix, 60))

	o.Memory = last(memory)
	o.Goroutines = last(goroutines)
	o.CPU = last(cpu)
	o.AwaitingSamples = len(cpu) == 0
	o.Runs = o.Runs[:min(len(o.Runs), 10)]
	return o
}

// gapAfter is how many expected intervals without a sample make a gap.
const gapAfter = 3

// chart lays points out for Chart.js along the whole axis, breaking the line
// wherever samples stop for longer than expected, and marks where budge
// started again so a gap or a drop can be read for what it is.
func chart(points []telemetry.Point, axis AdminAxis, starts []time.Time) AdminChart {
	c := AdminChart{
		Points: make([]AdminPoint, 0, len(points)), From: axis.From.UnixMilli(), To: axis.To.UnixMilli(),
		Ticks: make([]int64, len(axis.Ticks)), Days: axis.Days, Restarts: []int64{},
	}
	for i, at := range axis.Ticks {
		c.Ticks[i] = at.UnixMilli()
	}
	for i, p := range points {
		if i > 0 {
			previous := points[i-1].Time
			if p.Time.Sub(previous) > gapAfter*axis.Expected(previous) {
				c.Points = append(c.Points, AdminPoint{X: previous.Add(p.Time.Sub(previous) / 2).UnixMilli()})
			}
		}
		value := p.Value
		c.Points = append(c.Points, AdminPoint{X: p.Time.UnixMilli(), Y: &value})
	}
	for _, start := range starts {
		if !start.Before(axis.From) && !start.After(axis.To) {
			c.Restarts = append(c.Restarts, start.UnixMilli())
		}
	}
	return c
}

// tickSteps are where ticks fall for each span: on the hour, at midnight,
// and so on, so they read as the clock does.
var tickSteps = []struct{ upTo, step time.Duration }{
	{time.Hour, 10 * time.Minute},
	{6 * time.Hour, time.Hour},
	{24 * time.Hour, 3 * time.Hour},
	{7 * 24 * time.Hour, 24 * time.Hour},
	{math.MaxInt64, 5 * 24 * time.Hour},
}

// ticks are the clock times from..to falls across, in local time.
func ticks(from, to time.Time, span time.Duration) []time.Time {
	var step time.Duration
	for _, s := range tickSteps {
		if span <= s.upTo {
			step = s.step
			break
		}
	}
	local := from.Local()
	day := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.Local)
	var out []time.Time
	if step >= 24*time.Hour {
		days := int(step / (24 * time.Hour))
		for at := day; !at.After(to); at = at.AddDate(0, 0, days) {
			if !at.Before(from) {
				out = append(out, at)
			}
		}
		return out
	}
	for at := day.Add(local.Sub(day) / step * step); !at.After(to); at = at.Add(step) {
		if !at.Before(from) {
			out = append(out, at)
		}
	}
	return out
}

func last(points []telemetry.Point) float64 {
	if len(points) == 0 {
		return 0
	}
	return points[len(points)-1].Value
}

func serverError(status string) bool {
	code, err := strconv.Atoi(status)
	return err == nil && code >= 500
}

func adminRequests(tel *telemetry.Telemetry) AdminRequests {
	type key struct{ method, route string }
	grouped := map[key][]telemetry.Histogram{}
	errors := map[key]uint64{}
	for _, h := range tel.Metrics.Histograms(requestDuration) {
		// A request no route matched has none to group by.
		k := key{h.Attrs["http.request.method"], cmp.Or(h.Attrs["http.route"], "(unmatched)")}
		grouped[k] = append(grouped[k], h)
		if serverError(h.Attrs["http.response.status_code"]) {
			errors[k] += h.Count
		}
	}
	var out AdminRequests
	for k, hs := range grouped {
		out.Routes = append(out.Routes, AdminRoute{Method: k.method, Route: k.route, Errors: errors[k], AdminTiming: timing(telemetry.Merge(hs...))})
	}
	// The routes that cost the most in all come first.
	slices.SortFunc(out.Routes, func(a, b AdminRoute) int {
		return cmp.Or(cmp.Compare(b.Total, a.Total), strings.Compare(a.Route, b.Route))
	})

	// Every request and job kept, back across restarts. Only the slowest few
	// are held on to while walking, so a week of requests costs little.
	var slow []AdminRequest
	tel.Spans.History(func(span telemetry.Span) bool {
		if span.Kind != "server" {
			if len(out.Jobs) < 10 {
				out.Jobs = append(out.Jobs, span)
			}
			return true
		}
		request := AdminRequest{
			Span:   span,
			Method: span.Attrs["http.request.method"],
			Route:  cmp.Or(span.Attrs["http.route"], span.Attrs["url.path"]),
			Status: span.Attrs["http.response.status_code"],
		}
		if len(out.Recent) < adminListLimit {
			out.Recent = append(out.Recent, request)
		}
		slow = keepSlowest(slow, request, 10)
		return true
	})
	out.Slow = slow
	// What each listed request's queries came to, from its trace.
	for _, list := range [][]AdminRequest{out.Recent, out.Slow} {
		for i := range list {
			for _, span := range tel.Spans.Trace(list[i].TraceID) {
				if isQuery(span) {
					list[i].Queries++
					list[i].QueryTime += span.Duration
				}
			}
		}
	}
	return out
}

// keepSlowest adds a request to the slowest seen, keeping only n, slowest
// first.
func keepSlowest(slow []AdminRequest, request AdminRequest, n int) []AdminRequest {
	if len(slow) == n && request.Duration <= slow[n-1].Duration {
		return slow
	}
	i, _ := slices.BinarySearchFunc(slow, request, func(a, b AdminRequest) int { return cmp.Compare(b.Duration, a.Duration) })
	slow = slices.Insert(slow, i, request)
	return slow[:min(len(slow), n)]
}

func isQuery(span telemetry.Span) bool { return span.Attrs["db.system.name"] == "bbolt" }

func adminDatabase(tel *telemetry.Telemetry) AdminDatabase {
	type opKey struct{ operation, collection, kind string }
	byOperation := map[opKey][]telemetry.Histogram{}
	byCaller := map[string][]telemetry.Histogram{}
	callerOps := map[string]map[string]bool{}
	for _, h := range tel.Metrics.Histograms(queryDuration) {
		k := opKey{h.Attrs["db.operation.name"], h.Attrs["db.collection.name"], h.Attrs["bbolt.transaction"]}
		byOperation[k] = append(byOperation[k], h)
		caller := h.Attrs["code.function.name"]
		byCaller[caller] = append(byCaller[caller], h)
		if callerOps[caller] == nil {
			callerOps[caller] = map[string]bool{}
		}
		callerOps[caller][k.operation] = true
	}

	var out AdminDatabase
	for k, hs := range byOperation {
		out.Operations = append(out.Operations, AdminOperation{Operation: k.operation, Collection: k.collection, Kind: k.kind, AdminTiming: timing(telemetry.Merge(hs...))})
	}
	slices.SortFunc(out.Operations, func(a, b AdminOperation) int {
		return cmp.Or(cmp.Compare(b.Total, a.Total), strings.Compare(a.Operation, b.Operation))
	})
	for caller, hs := range byCaller {
		var ops []string
		for op := range callerOps[caller] {
			ops = append(ops, op)
		}
		slices.Sort(ops)
		out.Callers = append(out.Callers, AdminCaller{Caller: caller, Operations: ops, AdminTiming: timing(telemetry.Merge(hs...))})
	}
	slices.SortFunc(out.Callers, func(a, b AdminCaller) int {
		return cmp.Or(cmp.Compare(b.Total, a.Total), strings.Compare(a.Caller, b.Caller))
	})

	queries := tel.Spans.Recent(isQuery)
	slices.SortStableFunc(queries, func(a, b telemetry.Span) int { return cmp.Compare(b.Duration, a.Duration) })
	for _, span := range queries[:min(len(queries), 20)] {
		out.Slow = append(out.Slow, AdminQuery{
			Span:       span,
			Operation:  span.Attrs["db.operation.name"],
			Collection: span.Attrs["db.collection.name"],
			Caller:     span.Attrs["code.function.name"],
			Location:   location(span),
		})
	}
	return out
}

// location is where in budge's source a query was asked for, from the
// module's root: "pkg/application/budget.go:42".
func location(span telemetry.Span) string {
	file := span.Attrs["code.file.path"]
	if file == "" {
		return ""
	}
	if i := strings.LastIndex(file, "/pkg/"); i >= 0 {
		file = file[i+1:]
	}
	return file + ":" + span.Attrs["code.line.number"]
}

var logLevels = map[string]otellog.Severity{
	"debug": otellog.SeverityDebug,
	"info":  otellog.SeverityInfo,
	"warn":  otellog.SeverityWarn,
	"error": otellog.SeverityError,
}

func adminLogs(tel *telemetry.Telemetry, level, search string) AdminLogs {
	if _, ok := logLevels[level]; !ok {
		level = "info"
	}
	out := AdminLogs{Level: level, Search: search, Levels: []string{"debug", "info", "warn", "error"}}
	var match func(telemetry.LogRecord) bool
	if needle := strings.ToLower(strings.TrimSpace(search)); needle != "" {
		match = func(record telemetry.LogRecord) bool { return matchesLog(record, needle) }
	}
	out.Records, out.Total = tel.Logs.Search(logLevels[level], match, adminLogLimit)
	return out
}

func matchesLog(record telemetry.LogRecord, needle string) bool {
	if strings.Contains(strings.ToLower(record.Message), needle) {
		return true
	}
	for k, v := range record.Attrs {
		if strings.Contains(strings.ToLower(k+"="+v), needle) {
			return true
		}
	}
	return false
}

func adminErrors(tel *telemetry.Telemetry) AdminErrors {
	logs, _ := tel.Logs.Search(otellog.SeverityError, nil, adminListLimit)
	// Anything that failed and is still in memory, and every request or job
	// that failed back across the retention.
	failures := tel.Spans.Recent(func(span telemetry.Span) bool { return span.Error })
	seen := map[string]bool{}
	for _, span := range failures {
		seen[span.SpanID] = true
	}
	tel.Spans.History(func(span telemetry.Span) bool {
		if span.Error && !seen[span.SpanID] {
			failures = append(failures, span)
		}
		return len(failures) < adminListLimit
	})
	slices.SortStableFunc(failures, func(a, b telemetry.Span) int { return b.End().Compare(a.End()) })
	return AdminErrors{
		Logs:     logs,
		Failures: failures[:min(len(failures), adminListLimit)],
	}
}

// buildTrace lays a trace's spans out beneath each other, each indented under
// its parent and placed along the root's timeline.
func buildTrace(tel *telemetry.Telemetry, id string) (AdminTrace, bool) {
	spans := tel.Spans.Trace(id)
	if len(spans) == 0 {
		return AdminTrace{}, false
	}
	start, end := spans[0].Start, spans[0].End()
	children := map[string][]telemetry.Span{}
	known := map[string]bool{}
	for _, span := range spans {
		known[span.SpanID] = true
		if span.Start.Before(start) {
			start = span.Start
		}
		if span.End().After(end) {
			end = span.End()
		}
	}
	var roots []telemetry.Span
	for _, span := range spans {
		// A span whose parent was not kept still belongs in the picture.
		if span.ParentID == "" || !known[span.ParentID] {
			roots = append(roots, span)
		} else {
			children[span.ParentID] = append(children[span.ParentID], span)
		}
	}

	total := end.Sub(start)
	trace := AdminTrace{ID: id, Root: roots[0], Duration: total}
	var walk func(span telemetry.Span, depth int)
	walk = func(span telemetry.Span, depth int) {
		row := AdminTraceRow{Span: span, Bar: span.Kind, Depth: depth, Width: 100, Caller: span.Attrs["code.function.name"], Location: location(span)}
		if total > 0 {
			row.Offset = 100 * float64(span.Start.Sub(start)) / float64(total)
			// Wide enough to see, however quick.
			row.Width = max(0.5, 100*float64(span.Duration)/float64(total))
		}
		if isQuery(span) {
			row.Bar = "query"
		}
		trace.Rows = append(trace.Rows, row)
		for _, child := range children[span.SpanID] {
			walk(child, depth+1)
		}
	}
	for _, root := range roots {
		walk(root, 0)
	}
	trace.Spent = spent(trace.Rows, total)
	for _, record := range tel.Logs.Recent(otellog.SeverityDebug) {
		if record.TraceID == id {
			trace.Logs = append(trace.Logs, record)
		}
	}
	slices.Reverse(trace.Logs)
	return trace, true
}
