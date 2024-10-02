package application

import (
	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
	"maps"
	"slices"
	"time"
)

type TimeseriesData struct {
	ChartId    string
	Title      string
	Labels     []string
	Data       []float64
	Border     []string
	Background []string
}

func (app *Application) ChartTimeseries(c echo.Context) error {

	period := c.QueryParam("period")
	var periodDays = 7
	switch period {
	case "week":
		periodDays = 7
	case "month":
		periodDays = 30
	case "quarter":
		periodDays = 31 * 4
	}
	queryStart := time.Now().AddDate(0, 0, -1*periodDays)

	var transactions []models.Transaction
	transactions, err := app.store.ReadTransactionsByDate(queryStart, time.Now())
	if err != nil {
		c.Logger().Error(err)
		return err
	}

	// TODO: Size buckets appropriately for the period, days by default
	buckets := map[string]float64{}
	for _, transaction := range transactions {
		//if transaction.Amount < 0 {
		key := transaction.Date.Format(time.DateOnly)
		buckets[key] += transaction.Amount
		//}
	}

	var data []float64
	var labels []string
	var background []string
	keys := slices.Collect(maps.Keys(buckets))
	slices.Sort(keys)
	for _, k := range keys {
		data = append(data, buckets[k])
		labels = append(labels, k)
		if buckets[k] > 0 {
			background = append(background, "rgb(26, 188, 156)")
		} else {
			background = append(background, "rgb(255, 205, 52)")
		}
	}

	return c.Render(200, "chart.timeseries", TimeseriesData{
		ChartId:    "timeseries_chart",
		Title:      "Spend Over Time",
		Labels:     labels,
		Data:       data,
		Border:     background,
		Background: background,
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
