package application

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/telemetry"
	"github.com/labstack/echo/v5"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
)

// instrumentedApp is the real application with telemetry set up, as it runs.
func instrumentedApp(t *testing.T) (*Application, *telemetry.Telemetry) {
	return instrumentedAppKeeping(t, "")
}

// instrumentedAppKeeping is instrumentedApp keeping its telemetry in a file.
func instrumentedAppKeeping(t *testing.T, path string) (*Application, *telemetry.Telemetry) {
	t.Helper()
	opts := telemetry.DefaultOptions()
	opts.Stdout = io.Discard
	opts.Runtime = false
	opts.SampleEvery = time.Hour
	opts.Path = path
	tel, err := telemetry.Setup(context.Background(), opts)
	require.NoError(t, err)
	t.Cleanup(func() { _ = tel.Shutdown(context.Background()) })

	t.Setenv("BUDGE_BOLT_PATH", t.TempDir())
	store, err := database.NewStore()
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	app, err := NewApplication(store, nil, tel)
	require.NoError(t, err)
	return app, tel
}

func TestRequestsAreTracedAndTimed(t *testing.T) {
	app, tel := instrumentedApp(t)
	app.http.GET("/boom", func(*echo.Context) error { return echo.NewHTTPError(http.StatusInternalServerError, "it broke") })
	app.http.GET("/panic", func(*echo.Context) error { panic("oh no") })

	serve := func(method, target string) int {
		rec := httptest.NewRecorder()
		app.http.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
		return rec.Code
	}
	assert.Equal(t, http.StatusOK, serve(http.MethodGet, "/4XX"))
	assert.Equal(t, http.StatusInternalServerError, serve(http.MethodGet, "/boom"))
	assert.Equal(t, http.StatusInternalServerError, serve(http.MethodGet, "/panic"), "a panic is recovered and answered")
	serve(http.MethodGet, "/static/styles.css")
	serve(http.MethodGet, "/notifications/badge")

	requests := tel.Spans.Requests()
	var routes []string
	for _, span := range requests {
		routes = append(routes, span.Attrs["http.route"])
	}
	assert.ElementsMatch(t, []string{"/4XX", "/boom", "/panic"}, routes, "static files and background polls are not traced")

	byRoute := map[string]telemetry.Span{}
	for _, span := range requests {
		byRoute[span.Attrs["http.route"]] = span
	}
	assert.True(t, byRoute["/boom"].Error, "a server error marks its request failed")
	assert.False(t, byRoute["/4XX"].Error)

	errs := tel.Logs.Recent(otellog.SeverityError)
	require.NotEmpty(t, errs)
	var messages, traces []string
	for _, record := range errs {
		messages = append(messages, record.Message)
		if record.Message == "request failed" {
			traces = append(traces, record.TraceID)
		}
	}
	assert.Contains(t, messages, "request failed")
	assert.Contains(t, messages, "panic")
	assert.Contains(t, traces, byRoute["/boom"].TraceID, "an error log names its request's trace")

	require.NoError(t, tel.Metrics.Collect(context.Background()))
	durations := tel.Metrics.Histograms("http.server.request.duration")
	require.NotEmpty(t, durations, "request durations are measured, metrics: %v", tel.Metrics.Names())
	var timed []string
	for _, h := range durations {
		timed = append(timed, h.Attrs["http.route"])
	}
	assert.Contains(t, timed, "/4XX")
}

func TestQueriesSitUnderTheirRequest(t *testing.T) {
	app, tel := instrumentedApp(t)
	rec := httptest.NewRecorder()
	app.http.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/notifications", nil))
	require.Equal(t, http.StatusOK, rec.Code)

	requests := tel.Spans.Requests()
	require.Len(t, requests, 1)
	request := requests[0]

	var queries []telemetry.Span
	for _, span := range tel.Spans.Trace(request.TraceID) {
		if span.Attrs["db.system.name"] == "bbolt" {
			queries = append(queries, span)
		}
	}
	require.NotEmpty(t, queries, "the notifications are read within the request's trace")
	query := queries[0]
	assert.Equal(t, request.SpanID, query.ParentID, "the query is a child of the request")
	assert.Equal(t, "ReadNotifications", query.Attrs["db.operation.name"], "named by the Store method, not the generic helper")
	assert.Equal(t, "notifications", query.Attrs["db.collection.name"])
	assert.Equal(t, "view", query.Attrs["bbolt.transaction"])
	assert.Equal(t, "application.(*Application).sortedNotifications", query.Attrs["code.function.name"], "the caller is the code outside the store")
	assert.Contains(t, query.Attrs["code.file.path"], "notification.go")

	require.NoError(t, tel.Metrics.Collect(context.Background()))
	var timed []string
	for _, h := range tel.Metrics.Histograms("db.client.operation.duration") {
		timed = append(timed, h.Attrs["db.operation.name"])
	}
	assert.Contains(t, timed, "ReadNotifications")
}

func TestAdminPageShowsWhatWasRecorded(t *testing.T) {
	app, tel := instrumentedApp(t)
	app.http.GET("/boom", func(*echo.Context) error { return echo.NewHTTPError(http.StatusInternalServerError, "it broke") })
	get := func(target string, htmx bool) (int, string) {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodGet, target, nil)
		if htmx {
			req.Header.Set("HX-Request", "true")
		}
		app.http.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	get("/notifications", false)
	get("/boom", false)
	require.NoError(t, tel.Metrics.Collect(context.Background()))

	for tab, want := range map[string][]string{
		"overview": {"Requests", "adminCpu", "Kept in memory only", "Runs", "Running", "Storage", "In memory"},
		"requests": {"/notifications", "/boom", "Slowest"},
		"database": {"ReadNotifications", "notifications", "application.(*Application).sortedNotifications", "pkg/application/notification.go:"},
		"logs":     {"request failed", "it broke"},
		"errors":   {"request failed", "GET /boom"},
	} {
		for _, htmx := range []bool{false, true} {
			code, body := get("/admin?tab="+tab, htmx)
			require.Equal(t, http.StatusOK, code, "%s, htmx %v", tab, htmx)
			for _, text := range want {
				assert.Contains(t, body, text, "the %s tab", tab)
			}
		}
	}

	get("/notifications", false)
	require.NoError(t, tel.Metrics.Collect(context.Background()))
	overview := adminOverview(tel, "1h")
	require.Len(t, overview.RequestChart.Points, 1, "two samples make one rate")
	assert.Positive(t, *overview.RequestChart.Points[0].Y, "a request landed between them")
	assert.NotEmpty(t, overview.QueryChart.Points)

	_, body := get("/admin?tab=logs&level=error&q=nothing-matches-this", true)
	assert.NotContains(t, body, "it broke", "a search narrows the logs")

	var notifications telemetry.Span
	for _, span := range tel.Spans.Requests() {
		if span.Attrs["http.route"] == "/notifications" {
			notifications = span
		}
	}
	code, body := get("/admin/traces/"+notifications.TraceID, false)
	require.Equal(t, http.StatusOK, code)
	assert.Contains(t, body, "GET /notifications")
	assert.Contains(t, body, "ReadNotifications notifications", "the query sits in its request's waterfall")

	code, _ = get("/admin/traces/0123456789abcdef0123456789abcdef", false)
	assert.Equal(t, http.StatusTemporaryRedirect, code, "a trace no longer kept is not found, like any missing page")
}

func TestAdminCompactsTheTelemetryFile(t *testing.T) {
	app, tel := instrumentedAppKeeping(t, filepath.Join(t.TempDir(), "telemetry.db"))
	rec := httptest.NewRecorder()
	app.http.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/notifications", nil))

	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/admin/compact", nil)
	req.Header.Set("HX-Request", "true")
	app.http.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)
	assert.Contains(t, rec.Body.String(), "gave back", "the card shows what the compaction did")
	assert.Contains(t, rec.Header().Get("HX-Trigger"), "Compacted")
	assert.False(t, tel.Storage().Compacted.At.IsZero())
}

func TestAPanicIsRecordedWithItsStack(t *testing.T) {
	app, tel := instrumentedApp(t)
	app.http.GET("/boom", func(*echo.Context) error { return echo.NewHTTPError(http.StatusInternalServerError, "it broke") })
	app.http.GET("/panic", func(*echo.Context) error { panicHere(); return nil })
	for _, target := range []string{"/boom", "/panic"} {
		app.http.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, target, nil))
	}

	byRoute := map[string]telemetry.Span{}
	for _, span := range tel.Spans.Requests() {
		byRoute[span.Attrs["http.route"]] = span
	}

	panicked := byRoute["/panic"].Exceptions()
	require.Len(t, panicked, 1, "recorded once, as it was recovered, and not again by the error handler")
	assert.Equal(t, "application.recovered", panicked[0].Type)
	assert.Contains(t, panicked[0].Message, "oh no")
	assert.Contains(t, panicked[0].Stack, "panicHere", "the stack reaches where it panicked")

	var handled int
	for _, record := range tel.Logs.Recent(otellog.SeverityError) {
		if record.Message == "request failed" && record.Attrs["uri"] == "/boom" {
			handled++
		}
	}
	assert.Equal(t, 1, handled, "the error is handled inside the span, and only there")

	failed := byRoute["/boom"].Exceptions()
	require.Len(t, failed, 1)
	assert.Contains(t, failed[0].Message, "it broke")
	assert.Empty(t, failed[0].Stack, "a returned error carries no stack worth keeping")

	rec := httptest.NewRecorder()
	app.http.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/admin/traces/"+byRoute["/panic"].TraceID, nil))
	assert.Contains(t, rec.Body.String(), "Stack trace")
	assert.Contains(t, rec.Body.String(), "panicHere")
}

func panicHere() { panic("oh no") }

func TestWithoutTelemetryThereIsNoAdmin(t *testing.T) {
	t.Setenv("BUDGE_BOLT_PATH", t.TempDir())
	store, err := database.NewStore()
	require.NoError(t, err)
	t.Cleanup(func() { _ = store.Close() })
	without, err := NewApplication(store, nil, nil)
	require.NoError(t, err)
	with, _ := instrumentedApp(t)

	get := func(app *Application, target string) (int, string) {
		rec := httptest.NewRecorder()
		app.http.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, target, nil))
		return rec.Code, rec.Body.String()
	}

	code, _ := get(without, "/admin")
	assert.Equal(t, http.StatusTemporaryRedirect, code, "the page is not there, like any missing page")
	_, settings := get(without, "/settings")
	assert.NotContains(t, settings, `href="/admin"`, "neither the icon nor the settings card links to it")

	code, _ = get(with, "/admin")
	assert.Equal(t, http.StatusOK, code)
	_, settings = get(with, "/settings")
	assert.Equal(t, 2, strings.Count(settings, `href="/admin"`), "the icon and the settings card")
}

func TestEchosOwnErrorsKeepTheirStatus(t *testing.T) {
	app, _ := instrumentedApp(t)
	serve := func(method, target string) *httptest.ResponseRecorder {
		rec := httptest.NewRecorder()
		app.http.ServeHTTP(rec, httptest.NewRequest(method, target, nil))
		return rec
	}

	missing := serve(http.MethodGet, "/no-such-page")
	assert.Equal(t, http.StatusTemporaryRedirect, missing.Code, "an unknown page is not found, not a server error")
	assert.Equal(t, "/4XX", missing.Header().Get("Location"))
	assert.Empty(t, missing.Header().Get("Hx-Trigger"), "a redirect is the whole answer")
	assert.NotContains(t, missing.Body.String(), `"Not Found"`, "nothing is written after the redirect")

	req := httptest.NewRequest(http.MethodGet, "/no-such-page", nil)
	req.Header.Set("HX-Request", "true")
	htmx := httptest.NewRecorder()
	app.http.ServeHTTP(htmx, req)
	assert.Equal(t, http.StatusNotFound, htmx.Code, "htmx is told, not redirected")
	assert.Empty(t, htmx.Header().Get("Location"))
	assert.Contains(t, htmx.Header().Get("Hx-Trigger"), `"toast"`)
	assert.Equal(t, "none", htmx.Header().Get("Hx-Reswap"))

	wrongMethod := serve(http.MethodPut, "/settings")
	assert.Equal(t, http.StatusMethodNotAllowed, wrongMethod.Code)
}
