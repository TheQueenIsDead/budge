package application

import (
	"net/http"
	"strings"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// TransactionCategoryView is the categorise popover on a transaction row.
type TransactionCategoryView struct {
	ID     string
	Layout string // "m" or "d": each row is drawn twice, once per layout
	OOB    bool

	// Filled only when the popover opens; the closed popover carries none of
	// it, so a long transaction list does not repeat the category list.
	Loaded     bool
	Broad      string
	Specific   string
	Source     string // where the current category came from
	Overridden bool
	RuleText   string // what a rule for transactions like this one would match
	Groups     []CategoryOptionGroup
}

// TransactionCategoryUpdate re-renders a row's category badges and resets its
// popover, in both layouts, after the category changes.
type TransactionCategoryUpdate struct {
	ID         string
	Categories []string
	Popovers   []TransactionCategoryView
}

func categorySource(tx models.Transaction) (string, bool) {
	switch {
	case strings.HasPrefix(tx.Category.Id, models.OverrideCategoryPrefix):
		return "you", true
	case strings.HasPrefix(tx.Category.Id, models.RuleCategoryPrefix):
		return "a rule", false
	case tx.Category.Name == "" && tx.Category.Groups.PersonalFinance.Name == "":
		return "", false
	}
	return "Akahu", false
}

// TransactionCategoryForm loads the categorise popover's form.
func (app *Application) TransactionCategoryForm(c echo.Context) error {
	tx, err := app.store.GetTransaction(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Transaction not found.")
	}
	settings, err := app.store.GetCategorySettings()
	if err != nil {
		return err
	}
	txs, err := app.store.ReadTransactions()
	if err != nil {
		return err
	}
	source, overridden := categorySource(tx)
	return c.Render(http.StatusOK, "transactions.category.form", TransactionCategoryView{
		ID:         tx.Id,
		Layout:     c.QueryParam("layout"),
		Loaded:     true,
		Broad:      tx.Category.Groups.PersonalFinance.Name,
		Specific:   tx.Category.Name,
		Source:     source,
		Overridden: overridden,
		RuleText:   strings.TrimSpace(spendMerchant(tx)),
		Groups:     categoryOptionGroups(categoryCatalogue(settings.Categories, txs)),
	})
}

// TransactionSetCategory files a transaction under a category: an override
// for the one transaction, or with scope "like", a rule for every transaction
// with the same text. "clear" removes the override instead.
func (app *Application) TransactionSetCategory(c echo.Context) error {
	tx, err := app.store.GetTransaction(c.Param("id"))
	if err != nil {
		return echo.NewHTTPError(http.StatusNotFound, "Transaction not found.")
	}
	settings, err := app.store.GetCategorySettings()
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
		txs, err := app.store.ReadTransactions()
		if err != nil {
			return err
		}
		target, ok := categoryCatalogue(settings.Categories, txs)[strings.ToLower(c.FormValue("category"))]
		if !ok {
			return echo.NewHTTPError(http.StatusBadRequest, "Pick a category to file it under.")
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
				Broad:            target.Broad,
				Specific:         target.Specific,
				IncludeTransfers: tx.IsTransfer(),
			})
		default:
			settings.Overrides = append(settings.Overrides, models.CategoryOverride{
				TransactionID: tx.Id, Broad: target.Broad, Specific: target.Specific,
			})
		}
	}
	if err := app.store.SaveCategorySettings(settings); err != nil {
		return err
	}

	// A rule can recategorise any number of rows on the page, so it is
	// simplest to draw the page again.
	if scope == "like" {
		app.Toast(c, "Rule added", "Every transaction containing “"+strings.TrimSpace(spendMerchant(tx))+"” is filed there now.")
		c.Response().Header().Set("HX-Refresh", "true")
		return c.NoContent(http.StatusOK)
	}

	updated, err := app.store.GetTransaction(tx.Id)
	if err != nil {
		return err
	}
	update := TransactionCategoryUpdate{ID: tx.Id, Categories: updated.Categories()}
	for _, layout := range []string{"m", "d"} {
		update.Popovers = append(update.Popovers, TransactionCategoryView{ID: tx.Id, Layout: layout, OOB: true})
	}
	return c.Render(http.StatusOK, "transactions.category.updated", update)
}
