package main

import (
	"github.com/TheQueenIsDead/budge/pkg"
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

type BudgetFrequency string

const (
	Weekly  BudgetFrequency = "w"
	Monthly BudgetFrequency = "m"
	Yearly  BudgetFrequency = "y"
)

type BudgetItem struct {
	gorm.Model

	Name      string          `goorm:"name"`
	Cost      float64         `goorm:"cost"`
	Frequency BudgetFrequency `goorm:"frequency"`
}

func Index(c echo.Context) error {
	return c.Render(http.StatusOK, "index", nil)
}

func Budget(c echo.Context) error {

	var budgetItems []BudgetItem
	Database.Find(&budgetItems)
	return c.Render(http.StatusOK, "budget", budgetItems)
}

func main() {

	// github.com/mattn/go-sqlite3
	db, err := gorm.Open(sqlite.Open("budge.db"), &gorm.Config{})
	if err != nil {
		panic(err)
	}
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
		templates: template.Must(template.ParseGlob("public/views/*.gohtml")),
	}

	e := echo.New()
	e.Renderer = t
	e.GET("/", Index)
	e.GET("/budget", Budget)
	e.POST("/upload", pkg.Upload)

	err = e.Start(":1337")
	if err != nil {
		panic(err)
	}
}
