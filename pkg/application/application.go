package application

import (
	"bufio"
	"bytes"
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
	t := &Template{
		templates: tpl,
	}

	app.http.Logger.SetLevel(log.DEBUG)

	app.http.Renderer = t
	app.http.GET("/", app.Home)
	app.http.GET("/settings", app.Settings)
	app.http.GET("/merchants", app.ListMerchants)
	app.http.GET("/merchants/:id", app.GetMerchant)
	app.http.GET("/accounts", app.ListAccounts)
	app.http.GET("/transactions", app.ListTransactions)

	app.http.POST("/integrations/akahu/sync", app.SyncAkahu)

	app.http.Static("/assets", "./web/public")

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
