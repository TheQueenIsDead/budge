package application

import (
	"net/http"
	"strconv"
	"strings"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// BudgetSaveTarget creates or updates the target for a budge category, or
// a subcategory of one that is split, and returns the re-rendered row: the
// category's, which a subcategory's target adds up into.
func (app *Application) BudgetSaveTarget(c echo.Context) error {
	if key, general := strings.CutSuffix(c.FormValue("category"), generalSuffix); general {
		return app.saveGeneralTarget(c, key)
	}
	category, ok := models.BudgetCategoryByKey(c.FormValue("category"))
	if !ok || category.Key != c.FormValue("category") || !category.Budgeted() {
		return c.NoContent(http.StatusBadRequest)
	}
	rowID := sanitizeID(models.TopBudgetCategory(category.Key).Key)
	target, _ := strconv.ParseFloat(c.FormValue("target_amount"), 64)
	frequency := c.FormValue("target_frequency")
	if frequency == "" {
		frequency = "weekly"
	}

	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	if item, saved := budgetTargets(items)[category.Key]; saved {
		item.Amount = target
		item.Frequency = frequency
		if err := app.store.UpdateBudgetItem(item); err != nil {
			return err
		}
		return app.renderTargetRow(c, rowID)
	}
	if err := app.store.CreateBudgetItem(models.BudgetItem{
		ID:             newID(),
		Name:           category.Label,
		Amount:         target,
		Frequency:      frequency,
		BudgetCategory: category.Key,
	}); err != nil {
		return err
	}
	return app.renderTargetRow(c, rowID)
}

// saveGeneralTarget sets the target for what is filed under a split
// category itself, kept beside its own.
func (app *Application) saveGeneralTarget(c echo.Context, key string) error {
	category, ok := models.BudgetCategoryByKey(key)
	if !ok || category.Key != key || category.IsSub() || !category.Budgeted() {
		return c.NoContent(http.StatusBadRequest)
	}
	target, _ := strconv.ParseFloat(c.FormValue("target_amount"), 64)
	frequency := c.FormValue("target_frequency")
	if frequency == "" {
		frequency = "weekly"
	}
	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	item, saved := budgetTargets(items)[key]
	if !saved {
		item = models.BudgetItem{ID: newID(), Name: category.Label, BudgetCategory: key, Frequency: category.Frequency, Split: true}
	}
	item.GeneralAmount = target
	item.GeneralFrequency = frequency
	if saved {
		err = app.store.UpdateBudgetItem(item)
	} else {
		err = app.store.CreateBudgetItem(item)
	}
	if err != nil {
		return err
	}
	return app.renderTargetRow(c, sanitizeID(key))
}

// BudgetSaveSplit budgets a category by its subcategories, or by one target
// again. Both sets of targets are kept, so switching back and forth loses
// neither.
func (app *Application) BudgetSaveSplit(c echo.Context) error {
	category, ok := models.BudgetCategoryByKey(c.FormValue("category"))
	if !ok || category.Key != c.FormValue("category") || category.IsSub() || !category.Budgeted() || len(models.BudgetSubcategories(category.Key)) == 0 {
		return c.NoContent(http.StatusBadRequest)
	}
	split := c.FormValue("split") == "1"

	items, err := app.store.ReadBudgetItems()
	if err != nil {
		return err
	}
	if item, saved := budgetTargets(items)[category.Key]; saved {
		item.Split = split
		if err := app.store.UpdateBudgetItem(item); err != nil {
			return err
		}
	} else if err := app.store.CreateBudgetItem(models.BudgetItem{
		ID:             newID(),
		Name:           category.Label,
		Frequency:      category.Frequency,
		BudgetCategory: category.Key,
		Split:          split,
	}); err != nil {
		return err
	}
	return app.renderTargetRow(c, sanitizeID(category.Key))
}

// TargetRowUpdate answers an edit on the targets page: the edited row, and
// the figures elsewhere on the page that it changes.
type TargetRowUpdate struct {
	Row   CategoryTargetRow
	Setup CategorySetupData
}

// renderTargetRow answers an edit with just the edited row, swapped in place,
// and the totals of the group it sits in. Re-rendering the list instead
// closed groups opened by hand and moved the page under the pointer.
func (app *Application) renderTargetRow(c echo.Context, rowID string) error {
	setup, err := app.buildSetupData()
	if err != nil {
		return err
	}
	setup.Sections = buildTargetSections(setup.Rows, rowID)
	setup.OOB = true
	for i := range setup.Sections {
		setup.Sections[i].OOB = true
	}
	for _, section := range setup.Sections {
		for _, row := range section.Rows {
			if row.RowID == rowID {
				return c.Render(http.StatusOK, "budget.setup.row.update", TargetRowUpdate{Row: row, Setup: setup})
			}
		}
	}
	return echo.NewHTTPError(http.StatusNotFound, "That category is no longer in the budget.")
}

// budgetTargets indexes the targets set against budge categories and
// subcategories by key, with each category's general target beside it. A
// target naming no category, from before budge had its own, is left out, as
// is a deleted category's: the one its transactions went to keeps its own.
func budgetTargets(items []models.BudgetItem) map[string]models.BudgetItem {
	targets := make(map[string]models.BudgetItem, len(items))
	for _, item := range items {
		if category, ok := models.BudgetCategoryByKey(item.BudgetCategory); ok && category.Key == item.BudgetCategory && category.Budgeted() {
			targets[item.BudgetCategory] = item
			if !category.IsSub() {
				targets[generalKey(item.BudgetCategory)] = models.BudgetItem{
					Name:           category.Label + " (general)",
					Amount:         item.GeneralAmount,
					Frequency:      item.GeneralFrequency,
					BudgetCategory: generalKey(item.BudgetCategory),
				}
			}
		}
	}
	return targets
}

// budgetedSplit reports whether a category is budgeted by its subcategories:
// marked split, on budget, and with subcategories to split into. The row, the
// totals and the charts all ask this, so they agree on which targets are in
// force, even for a split category whose subcategories have all been deleted.
func budgetedSplit(category models.BudgetCategory, targets map[string]models.BudgetItem) bool {
	return targets[category.Key].Split && category.Budgeted() && len(models.BudgetSubcategories(category.Key)) > 0
}

// weeklyTargetTotal adds up the targets in force, per week: a category's
// own, or its subcategories' when it is split.
func weeklyTargetTotal(targets map[string]models.BudgetItem) float64 {
	var total float64
	for _, category := range models.TopBudgetCategories() {
		if !budgetedSplit(category, targets) {
			total += weeklyTarget(targets[category.Key])
			continue
		}
		for _, sub := range models.BudgetSubcategories(category.Key) {
			total += weeklyTarget(targets[sub.Key])
		}
		total += weeklyTarget(targets[generalKey(category.Key)])
	}
	return total
}

// weeklyTarget is a target as an amount a week.
func weeklyTarget(item models.BudgetItem) float64 {
	return toWeeklyAmount(item.Amount, item.Frequency)
}

func actualInFrequency(weeklyActual float64, frequency string) float64 {
	switch frequency {
	case "weekly", "":
		return weeklyActual
	case "fortnightly":
		return weeklyActual * 2
	case "monthly":
		return weeklyActual * 52 / 12
	case "quarterly":
		return weeklyActual * 13
	case "yearly":
		return weeklyActual * 52
	}
	return weeklyActual
}

// frequencyPer reads a target's frequency the way the targets page phrases
// it after an amount: "$150 a week".
func frequencyPer(frequency string) string {
	switch frequency {
	case "fortnightly":
		return "a fortnight"
	case "monthly":
		return "a month"
	case "quarterly":
		return "a quarter"
	case "yearly":
		return "a year"
	}
	return "a week"
}

func toWeeklyAmount(amount float64, frequency string) float64 {
	switch frequency {
	case "weekly":
		return amount
	case "fortnightly":
		return amount / 2
	case "monthly":
		return amount * 12 / 52
	case "quarterly":
		return amount * 4 / 52
	case "yearly":
		return amount / 52
	}
	return amount
}
