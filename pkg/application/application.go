package application

import (
	"bufio"
	"bytes"
	"encoding/json"
	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/integrations"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"html/template"
	"io"
	"net/http"
)

type Application struct {
	http         *echo.Echo
	store        *database.Store
	integrations *integrations.Integrations
}

func NewApplication(store *database.Store, integrations *integrations.Integrations) (*Application, error) {

	app := new(Application)

	app.integrations = integrations

	app.store = store
	app.http = echo.New()

	app.http.Debug = true

	// Setup HTTP server
	tpl := template.Must(template.ParseGlob("web/templates/*.gohtml"))
	tpl = template.Must(tpl.ParseGlob("web/templates/partials/*.gohtml"))
	tpl = template.Must(tpl.ParseGlob("web/templates/charts/*.gohtml"))

	t := &Template{
		templates: tpl,
	}

	app.http.Logger.SetLevel(log.INFO)

	app.http.HTTPErrorHandler = func(err error, c echo.Context) {
		// Extract the code from the HTTPError
		code := http.StatusInternalServerError
		message := err.Error()
		if he, ok := err.(*echo.HTTPError); ok {
			code = he.Code
		}
		c.Logger().Error(err)

		switch code {
		case http.StatusNotFound:
			c.Logger().Errorj(log.JSON{
				"err": err.Error(),
				"uri": c.Request().RequestURI,
			})
			err = c.Redirect(http.StatusTemporaryRedirect, "/4XX")
			if err != nil {
				c.Logger().Error(err)
			}
		}

		// Set an HTMX Error even via headers
		event := map[string]interface{}{
			"toast": map[string]string{
				"level":   "error",
				"message": message,
			},
		}
		buf, err := json.Marshal(event)
		if err != nil {
			c.Logger().Error(err)
		}
		c.Response().Header().Add("Hx-Trigger", string(buf))
		c.Response().Header().Add("Hx-Reswap", "none")

		// On error, return JSON with the inherited code
		if err := c.JSON(code, message); err != nil {
			c.Logger().Error(err)
		}
	}

	app.http.Renderer = t

	// General
	app.http.GET("/", app.Home)
	app.http.GET("/4XX", app._4XX)

	// Accounts
	app.http.GET("/accounts", app.ListAccounts)
	app.http.GET("/accounts/charts/balance", app.AccountBalanceGraph)

	//// Integrations
	app.http.POST("/integrations/akahu/sync", app.SyncAkahu)
	app.http.POST("/integrations/akahu/save", app.PutAkahuSettings)
	app.http.GET("/integrations/akahu/accounts", app.ListAkahuAccounts)

	// Inventory
	app.http.GET("/inventory", app.Inventory)
	app.http.GET("/inventory/new", app.InventoryNew)
	app.http.POST("/inventory/new", app.InventoryCreate)
	app.http.POST("/inventory/:id/delete", app.DeleteInventory)

	// Merchants
	app.http.GET("/merchants", app.ListMerchants)
	app.http.GET("/merchants/:id", app.GetMerchant)

	// Settings
	app.http.GET("/settings", app.Settings)
	app.http.POST("/settings/danger/remove/synced", app.SettingsDeleteSynced)

	// Transactions
	app.http.GET("/transactions", app.ListTransactions)

	// Static Assets
	app.http.Static("/assets", "./web/public")

	app.http.GET("/charts/timeseries", app.ChartTimeseries)
	app.http.GET("/charts/doughnut", app.ChartDoughnut)
	app.http.GET("/charts/gauge", app.ChartGauge)

	return app, nil
}

func (app *Application) Start() error {
	return app.http.Start(":1337")
}

func (app *Application) Close() error {
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
	c.Logger().Debug("Rendering template:", t.templates.Templates())
	for _, t := range t.templates.Templates() {
		if t.Name() == name {
			c.Logger().Debug("template found for '", name, "'")
			found = true
			break
		}
	}

	if !found {
		c.Logger().Error("could not find template for '", name, "'")
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
		c.Logger().Error(err)
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
		c.Logger().Error(err)
		return
	}
	c.Response().Header().Add("Hx-Trigger", string(buf))
}
