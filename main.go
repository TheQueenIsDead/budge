package main

import (
	"github.com/labstack/echo/v4"
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

func Budget(c echo.Context) error {

	var budgetItems []BudgetItem
	Database.Find(&budgetItems)
	return c.Render(http.StatusOK, "budget", budgetItems)
}

func main() {

	// github.com/mattn/go-sqlite3
	db, err := gorm.Open(sqlite.Open("budge.db"), &gorm.Config{})
	Database = db

	err = db.AutoMigrate(
		&BudgetItem{},
	)

	budgetItems := []BudgetItem{
		{
			Name:      "Car",
			Cost:      50,
			Frequency: Weekly,
		},
		{
			Name:      "Insurance",
			Cost:      1300,
			Frequency: Yearly,
		},
	}

	db.Create(&budgetItems)

	t := &Template{
		templates: template.Must(template.ParseGlob("public/views/*.html")),
	}

	e := echo.New()
	e.Renderer = t
	e.GET("/", func(c echo.Context) error {
		return c.Redirect(http.StatusTemporaryRedirect, "/budget")
	})
	e.GET("/budget", Budget)

	err = e.Start(":8080")
	if err != nil {
		panic(err)
	}
}
