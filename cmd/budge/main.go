package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/TheQueenIsDead/budge/pkg"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	bolt "go.etcd.io/bbolt"
	"html/template"
	"io"
	"net/http"
	"time"
)

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

func main() {

	// Setup application container
	app := pkg.Application{}

	opts := bolt.DefaultOptions
	opts.Timeout = 5 * time.Second
	db, err := bolt.Open("budge.bolt.db", 0600, opts)
	if err != nil {
		panic(err)
	}
	defer db.Close()
	app.DB = db

	// Setup DB tables and data
	buckets := [][]byte{
		pkg.AccountBucket,
		pkg.MerchantBucket,
		pkg.TransactionBucket,
	}
	for _, bucket := range buckets {
		db.Update(func(tx *bolt.Tx) error {
			_, err := tx.CreateBucketIfNotExists(bucket)
			if err != nil {
				return fmt.Errorf("create bucket: %s", err)
			}
			return nil
		})
	}

	e := echo.New()
	e.Debug = true
	app.HTTP = e

	// Setup HTTP server
	tpl := template.Must(template.ParseGlob("web/templates/*.gohtml"))
	tpl = template.Must(tpl.ParseGlob("web/templates/account/*.gohtml"))
	tpl = template.Must(tpl.ParseGlob("web/templates/merchant/*.gohtml"))
	tpl = template.Must(tpl.ParseGlob("web/templates/transaction/*.gohtml"))
	t := &Template{
		templates: tpl,
	}

	e.Logger.SetLevel(log.DEBUG)

	e.Renderer = t
	e.GET("/", app.Index)
	//e.GET("/budget", app.Budget)
	e.GET("/merchants", app.ListMerchants)
	e.GET("/accounts", app.ListAccounts)
	e.GET("/transactions", app.ListTransactions)
	//e.GET("/merchant/:id/edit", app.EditMerchant)
	//e.PUT("/merchant/:id", app.PutMerchant)
	//e.GET("/merchant/:id", app.GetMerchant)
	e.POST("/upload", app.Upload)
	//e.GET("/layout", app.Layout)

	e.Static("/assets", "./web/public")

	err = app.HTTP.Start(":1337")
	if err != nil {
		panic(err)
	}
}
