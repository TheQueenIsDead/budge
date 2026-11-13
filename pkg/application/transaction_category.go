package application

import (
	"fmt"
	"net/http"
	"strings"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v5"
)

// TransactionCategoryView is the categorise popover on a transaction row.
type TransactionCategoryView struct {
	ID     string
	Layout string // "m" or "d": each row is drawn twice, once per layout
	OOB    bool

	// Filled only when the popover opens; the closed popover carries none of
	// it, so a long transaction list does not repeat the category list.
	Loaded     bool
	Current    string // where it is filed, as its badges name it
	Source     string // what filed it
	Overridden bool
	RuleText   string // what a rule for transactions like this one would match
	Groups     []CategoryPickerGroup
}

// TransactionCategoryUpdate re-renders a row's category badges and resets its
// popover, in both layouts, after the category changes.
type TransactionCategoryUpdate struct {
	ID         string
	Categories []models.CategoryTag
	Popovers   []TransactionCategoryView
}

// CategoryPickerGroup is one category in a category picker. A category with
// subcategories is a heading over them, with the category itself first for
// anything that fits none of them; one without is a single option.
type CategoryPickerGroup struct {
	Key     string
	Label   string
	Options []CategoryPickerOption
}

// CategoryPickerOption is a category or subcategory a transaction or rule
// can be filed under.
type CategoryPickerOption struct {
	Key   string
	Label string
}

// categoryPicker lists every budge category for filing under, in the order
// the budget lists them. A category with subcategories offers itself first,
// as its general bucket.
func categoryPicker() []CategoryPickerGroup {
	var groups []CategoryPickerGroup
	for _, category := range models.TopBudgetCategories() {
		group := CategoryPickerGroup{Key: category.Key, Label: category.Label}
		if subs := models.BudgetSubcategories(category.Key); len(subs) > 0 {
			group.Options = append(group.Options, CategoryPickerOption{Key: category.Key, Label: category.Label + " (general)"})
			for _, sub := range subs {
				group.Options = append(group.Options, CategoryPickerOption{Key: sub.Key, Label: sub.Label})
			}
		}
		groups = append(groups, group)
	}
	return groups
}

// incomePicker offers the one category money coming in is filed under.
func incomePicker() []CategoryPickerGroup {
	income := models.TopBudgetCategory(models.CategoryIncome)
	return []CategoryPickerGroup{{Key: income.Key, Label: income.Label}}
}

// pickerFor is what a transaction can be filed under: Income for money
// coming in, and the spending categories for anything else.
func pickerFor(tx models.Transaction) []CategoryPickerGroup {
	if tx.IsCredit() {
		return incomePicker()
	}
	return categoryPicker()
}

// bulkCategoryPicker is the picker for ticked transactions, which can be
// going either way: the spending categories, then Income.
func bulkCategoryPicker() []CategoryPickerGroup {
	return append(categoryPicker(), incomePicker()...)
}

// fileable reports whether a transaction can go under a category, and if
// not, why. Income is money coming in, and every other category money going
// out.
func fileable(tx models.Transaction, category models.BudgetCategory) (bool, string) {
	switch income := models.TopBudgetCategory(category.Key).Key == models.CategoryIncome; {
	case income && !tx.IsCredit():
		return false, "Only money coming in can be filed as income."
	case !income && tx.IsCredit():
		return false, "Money coming in can only be filed as income."
	}
	return true, ""
}

// categorySource says what filed a transaction, and whether it was the owner.
func categorySource(tx models.Transaction) (string, bool) {
	switch tx.FiledBy {
	case models.FiledByOwner:
		return "you", true
	case models.FiledByRule:
		return "a rule", false
	}
	if tx.BudgetCategory() == models.CategoryUncategorised {
		return "", false
	}
	return "Akahu", false
}

// TransactionCategoryForm loads the categorise popover's form.
func (app *Application) TransactionCategoryForm(c *echo.Context) error {
	ctx := c.Request().Context()
	tx, err := app.store.GetTransaction(ctx, c.Param("id"))
	if err != nil {
		return lookupError(err, "Transaction not found.")
	}
	source, overridden := categorySource(tx)
	return c.Render(http.StatusOK, "transactions.category.form", TransactionCategoryView{
		ID:         tx.Id,
		Layout:     c.QueryParam("layout"),
		Loaded:     true,
		Current:    strings.Join(tx.Categories(), " › "),
		Source:     source,
		Overridden: overridden,
		RuleText:   strings.TrimSpace(spendMerchant(tx)),
		Groups:     pickerFor(tx),
	})
}

// TransactionSetCategory files a transaction under a category: an override
// for the one transaction, or with scope "like", a rule for every transaction
// with the same text. "clear" removes the override instead.
func (app *Application) TransactionSetCategory(c *echo.Context) error {
	ctx := c.Request().Context()
	tx, err := app.store.GetTransaction(ctx, c.Param("id"))
	if err != nil {
		return lookupError(err, "Transaction not found.")
	}
	settings, err := app.store.GetCategorySettings(ctx)
	if err != nil {
		return err
	}

	kept := settings.Overrides[:0]
	for _, o := range settings.Overrides {
		if o.TransactionID != tx.Id {
			kept = append(kept, o)
		}
	}
	settings.Overrides = kept

	scope := c.FormValue("scope")
	if c.FormValue("clear") != "" {
		scope = "clear"
	}
	if scope != "clear" {
		target, ok := models.BudgetCategoryByKey(c.FormValue("category"))
		if !ok {
			return echo.NewHTTPError(http.StatusBadRequest, "Pick a category to file it under.")
		}
		if ok, why := fileable(tx, target); !ok {
			return echo.NewHTTPError(http.StatusBadRequest, why)
		}
		switch scope {
		case "like":
			text := strings.TrimSpace(spendMerchant(tx))
			if text == "" {
				return echo.NewHTTPError(http.StatusBadRequest, "This transaction has no text for a rule to match.")
			}
			settings.Rules = append(settings.Rules, models.CategoryRule{
				ID:               newID(),
				Contains:         text,
				Category:         target.Key,
				IncludeTransfers: tx.IsTransfer(),
			})
		default:
			settings.Overrides = append(settings.Overrides, models.CategoryOverride{
				TransactionID: tx.Id, Category: target.Key,
			})
		}
	}
	if err := app.store.SaveCategorySettings(ctx, settings); err != nil {
		return err
	}

	// A rule can recategorise any number of rows on the page, so it is
	// simplest to draw the page again.
	if scope == "like" {
		app.Toast(c, "Rule added", "Every transaction containing “"+strings.TrimSpace(spendMerchant(tx))+"” is filed there now.")
		c.Response().Header().Set("HX-Refresh", "true")
		return c.NoContent(http.StatusOK)
	}

	updated, err := app.store.GetTransaction(ctx, tx.Id)
	if err != nil {
		return err
	}
	update := TransactionCategoryUpdate{ID: tx.Id, Categories: updated.CategoryTags()}
	for _, layout := range []string{"m", "d"} {
		update.Popovers = append(update.Popovers, TransactionCategoryView{ID: tx.Id, Layout: layout, OOB: true})
	}
	return c.Render(http.StatusOK, "transactions.category.updated", update)
}

// TransactionsSetCategory files every ticked transaction under one category,
// as if each were filed by hand, so it wins over rules and Akahu. The rows it
// changes can be anywhere on the page, and under the uncategorised filter
// they leave it, so the page is drawn again.
func (app *Application) TransactionsSetCategory(c *echo.Context) error {
	ctx := c.Request().Context()
	ids, err := selectedTransactions(c)
	if err != nil {
		return err
	}
	target, ok := models.BudgetCategoryByKey(c.FormValue("category"))
	if !ok {
		return echo.NewHTTPError(http.StatusBadRequest, "Pick a category to file them under.")
	}

	// Ticked transactions can be going either way, so only those the
	// category suits are filed, and the rest are said to be left.
	var filing []string
	var why string
	for _, id := range ids {
		tx, err := app.store.GetTransaction(ctx, id)
		if err != nil {
			return lookupError(err, "A ticked transaction no longer exists.")
		}
		ok, reason := fileable(tx, target)
		if !ok {
			why = reason
			continue
		}
		filing = append(filing, id)
	}
	if len(filing) == 0 {
		return echo.NewHTTPError(http.StatusBadRequest, why)
	}
	skipped := len(ids) - len(filing)
	ids = filing

	settings, err := app.store.GetCategorySettings(ctx)
	if err != nil {
		return err
	}
	ticked := make(map[string]bool, len(ids))
	for _, id := range ids {
		ticked[id] = true
	}
	kept := settings.Overrides[:0]
	for _, o := range settings.Overrides {
		if !ticked[o.TransactionID] {
			kept = append(kept, o)
		}
	}
	settings.Overrides = kept
	for _, id := range ids {
		settings.Overrides = append(settings.Overrides, models.CategoryOverride{TransactionID: id, Category: target.Key})
	}
	if err := app.store.SaveCategorySettings(ctx, settings); err != nil {
		return err
	}

	message := fmt.Sprintf("Filed %d under %s.", len(ids), strings.Join(append([]string{models.TopBudgetCategory(target.Key).Label}, subLabel(target)...), " › "))
	if skipped > 0 {
		message += fmt.Sprintf(" %d left as they were: %s", skipped, why)
	}
	app.Toast(c, "Saved", message)
	c.Response().Header().Set("HX-Refresh", "true")
	return c.NoContent(http.StatusOK)
}

// subLabel is a subcategory's own name, for following its category's.
func subLabel(category models.BudgetCategory) []string {
	if category.IsSub() {
		return []string{category.Label}
	}
	return nil
}
