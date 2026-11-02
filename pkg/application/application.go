package application

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"math"
	"net/http"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/integrations"
	"github.com/TheQueenIsDead/budge/pkg/telemetry"
	"github.com/TheQueenIsDead/budge/web"
	"github.com/dustin/go-humanize"
	echootel "github.com/labstack/echo-otel/v4"
	"github.com/labstack/echo/v4"
	"github.com/labstack/echo/v4/middleware"
	"github.com/labstack/gommon/log"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
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
		"fmtLower": func(text string) string {
			return strings.ToLower(text)
		},
		"fmtTitle": func(text string) string {
			return cases.Title(language.English).String(text)
		},
		"asset": assetURL,
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
	store        *database.Store
	integrations *integrations.Integrations
	scheduler    *scheduler
	akahuSync    syncRunner
	telemetry    *telemetry.Telemetry
}

func NewApplication(store *database.Store, integrations *integrations.Integrations, tel *telemetry.Telemetry) (*Application, error) {

	app := new(Application)

	app.integrations = integrations
	app.telemetry = tel

	app.store = store
	app.http = echo.New()

	app.http.Debug = true

	tpl := template.New("").Funcs(templateFuncs())
	tpl = template.Must(tpl.ParseFS(web.Templates, "templates/*.gohtml"))

	t := &Template{
		templates: tpl,
	}

	// Echo's own logging, from starting the server and the like, goes to
	// slog with the rest, so it is kept and exported too.
	app.http.Logger.SetLevel(log.INFO)
	app.http.Logger.SetOutput(echoLogWriter{})
	app.http.HideBanner = true

	app.http.HTTPErrorHandler = func(err error, c echo.Context) {
		// Extract the code from the HTTPError
		code := http.StatusInternalServerError
		msg := err.Error()
		if he, ok := err.(*echo.HTTPError); ok {
			code = he.Code
		}

		// Logged against the request, so the log names its trace, and the
		// request's span marked failed for anything that is not the
		// visitor's mistake.
		ctx := c.Request().Context()
		level := slog.LevelWarn
		if code >= http.StatusInternalServerError {
			level = slog.LevelError
			span := trace.SpanFromContext(ctx)
			span.RecordError(err)
			span.SetStatus(codes.Error, msg)
		}
		slog.Log(ctx, level, "request failed", "error", err, "status", code, "uri", c.Request().RequestURI)

		if code == http.StatusNotFound {
			if err := c.Redirect(http.StatusTemporaryRedirect, "/4XX"); err != nil {
				slog.ErrorContext(ctx, "could not redirect to the not found page", "error", err)
			}
		}

		// Set an HTMX Error even via headers
		event := map[string]interface{}{
			"toast": map[string]string{
				"level":   "error",
				"message": msg,
			},
		}
		buf, err := json.Marshal(event)
		if err != nil {
			slog.ErrorContext(ctx, "could not word the error", "error", err)
		}
		c.Response().Header().Add("Hx-Trigger", string(buf))
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
	app.http.Use(echootel.NewMiddlewareWithConfig(echootel.Config{ServerName: "budge", Skipper: untraced}))
	app.http.Use(middleware.RecoverWithConfig(middleware.RecoverConfig{
		LogErrorFunc: func(c echo.Context, err error, stack []byte) error {
			slog.ErrorContext(c.Request().Context(), "panic", "error", err, "stack", string(stack))
			return err
		},
	}))
	app.http.Use(Caching())

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
	app.http.GET("/admin", app.Admin)
	app.http.GET("/admin/traces/:id", app.AdminTrace)
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
	app.http.GET("/budget/targets", func(c echo.Context) error { return c.Redirect(http.StatusMovedPermanently, "/budget") })
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
	// stylesheet.
	app.http.Group("/static", StaticCaching()).StaticFS("/", staticFiles)

	return app, nil
}

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
func untraced(c echo.Context) bool {
	path := c.Request().URL.Path
	return strings.HasPrefix(path, "/static/") ||
		path == "/notifications/badge" ||
		path == "/integrations/akahu/sync" && c.Request().Method == http.MethodGet
}

// echoLogWriter hands echo's own log lines, which it writes as JSON, to
// slog at their level.
type echoLogWriter struct{}

func (echoLogWriter) Write(p []byte) (int, error) {
	var line struct {
		Level   string `json:"level"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(p, &line); err != nil || line.Message == "" {
		slog.Info(strings.TrimSpace(string(p)), "source", "echo")
		return len(p), nil
	}
	level := slog.LevelInfo
	switch line.Level {
	case "WARN":
		level = slog.LevelWarn
	case "ERROR":
		level = slog.LevelError
	case "DEBUG":
		level = slog.LevelDebug
	}
	slog.Log(context.Background(), level, line.Message, "source", "echo")
	return len(p), nil
}

func (app *Application) Start() error {
	app.scheduler.start()
	slog.Info("http server started", "address", ":1337")
	return app.http.Start(":1337")
}

func (app *Application) Close() error {
	// Stop the scheduler first and wait for any job in flight, so shutdown does
	// not cut a sync off midway through writing.
	app.scheduler.stop()
	// TODO: Change this to close down gracefully
	return app.http.Close()
}

type Template struct {
	templates *template.Template
}

func (t *Template) renderPartial(name string, data interface{}) (string, error) {
	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	err := t.templates.ExecuteTemplate(bw, name, data)
	if err != nil {
		return "", err
	}
	err = bw.Flush()
	if err != nil {
		return "", err
	}

	return buf.String(), nil
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {

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

	// If the request was initiated by HTMX, return a standalone partial
	if hx := c.Request().Header.Get("HX-Request"); hx != "" {
		// If the
		return t.templates.ExecuteTemplate(w, name, data)
	}

	// Else, render a full template within the context of the main HTML layout
	partial, err := t.renderPartial(name, data)
	if err != nil {
		slog.ErrorContext(c.Request().Context(), "request failed", "error", err)
		return err
	}

	return t.templates.ExecuteTemplate(w, "layout", map[string]interface{}{
		"content": template.HTML(partial),
	})

}

func (app *Application) Toast(c echo.Context, level string, message string) {
	// Set an HTMX Error even via headers
	event := map[string]interface{}{
		"toast": map[string]string{
			"level":   level,
			"message": message,
		},
	}
	buf, err := json.Marshal(event)
	if err != nil {
		slog.ErrorContext(c.Request().Context(), "request failed", "error", err)
		return
	}
	c.Response().Header().Add("Hx-Trigger", string(buf))
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
