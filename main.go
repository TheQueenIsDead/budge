package main

import (
	"github.com/TheQueenIsDead/budge/pkg"
	"github.com/labstack/echo/v4"
	"github.com/labstack/gommon/log"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"html/template"
	"io"
	"net/http"
)

var (
	Database *gorm.DB
)

type Template struct {
	templates *template.Template
}

func (t *Template) Render(w io.Writer, name string, data interface{}, c echo.Context) error {
	return t.templates.ExecuteTemplate(w, name, data)
}

func Index(c echo.Context) error {
	return c.Render(http.StatusOK, "index", nil)
}

func Budget(c echo.Context) error {

	var budgetItems []pkg.BudgetItem
	Database.Find(&budgetItems)
	return c.Render(http.StatusOK, "budget", budgetItems)
}

func main() {

	db, err := gorm.Open(sqlite.Open("budge.db"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
	Database = db

	err = db.AutoMigrate(
		&pkg.BudgetItem{},
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

	db.Create(&budgetItems)

	t := &Template{
		templates: template.Must(template.ParseGlob("web/templates/*.gohtml")),
	}

	e := echo.New()

	e.Logger.SetLevel(log.DEBUG)

	e.Renderer = t
	e.GET("/", Index)
	e.GET("/budget", Budget)
	e.POST("/upload", pkg.Upload)

	err = e.Start(":1337")
	if err != nil {
		panic(err)
	}
}
