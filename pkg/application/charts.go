package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

type TimeseriesData struct {
	ChartId    string
	Title      string
	Labels     []string
	Data       []int
	Border     []string
	Background []string
}

func (app *Application) ChartTimeseries(c echo.Context) error {
	// TODO: Grab actual data over time
	return c.Render(200, "chart.timeseries", TimeseriesData{
		"timeseries_chart",
		"My First Chart",
		[]string{"a", "b", "c", "d", "e", "f", "g"},
		[]int{1, 2, 3, 4, 5, 6, 7},
		[]string{
			"rgba(255, 99, 132, 0.2)",
			"rgba(255, 159, 64, 0.2)",
			"rgba(255, 205, 86, 0.2)",
			"rgba(75, 192, 192, 0.2)",
			"rgba(54, 162, 235, 0.2)",
			"rgba(153, 102, 255, 0.2)",
			"rgba(201, 203, 207, 0.2)",
		},
		[]string{
			"rgb(255, 99, 132)",
			"rgb(255, 159, 64)",
			"rgb(255, 205, 86)",
			"rgb(75, 192, 192)",
			"rgb(54, 162, 235)",
			"rgb(153, 102, 255)",
			"rgb(201, 203, 207)",
		},
	})
}

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
