package application

import (
	"net/http"

	"github.com/labstack/echo/v4"
)

// BudgetPageData holds all the data for rendering the budget.gohtml template.
type BudgetPageData struct {
	Income      float64
	Budgeted    float64
	Spent       float64
	Remaining   float64
	BudgetItems []BudgetItem
	SankeyData  []SankeyDataItem
}

// BudgetItem represents a single item in the budget.
type BudgetItem struct {
	Category  string
	Name      string
	Amount    float64 // All amounts should be normalized to a monthly value for display.
	Spent     float64
	Remaining float64
	Period    string // The original period for editing ("week", "month", "year").
}

// SankeyDataItem represents a single flow of money for the Sankey chart.
type SankeyDataItem struct {
	From   string
	To     string
	Weight float64
}

// getFakeBudgetData returns a populated BudgetPageData struct for demonstration.
func getFakeBudgetData() BudgetPageData {
	// --- Raw Data ---
	income := 5000.0

	items := []BudgetItem{
		{Category: "Housing", Name: "Rent", Amount: 1500.0, Spent: 1500.0, Period: "month"},
		{Category: "Food", Name: "Groceries", Amount: 400.0, Spent: 475.50, Period: "month"},
		{Category: "Food", Name: "Restaurants", Amount: 200.0, Spent: 150.25, Period: "month"},
		{Category: "Utilities", Name: "Internet", Amount: 80.0, Spent: 80.0, Period: "month"},
		{Category: "Transport", Name: "Car Payment", Amount: 350.0, Spent: 350.0, Period: "month"},
		{Category: "Transport", Name: "Gas", Amount: 150.0, Spent: 125.70, Period: "week"}, // Example of a weekly item
		{Category: "Entertainment", Name: "Streaming Services", Amount: 45.0, Spent: 45.0, Period: "month"},
		{Category: "Savings", Name: "Emergency Fund", Amount: 1000.0, Spent: 1000.0, Period: "month"},
	}

	// --- Calculated Totals ---
	var totalBudgeted float64
	var totalSpent float64
	sankeyDataMap := make(map[string]float64)

	for i, item := range items {
		// Normalize weekly amounts to monthly for totals
		monthlyAmount := item.Amount
		switch item.Period {
		case "week":
			monthlyAmount = item.Amount * 52 / 12
		case "year":
			monthlyAmount = monthlyAmount / 12
		}

		items[i].Remaining = monthlyAmount - item.Spent
		totalBudgeted += monthlyAmount
		totalSpent += item.Spent
		sankeyDataMap[item.Category] += monthlyAmount
	}

	// --- Sankey Chart Data ---
	sankeyData := []SankeyDataItem{}
	for category, amount := range sankeyDataMap {
		sankeyData = append(sankeyData, SankeyDataItem{From: "Income", To: category, Weight: amount})
	}
	// Add remaining discretionary spending to the chart
	discretionary := income - totalBudgeted
	if discretionary > 0 {
		sankeyData = append(sankeyData, SankeyDataItem{From: "Income", To: "Discretionary", Weight: discretionary})
	}

	return BudgetPageData{
		Income:      income,
		Budgeted:    totalBudgeted,
		Spent:       totalSpent,
		Remaining:   income - totalSpent, // This is the final cashflow
		BudgetItems: items,
		SankeyData:  sankeyData,
	}
}

func (app *Application) Budget(c echo.Context) error {

	return c.Render(http.StatusOK, "budget", getFakeBudgetData())
}
