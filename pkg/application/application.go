package application

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"sync"
	"time"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/integrations"
	"github.com/TheQueenIsDead/budge/pkg/telemetry"
	"github.com/TheQueenIsDead/budge/web"
	"github.com/dustin/go-humanize"
	echootel "github.com/labstack/echo-otel/v5"
	"github.com/labstack/echo/v5"
	"github.com/labstack/echo/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
	"go.opentelemetry.io/otel/trace"
	"golang.org/x/text/cases"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// templateFuncs are the formatting helpers made available to every template.
// It is split out of NewApplication so tests can render templates without a
// live store.
func templateFuncs() template.FuncMap {
	return template.FuncMap{
		// The sign belongs in front of the symbol: "-$40.51", not "$-40.51".
		"fmtCurrency": func(number float64) string {
			p := message.NewPrinter(language.English)
			if number < 0 {
				return p.Sprintf("-$%.2f", math.Abs(number))
			}
			return p.Sprintf("$%.2f", number)
		},
		// fmtCurrencyAbs drops the sign, for places where an adjacent label
		// ("deficit", "over-allocated") already carries the direction.
		// fmtWhole rounds to whole dollars, for figures read at a glance where
		// cents are noise, such as targets and their averages.
		"fmtWhole": fmtWhole,
		// fmtWholeAbs is fmtWhole without the sign, for where the words say
		// which way the figure goes.
		"fmtWholeAbs": func(number float64) string {
			p := message.NewPrinter(language.English)
			return p.Sprintf("$%.0f", math.Abs(number))
		},
		"fmtCurrencyAbs": func(number float64) string {
			p := message.NewPrinter(language.English)
			return p.Sprintf("$%.2f", math.Abs(number))
		},
		"fmtPercent": func(number float64) string {
			p := message.NewPrinter(language.English)
			return p.Sprintf("%.1f%%", math.Abs(number)*100)
		},
		// pctOf expresses a value as a percentage of a maximum, for the share
		// bars behind ranked lists. A zero maximum yields zero rather than NaN.
		"pctOf": func(value, max float64) float64 {
			if max == 0 {
				return 0
			}
			return math.Abs(value) / math.Abs(max) * 100
		},
		"fmtRelative": func(date time.Time) string {
			return humanize.RelTime(date, time.Now(), "ago", "")
		},
		// fmtUntil words a time ahead of now. fmtRelative leaves an empty suffix
		// for future dates, which reads as "6 days ." in the middle of a sentence.
		"fmtUntil": func(date time.Time) string {
			if !date.After(time.Now()) {
				return "shortly"
			}
			return "in " + strings.TrimSpace(humanize.RelTime(time.Now(), date, "", ""))
		},
		// fmtDuration words a timing at the precision it deserves: a bolt read
		// in microseconds, a request in milliseconds, a sync in seconds.
		"fmtDuration": fmtDuration,
		"fmtBytes": func(bytes float64) string {
			return humanize.IBytes(uint64(max(0, bytes)))
		},
		"fmtBytes64": func(bytes int64) string {
			return humanize.IBytes(uint64(max(0, bytes)))
		},
		"fmtLower": func(text string) string {
			return strings.ToLower(text)
		},
		"fmtTitle": func(text string) string {
			return cases.Title(language.English).String(text)
		},
		"asset": assetURL,
		// nonce marks a script as the page's own, so the Content Security
		// Policy lets it run. Each request binds its own; outside one there is
		// none.
		"nonce": func() string { return "" },
		// dict builds a map from alternating keys and values, so a partial
		// template can be handed more than one thing.
		"dict": func(pairs ...interface{}) map[string]interface{} {
			values := make(map[string]interface{}, len(pairs)/2)
			for i := 0; i+1 < len(pairs); i += 2 {
				if key, ok := pairs[i].(string); ok {
					values[key] = pairs[i+1]
				}
			}
			return values
		},
		// toJSON encodes a value for an hx-vals attribute. Writing the JSON out
		// by hand in the markup breaks on the first merchant with a quote in
		// its name.
		"toJSON": func(value interface{}) (string, error) {
			encoded, err := json.Marshal(value)
			return string(encoded), err
		},
	}
}

// tracer starts the spans budge makes itself, for work that is not a request.
// It is fetched each time, so it follows telemetry being set up again.
func tracer() trace.Tracer {
	return otel.Tracer("github.com/TheQueenIsDead/budge/pkg/application")
}

type Application struct {
	http         *echo.Echo
	server       *http.Server
	store        *database.Store
	integrations *integrations.Integrations
	scheduler    *scheduler
	akahuSync    syncRunner
	// background tracks the syncs started from the settings page, which
	// outlive their requests, so Close can wait for them.
	background sync.WaitGroup
	telemetry  *telemetry.Telemetry
}

// trustedOrigins are the origins, other than its own, that budge takes
// changes from; see CrossOriginProtection.
func NewApplication(store *database.Store, integrations *integrations.Integrations, tel *telemetry.Telemetry, trustedOrigins []string) (*Application, error) {

	app := new(Application)

	app.integrations = integrations
	app.telemetry = tel

	app.store = store
	app.http = echo.New()

	tpl := template.New("").Funcs(templateFuncs())
	tpl = template.Must(tpl.ParseFS(web.Templates, "templates/*.gohtml"))

	t, err := newTemplate(tpl, tel != nil)
	if err != nil {
		return nil, err
	}

	// Echo's own logging goes to slog with the rest, so it is kept and
	// exported too.
	app.http.Logger = slog.Default()

	app.http.HTTPErrorHandler = func(c *echo.Context, err error) {
		// Handled already, inside the request's span: Echo hands the error
		// over again once the middleware has returned.
		if res, _ := echo.UnwrapResponse(c.Response()); res != nil && res.Committed {
			return
		}

		// The status the error carries: an HTTPError's, or one of Echo's own,
		// such as its not found; anything else is a server error.
		code := http.StatusInternalServerError
		var coded echo.HTTPStatusCoder
		if errors.As(err, &coded) && coded.StatusCode() != 0 {
			code = coded.StatusCode()
		}

		// Logged against the request, so the log names its trace, and the
		// request's span marked failed for anything that is not the
		// visitor's mistake. A recovered panic carries the stack it came from,
		// which goes on the span and in the log, but never to the visitor.
		ctx := c.Request().Context()
		span := trace.SpanFromContext(ctx)
		msg := err.Error()
		level := slog.LevelWarn
		var panicked *middleware.PanicStackError
		switch {
		case errors.As(err, &panicked):
			level = slog.LevelError
			recovered := recovered{panicked.Err}
			msg = recovered.Error()
			slog.ErrorContext(ctx, "panic", "error", panicked.Err, "stack", string(panicked.Stack))
			span.RecordError(recovered, trace.WithAttributes(semconv.ExceptionStacktrace(string(panicked.Stack))))
			span.SetStatus(codes.Error, msg)
		case code >= http.StatusInternalServerError:
			level = slog.LevelError
			span.RecordError(err)
			span.SetStatus(codes.Error, msg)
		}
		// The path, not the whole URI: a query can carry a search, a payee or
		// an amount, and the log is kept.
		slog.Log(ctx, level, "request failed", "error", msg, "status", code, "path", c.Request().URL.Path)

		// A page that is not there is sent to the not found page, and that is
		// the whole answer. htmx would swap the not found page into whatever
		// asked, so its requests are told with a toast like any other error.
		if code == http.StatusNotFound && c.Request().Header.Get("HX-Request") == "" {
			if err := c.Redirect(http.StatusTemporaryRedirect, "/4XX"); err != nil {
				slog.ErrorContext(ctx, "could not redirect to the not found page", "error", err)
			}
			return
		}

		// Set an HTMX Error even via headers
		event := map[string]interface{}{
			"toast": map[string]string{
				"level":   "error",
				"message": msg,
			},
		}
		trigger, err := triggerJSON(event)
		if err != nil {
			slog.ErrorContext(ctx, "could not word the error", "error", err)
		}
		c.Response().Header().Add("Hx-Trigger", trigger)
		c.Response().Header().Add("Hx-Reswap", "none")

		// On error, return JSON with the inherited code
		if err := c.JSON(code, msg); err != nil {
			slog.ErrorContext(ctx, "could not answer the error", "error", err)
		}
	}

	app.http.Renderer = t

	// Middlewares. Every request is traced and timed, apart from static files
	// and the polls that run in the background of every page, which would
	// drown out everything else. A panic is recovered, logged and answered.
	// The tracing middleware ends the request's span as the handler returns,
	// before Echo would handle an error, so it is handled here instead, while
	// the span is open to record it.
	app.http.Use(echootel.NewMiddlewareWithConfig(echootel.Config{
		ServerName:  "budge",
		Skipper:     untraced,
		OnNextError: func(c *echo.Context, err error) { c.Echo().HTTPErrorHandler(c, err) },
	}))
	// A panic comes back as an error carrying its stack, for the error
	// handler to record and answer.
	app.http.Use(middleware.Recover())
	app.http.Use(SecurityHeaders())
	crossOrigin, err := CrossOriginProtection(trustedOrigins...)
	if err != nil {
		return nil, fmt.Errorf("BUDGE_TRUSTED_ORIGINS: %w", err)
	}
	app.http.Use(crossOrigin)

	// General
	app.http.GET("/", app.Dashboard)
	app.http.GET("/4XX", app._4XX)

	//// Integrations
	app.http.POST("/integrations/akahu/sync", app.SyncAkahu)
	app.http.GET("/integrations/akahu/sync", app.SyncAkahuStatus)
	app.http.POST("/integrations/akahu/resync", app.ResyncAkahu)
	app.http.POST("/integrations/akahu/save", app.PutAkahuSettings)

	// Notifications
	app.http.GET("/notifications", app.Notifications)
	app.http.GET("/notifications/badge", app.NotificationBadge)
	app.http.POST("/notifications/read", app.NotificationsRead)
	app.http.DELETE("/notifications", app.NotificationsClear)
	app.http.DELETE("/notifications/:id", app.NotificationDelete)

	// Settings
	app.http.GET("/settings", app.Settings)
	// Admin is what telemetry has recorded, so without it there is no page.
	if tel != nil {
		app.http.GET("/admin", app.Admin)
		app.http.GET("/admin/traces/:id", app.AdminTrace)
		app.http.POST("/admin/compact", app.AdminCompact)
	}
	app.http.POST("/settings/schedule", app.SettingsSaveSchedule)
	app.http.POST("/settings/budget-categories", app.SettingsAddBudgetCategory)
	app.http.POST("/settings/budget-categories/:key", app.SettingsUpdateBudgetCategory)
	app.http.DELETE("/settings/budget-categories/:key", app.SettingsDeleteBudgetCategory)
	app.http.POST("/settings/rules", app.SettingsAddRule)
	app.http.GET("/settings/rules/preview", app.SettingsPreviewRule)
	app.http.DELETE("/settings/rules/:id", app.SettingsDeleteRule)
	app.http.POST("/settings/danger/remove/synced", app.SettingsDeleteSynced)
	app.http.GET("/settings/backup", app.SettingsExport)
	app.http.POST("/settings/backup", app.SettingsImport)

	// Insights
	app.http.GET("/insights", app.Insights)

	// Transactions
	app.http.GET("/transactions", app.Transactions)

	// Timeline: the purchases worth remembering, and the memories made of them.
	app.http.GET("/timeline", app.Timeline)
	app.http.POST("/timeline/memories", app.TimelineSaveMemory)
	app.http.PUT("/timeline/memories/:id", app.TimelineUpdateMemory)
	app.http.DELETE("/timeline/memories/:id", app.TimelineDeleteMemory)
	app.http.DELETE("/timeline/memories/:id/transactions/:tx", app.TimelineRemoveFromMemory)
	app.http.POST("/timeline/hide", app.TimelineHide)
	app.http.POST("/timeline/restore", app.TimelineRestore)

	// Adding transactions to a memory from the transactions list, one at a
	// time or every ticked row at once, and taking them back out.
	app.http.POST("/transactions/category", app.TransactionsSetCategory)
	app.http.POST("/transactions/memory", app.TransactionsAddToMemory)
	app.http.DELETE("/transactions/memory", app.TransactionsRemoveFromMemory)
	app.http.POST("/transactions/:id/memory", app.TransactionAddToMemory)
	app.http.DELETE("/transactions/:id/memory", app.TransactionRemoveFromMemory)
	app.http.GET("/transactions/:id/category", app.TransactionCategoryForm)
	app.http.POST("/transactions/:id/category", app.TransactionSetCategory)

	// Portfolio: everything owned and owed. Assets tracked by hand sit under it
	// alongside the synced accounts, so their URLs sit under it too.
	app.http.GET("/portfolio", app.Portfolio)
	app.http.GET("/portfolio/accounts/:id", app.Account)

	// Adding sits beside the portfolio rather than under /assets, so that
	// /portfolio/assets/:id has no static siblings to be confused with.
	app.http.GET("/portfolio/new", app.AssetNew)
	app.http.GET("/portfolio/address-suggest", app.AddressSuggest)

	app.http.POST("/portfolio/assets", app.AssetCreate)
	app.http.GET("/portfolio/assets/:id", app.Asset)
	app.http.DELETE("/portfolio/assets/:id", app.AssetDelete)
	app.http.POST("/portfolio/assets/:id/valuations", app.AssetAddValuation)
	app.http.POST("/portfolio/assets/:id/purchase", app.AssetUpdatePurchase)
	app.http.POST("/portfolio/assets/:id/estimate", app.AssetRefreshEstimate)
	app.http.DELETE("/portfolio/assets/:id/valuations/:valuationId", app.AssetDeleteValuation)

	// Budget
	app.http.GET("/budget", app.Budget)
	app.http.GET("/budget/income", app.BudgetIncome)
	// The targets now live on the budget page itself.
	app.http.GET("/budget/targets", func(c *echo.Context) error { return c.Redirect(http.StatusMovedPermanently, "/budget") })
	app.http.GET("/budget/summary", app.BudgetSummary)
	app.http.POST("/budget/salary", app.BudgetSaveSalary)
	app.http.GET("/budget/cards", app.BudgetCards)
	app.http.GET("/budget/allocation", app.BudgetAllocation)
	app.http.GET("/budget/periods", app.BudgetPeriods)
	app.http.POST("/budget/split", app.BudgetSaveSplit)
	app.http.POST("/budget/savings-goal", app.BudgetSaveSavingsGoal)
	app.http.POST("/budget/targets", app.BudgetSaveTarget)

	app.scheduler = newScheduler(app)

	// Static files, compiled into the binary. Mounted at /static rather than
	// /assets so the assets feature can own /assets/:id without shadowing the
	// stylesheet. The libraries the pages load are under /static/lib.
	static := app.http.Group("/static", StaticCaching())
	static.StaticFS("/lib", libraryFiles)
	static.StaticFS("/", staticFiles)

	return app, nil
}

// recovered is a panic the recover middleware caught, as the error handler
// records it. It names the exception's type, so a panic reads as one.
type recovered struct{ error }

func (r recovered) Error() string { return "panic: " + r.error.Error() }

func (r recovered) Unwrap() error { return r.error }

func fmtDuration(d time.Duration) string {
	switch {
	case d < time.Millisecond:
		return fmt.Sprintf("%.0fµs", float64(d)/float64(time.Microsecond))
	case d < 10*time.Millisecond:
		return fmt.Sprintf("%.2fms", float64(d)/float64(time.Millisecond))
	case d < time.Second:
		return fmt.Sprintf("%.0fms", float64(d)/float64(time.Millisecond))
	case d < time.Minute:
		return fmt.Sprintf("%.2fs", d.Seconds())
	default:
		return d.Round(time.Second).String()
	}
}

// untraced reports requests not worth a trace of their own: static files,
// and the polls that run in the background of every page.
func untraced(c *echo.Context) bool {
	path := c.Request().URL.Path
	return strings.HasPrefix(path, "/static/") ||
		path == "/notifications/badge" ||
		path == "/integrations/akahu/sync" && c.Request().Method == http.MethodGet
}

// Start serves until Close shuts the server down, when it returns
// http.ErrServerClosed.
func (app *Application) Start() error {
	app.scheduler.start()
	app.server = &http.Server{Addr: ":1337", Handler: app.http}
	slog.Info("http server started", "address", app.server.Addr)
	return app.server.ListenAndServe()
}

// backgroundGrace is how long Close waits for syncs started from the
// settings page to finish.
var backgroundGrace = 20 * time.Second

// Close stops everything that could write to the store, so the store can be
// closed after it.
func (app *Application) Close() error {
	// Stop the scheduler first, cancelling any job in flight and waiting for
	// it to return; see scheduler.stop.
	if app.scheduler != nil {
		app.scheduler.stop()
	}
	// Requests under way are let finish, for a while, before the server
	// closes. Once it has, nothing can start another sync.
	var err error
	if app.server != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err = app.server.Shutdown(ctx)
	}
	// A sync started from the settings page is not tied to its request, so
	// it is waited for here, before the store closes under it. Not forever:
	// one stuck on Akahu should not hold the process open. Past the wait,
	// bolt lets a write already under way finish before it closes, and any
	// after that fails rather than being half written.
	if !app.waitForBackground(backgroundGrace) {
		slog.Warn("stopping with a sync still running", "waited", backgroundGrace)
	}
	return err
}

type Template struct {
	// templates are never executed themselves, only clones of them, as
	// html/template cannot clone a set once it has run. See forRequest.
	templates *template.Template
	// fragments answer htmx's requests, with no nonce bound: htmx gives the
	// scripts it swaps in the page's own nonce, so a fragment needs none, and
	// cloning for every badge poll would be a waste.
	fragments *template.Template
	// admin is whether the admin page is there to link to.
	admin bool
}

func newTemplate(templates *template.Template, admin bool) (*Template, error) {
	fragments, err := templates.Clone()
	if err != nil {
		return nil, err
	}
	return &Template{templates: templates, fragments: fragments, admin: admin}, nil
}

// forRequest is the templates to answer a request with: for a whole page,
// cloned with the request's Content Security Policy nonce bound, so the
// page's own scripts carry it.
func (t *Template) forRequest(c *echo.Context) (*template.Template, error) {
	if c.Request().Header.Get("HX-Request") != "" && t.fragments != nil {
		return t.fragments, nil
	}
	nonce, _ := c.Get(cspNonceKey).(string)
	clone, err := t.templates.Clone()
	if err != nil {
		return nil, err
	}
	return clone.Funcs(template.FuncMap{"nonce": func() string { return nonce }}), nil
}

func renderPartial(templates *template.Template, name string, data interface{}) (string, error) {
	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	err := templates.ExecuteTemplate(bw, name, data)
	if err != nil {
		return "", err
	}
	err = bw.Flush()
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (t *Template) Render(c *echo.Context, w io.Writer, name string, data any) error {

	var found bool
	for _, t := range t.templates.Templates() {
		if t.Name() == name {
			found = true
			break
		}
	}

	if !found {
		return echo.NewHTTPError(http.StatusNotFound, "could not find template")
	}

	templates, err := t.forRequest(c)
	if err != nil {
		return err
	}

	// If the request was initiated by HTMX, return a standalone partial
	if hx := c.Request().Header.Get("HX-Request"); hx != "" {
		return templates.ExecuteTemplate(w, name, data)
	}

	// Else, render a full template within the context of the main HTML layout
	partial, err := renderPartial(templates, name, data)
	if err != nil {
		slog.ErrorContext(c.Request().Context(), "request failed", "error", err)
		return err
	}

	return templates.ExecuteTemplate(w, "layout", map[string]interface{}{
		"content": template.HTML(partial),
		"admin":   t.admin,
	})

}

// lookupError answers a lookup by id that failed. Nothing under the id, as
// from a bad link or one to something since removed, is not found, with the
// message given; anything else is the store failing, a server error.
func lookupError(err error, message string) error {
	if errors.Is(err, database.ErrNotFound) {
		return echo.NewHTTPError(http.StatusNotFound, message)
	}
	return err
}

func (app *Application) Toast(c *echo.Context, level string, message string) {
	// Set an HTMX Error even via headers
	event := map[string]interface{}{
		"toast": map[string]string{
			"level":   level,
			"message": message,
		},
	}
	trigger, err := triggerJSON(event)
	if err != nil {
		slog.ErrorContext(c.Request().Context(), "request failed", "error", err)
		return
	}
	c.Response().Header().Add("Hx-Trigger", trigger)
}

// triggerJSON writes an HX-Trigger event as JSON in ASCII alone. A header's
// bytes are read by the browser as Latin-1, so text such as the quotes
// around a memory's name, or a merchant's é, would arrive garbled. Escaped
// as \u, it is plain ASCII, and htmx's JSON.parse reads back the same text.
// Outside strings JSON has only ASCII, so only string contents change.
func triggerJSON(event any) (string, error) {
	buf, err := json.Marshal(event)
	if err != nil {
		return "", err
	}
	var b strings.Builder
	for _, r := range string(buf) {
		switch {
		case r < utf8.RuneSelf:
			b.WriteRune(r)
		case r > 0xFFFF:
			high, low := utf16.EncodeRune(r)
			fmt.Fprintf(&b, `\u%04x\u%04x`, high, low)
		default:
			fmt.Fprintf(&b, `\u%04x`, r)
		}
	}
	return b.String(), nil
}

// fmtWhole rounds to whole dollars, for figures read at a glance where cents
// are noise, such as targets and their averages.
func fmtWhole(number float64) string {
	p := message.NewPrinter(language.English)
	if number < 0 {
		return p.Sprintf("-$%.0f", math.Abs(number))
	}
	return p.Sprintf("$%.0f", number)
}
