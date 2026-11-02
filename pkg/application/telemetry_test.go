package application

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/telemetry"
	"github.com/labstack/echo/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	otellog "go.opentelemetry.io/otel/log"
)

// instrumentedApp is the real application with telemetry set up, as it runs.
func instrumentedApp(t *testing.T) (*Application, *telemetry.Telemetry) {
	t.Helper()
	opts := telemetry.DefaultOptions()
	opts.Stdout = io.Discard
	opts.Runtime = false
	opts.SampleEvery = time.Hour
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
	app.http.GET("/boom", func(echo.Context) error { return echo.NewHTTPError(http.StatusInternalServerError, "it broke") })
	app.http.GET("/panic", func(echo.Context) error { panic("oh no") })

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
