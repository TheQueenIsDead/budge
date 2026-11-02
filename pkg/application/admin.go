package application

import (
	"cmp"
	"net/http"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/telemetry"
	"github.com/labstack/echo/v4"
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
	GoVersion string
	Revision  string
	CPUs      int
	Exporting bool
}

// AdminChart is a series ready for Chart.js: labels and values side by side.
type AdminChart struct {
	Labels []string  `json:"labels"`
	Values []float64 `json:"values"`
}

type AdminOverview struct {
	Requests   uint64
	ErrorRate  float64
	Queries    uint64
	QueryTime  time.Duration
	Errors     int
	Memory     float64
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
}

type AdminTraceRow struct {
	telemetry.Span
	Depth    int
	Offset   float64
	Width    float64
	Caller   string
	Location string
}

// Admin shows the admin page, opened on the tab asked for.
func (app *Application) Admin(c echo.Context) error {
	if app.telemetry == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "telemetry is not running")
	}
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
		page.Overview = adminOverview(tel)
	}
	return c.Render(http.StatusOK, "admin", page)
}

// AdminTrace shows one trace as a waterfall.
func (app *Application) AdminTrace(c echo.Context) error {
	if app.telemetry == nil {
		return echo.NewHTTPError(http.StatusServiceUnavailable, "telemetry is not running")
	}
	trace, ok := buildTrace(app.telemetry, c.Param("id"))
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "that trace is no longer kept")
	}
	return c.Render(http.StatusOK, "admin.trace", trace)
}

func process(tel *telemetry.Telemetry) AdminProcess {
	p := AdminProcess{
		Started:   tel.Started,
		Uptime:    tel.Uptime().Round(time.Second),
		GoVersion: runtime.Version(),
		CPUs:      runtime.NumCPU(),
		Exporting: tel.Exporting,
	}
	if info, ok := debug.ReadBuildInfo(); ok {
		for _, setting := range info.Settings {
			if setting.Key == "vcs.revision" && len(setting.Value) >= 7 {
				p.Revision = setting.Value[:7]
			}
		}
	}
	return p
}

func adminOverview(tel *telemetry.Telemetry) AdminOverview {
	requests := telemetry.Merge(tel.Metrics.Histograms(requestDuration)...)
	queries := telemetry.Merge(tel.Metrics.Histograms(queryDuration)...)
	var failed uint64
	for _, h := range tel.Metrics.Histograms(requestDuration) {
		if serverError(h.Attrs["http.response.status_code"]) {
			failed += h.Count
		}
	}

	// process.cpu.time is seconds of CPU spent, so its rate is how many cores
	// were busy; as a percentage, 100% is one core, the way top reads.
	cpu := telemetry.Rate(tel.Metrics.Total("process.cpu.time"))
	for i := range cpu {
		cpu[i].Value *= 100
	}
	perMinute := func(points []telemetry.Point) []telemetry.Point {
		for i := range points {
			points[i].Value *= 60
		}
		return points
	}

	memory := tel.Metrics.Total("go.memory.used")
	goroutines := tel.Metrics.Total("go.goroutine.count")
	o := AdminOverview{
		Requests:       requests.Count,
		Queries:        queries.Count,
		QueryTime:      seconds(queries.Sum),
		Errors:         len(tel.Logs.Recent(otellog.SeverityError)),
		CPUChart:       chart(cpu),
		MemoryChart:    chart(memory),
		GCGoalChart:    chart(tel.Metrics.Total("go.memory.gc.goal")),
		GoroutineChart: chart(goroutines),
		RequestChart:   chart(perMinute(telemetry.Rate(tel.Metrics.Total(requestDuration + telemetry.CountSuffix)))),
		QueryChart:     chart(perMinute(telemetry.Rate(tel.Metrics.Total(queryDuration + telemetry.CountSuffix)))),
		SampleEvery:    tel.SampleEvery.String(),
	}
	if requests.Count > 0 {
		o.ErrorRate = float64(failed) / float64(requests.Count)
	}
	o.Memory = last(memory)
	o.Goroutines = last(goroutines)
	o.CPU = last(cpu)
	o.AwaitingSamples = len(cpu) == 0
	return o
}

func chart(points []telemetry.Point) AdminChart {
	c := AdminChart{Labels: make([]string, len(points)), Values: make([]float64, len(points))}
	for i, p := range points {
		c.Labels[i] = p.Time.Local().Format("15:04")
		c.Values[i] = p.Value
	}
	return c
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

	// What each trace's queries came to, to set beside its request.
	type cost struct {
		count int
		time  time.Duration
	}
	costs := map[string]cost{}
	for _, span := range tel.Spans.Recent(isQuery) {
		c := costs[span.TraceID]
		costs[span.TraceID] = cost{c.count + 1, c.time + span.Duration}
	}
	var all []AdminRequest
	for _, span := range tel.Spans.Roots() {
		if span.Kind != "server" {
			out.Jobs = append(out.Jobs, span)
			continue
		}
		c := costs[span.TraceID]
		all = append(all, AdminRequest{
			Span:      span,
			Method:    span.Attrs["http.request.method"],
			Route:     cmp.Or(span.Attrs["http.route"], span.Attrs["url.path"]),
			Status:    span.Attrs["http.response.status_code"],
			Queries:   c.count,
			QueryTime: c.time,
		})
	}
	out.Recent = all[:min(len(all), adminListLimit)]
	slow := slices.Clone(all)
	slices.SortStableFunc(slow, func(a, b AdminRequest) int { return cmp.Compare(b.Duration, a.Duration) })
	out.Slow = slow[:min(len(slow), 10)]
	out.Jobs = out.Jobs[:min(len(out.Jobs), 10)]
	return out
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
	needle := strings.ToLower(strings.TrimSpace(search))
	for _, record := range tel.Logs.Recent(logLevels[level]) {
		if needle != "" && !matchesLog(record, needle) {
			continue
		}
		out.Total++
		if len(out.Records) < adminLogLimit {
			out.Records = append(out.Records, record)
		}
	}
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
	logs := tel.Logs.Recent(otellog.SeverityError)
	failures := tel.Spans.Recent(func(span telemetry.Span) bool { return span.Error })
	return AdminErrors{
		Logs:     logs[:min(len(logs), adminListLimit)],
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
		row := AdminTraceRow{Span: span, Depth: depth, Width: 100, Caller: span.Attrs["code.function.name"], Location: location(span)}
		if total > 0 {
			row.Offset = 100 * float64(span.Start.Sub(start)) / float64(total)
			// Wide enough to see, however quick.
			row.Width = max(0.5, 100*float64(span.Duration)/float64(total))
		}
		trace.Rows = append(trace.Rows, row)
		for _, child := range children[span.SpanID] {
			walk(child, depth+1)
		}
	}
	for _, root := range roots {
		walk(root, 0)
	}
	for _, record := range tel.Logs.Recent(otellog.SeverityDebug) {
		if record.TraceID == id {
			trace.Logs = append(trace.Logs, record)
		}
	}
	slices.Reverse(trace.Logs)
	return trace, true
}
