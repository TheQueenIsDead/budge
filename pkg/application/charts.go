package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

type TimeseriesData struct {
	ChartId    string
	Title      string
	Labels     []string
	Data       []float64
	Border     []string
	Background []string
}

//func (app *Application) ChartTimeseries(c echo.Context) error {
//
//}

type DoughnutData struct {
	ChartId string
	Title   string
	Labels  []string
	Data    []float64
}

func (app *Application) ChartDoughnut(c echo.Context) error {

	var err error

	var transactions []models.Transaction
	transactions, err = app.store.ReadTransactions()
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	categories := make(map[string]float64)
	for _, transaction := range transactions {
		category := transaction.Category.Groups.PersonalFinance.Name
		if category != "" {
			categories[category] += transaction.Amount
		}
	}

	var categoryLabels []string
	var categoryData []float64
	for k, v := range categories {
		categoryLabels = append(categoryLabels, k)
		categoryData = append(categoryData, v)
	}

	return c.Render(200, "chart.doughnut", DoughnutData{
		ChartId: "doughnut_chart",
		Title:   "Spend By Category",
		Labels:  categoryLabels,
		Data:    categoryData,
	})
}

type GaugeData struct {
	ChartId    string
	Title      string
	Labels     []string
	Data       []float64
	Background []string
}

func (app *Application) ChartGauge(c echo.Context) error {

	var err error

	var transactions []models.Transaction
	transactions, err = app.store.ReadTransactions()
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	categories := make(map[string]float64)
	var in, out float64
	for _, transaction := range transactions {
		if transaction.Amount < 0 {
			out += transaction.Float()
			category := transaction.Category.Groups.PersonalFinance.Name
			if category != "" {
				categories[category] += transaction.Amount
			}
		} else {
			in += transaction.Float()
		}
	}

	return c.Render(200, "chart.gauge", GaugeData{
		ChartId: "gauge_chart",
		Title:   "Incoming vs Outgoing",
		Labels:  []string{"Incoming", "Outgoing"},
		Data:    []float64{in, out},
		Background: []string{
			"rgb(26, 188, 156)",
			"rgb(255, 205, 52)",
		},
	})
}
