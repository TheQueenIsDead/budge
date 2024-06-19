package main

import (
	"bufio"
	"bytes"
	"github.com/TheQueenIsDead/budge/pkg"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"html/template"
	"io"
	"net/http"
)

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {

	var found bool
	for _, t := range t.templates.Templates() {
		if t.Name() == name {
			c.Logger().Debug("template found for ", name)
			found = true
		}
	}

	if !found {
		c.Logger().Error("could not find template for", name)
		return echo.NewHTTPError(http.StatusNotFound, "could not find template for")
	}

	var buf bytes.Buffer
	bw := bufio.NewWriter(&buf)
	err := t.templates.ExecuteTemplate(bw, name, data)
	if err != nil {
		c.Logger().Error(err)
		return err
	}
	err = bw.Flush()
	if err != nil {
		return err
	}

	content := map[string]interface{}{
		"content": template.HTML(buf.String()),
	}

	return t.templates.ExecuteTemplate(w, "layout", content)
}

func main() {

	// Setup application container
	app := pkg.Application{}

	db, err := gorm.Open(sqlite.Open("budge.db"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	app.DB = db

	e := echo.New()
	app.HTTP = e

	// Setup DB tables and data
	err = app.DB.AutoMigrate(
		&pkg.BudgetItem{},
		&pkg.Merchant{},
	)

	budgetItems := []pkg.BudgetItem{
		{
			Name:      "Car",
			Cost:      50,
			Frequency: pkg.Weekly,
		},
		{
			Name:      "Insurance",
			Cost:      1300,
			Frequency: pkg.Yearly,
		},
	}

	app.DB.Create(&budgetItems)

	// Setup HTTP server
	t := &Template{
		templates: template.Must(template.ParseGlob("web/templates/*.gohtml")),
	}

	e.Logger.SetLevel(log.DEBUG)

	e.Renderer = t
	e.GET("/", app.Index)
	e.GET("/budget", app.Budget)
	e.GET("/merchant", app.Merchant)
	e.POST("/upload", app.Upload)
	e.GET("/layout", app.Layout)

	e.Static("/assets", "./web/public")

	err = app.HTTP.Start(":1337")
	if err != nil {
		panic(err)
	}
}
