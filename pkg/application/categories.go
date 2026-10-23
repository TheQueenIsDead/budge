package application

import (
	"net/http"
	"sort"
	"strings"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// CategoryOption is one category a rule can file transactions under.
type CategoryOption struct {
	Broad    string
	Specific string
	Custom   bool
}

// CategoryOptionGroup is the options under one broad category, for an
// <optgroup> in the rule form.
type CategoryOptionGroup struct {
	Broad   string
	Options []CategoryOption
}

// RuleView is a rule with how many transactions it currently files, so a
// rule that matches nothing stands out as a typo.
type RuleView struct {
	models.CategoryRule
	Matches int
}

// CategoriesView is the view-model for the settings.categories partial.
type CategoriesView struct {
	Custom []models.CustomCategory
	Rules  []RuleView
	Groups []CategoryOptionGroup
	Types  []string // transaction types seen, for narrowing a rule
	Error  string
}

// categoryCatalogue lists every category a rule can target: the custom ones
// first, then each pair Akahu has used. Transfers are left out since no rule
// applies to them. Specific names are the key budget items match on, so the
// catalogue is keyed by them, lower-cased.
func categoryCatalogue(custom []models.CustomCategory, txs []models.Transaction) map[string]CategoryOption {
	catalogue := make(map[string]CategoryOption)
	for _, category := range custom {
		catalogue[strings.ToLower(category.Specific)] = CategoryOption{Broad: category.Broad, Specific: category.Specific, Custom: true}
	}
	for _, tx := range txs {
		broad := tx.Category.Groups.PersonalFinance.Name
		if broad == "" || tx.Type == "TRANSFER" {
			continue
		}
		specific := tx.Category.Name
		if specific == "" {
			specific = broad
		}
		if _, seen := catalogue[strings.ToLower(specific)]; !seen {
			catalogue[strings.ToLower(specific)] = CategoryOption{Broad: broad, Specific: specific}
		}
	}
	return catalogue
}

func (app *Application) buildCategoriesView() (CategoriesView, error) {
	settings, err := app.store.GetCategorySettings()
	if err != nil {
		return CategoriesView{}, err
	}
	txs, err := app.store.ReadTransactions()
	if err != nil {
		return CategoriesView{}, err
	}

	view := CategoriesView{Custom: settings.Categories}

	// Rules match on description, merchant and type, none of which a rule
	// changes, so matching the already-categorised copies counts correctly.
	matches := make([]int, len(settings.Rules))
	types := make(map[string]bool)
	for _, tx := range txs {
		if n := settings.MatchingRule(tx); n >= 0 {
			matches[n]++
		}
		if tx.Type != "" && tx.Type != "TRANSFER" {
			types[tx.Type] = true
		}
	}
	for i, rule := range settings.Rules {
		view.Rules = append(view.Rules, RuleView{CategoryRule: rule, Matches: matches[i]})
	}
	for t := range types {
		view.Types = append(view.Types, t)
	}
	sort.Strings(view.Types)

	byBroad := make(map[string][]CategoryOption)
	for _, option := range categoryCatalogue(settings.Categories, txs) {
		byBroad[option.Broad] = append(byBroad[option.Broad], option)
	}
	for broad, options := range byBroad {
		sort.Slice(options, func(i, j int) bool { return options[i].Specific < options[j].Specific })
		view.Groups = append(view.Groups, CategoryOptionGroup{Broad: broad, Options: options})
	}
	sort.Slice(view.Groups, func(i, j int) bool { return view.Groups[i].Broad < view.Groups[j].Broad })
	return view, nil
}

// renderCategories answers every categories action with the whole card, so
// rule counts and the category picker always reflect the change. A message
// explains an action that was refused.
func (app *Application) renderCategories(c echo.Context, message string) error {
	view, err := app.buildCategoriesView()
	if err != nil {
		return err
	}
	view.Error = message
	return c.Render(http.StatusOK, "settings.categories", view)
}

func (app *Application) SettingsAddCategory(c echo.Context) error {
	broad := strings.TrimSpace(c.FormValue("broad"))
	specific := strings.TrimSpace(c.FormValue("specific"))
	if broad == "" || specific == "" {
		return app.renderCategories(c, "A category needs both a group and a name.")
	}

	settings, err := app.store.GetCategorySettings()
	if err != nil {
		return err
	}
	txs, err := app.store.ReadTransactions()
	if err != nil {
		return err
	}
	// Budget targets are keyed by the specific name alone, so two categories
	// sharing one would share a target.
	if existing, taken := categoryCatalogue(settings.Categories, txs)[strings.ToLower(specific)]; taken {
		return app.renderCategories(c, "“"+specific+"” already exists under "+existing.Broad+".")
	}

	settings.Categories = append(settings.Categories, models.CustomCategory{ID: newID(), Broad: broad, Specific: specific})
	if err := app.store.SaveCategorySettings(settings); err != nil {
		return err
	}
	return app.renderCategories(c, "")
}

// SettingsDeleteCategory removes a custom category from the picker. Rules
// carry their own copy of the category, so any already filing under it keep
// working until they are deleted too.
func (app *Application) SettingsDeleteCategory(c echo.Context) error {
	settings, err := app.store.GetCategorySettings()
	if err != nil {
		return err
	}
	id := c.Param("id")
	kept := settings.Categories[:0]
	for _, category := range settings.Categories {
		if category.ID != id {
			kept = append(kept, category)
		}
	}
	settings.Categories = kept
	if err := app.store.SaveCategorySettings(settings); err != nil {
		return err
	}
	return app.renderCategories(c, "")
}

func (app *Application) SettingsAddRule(c echo.Context) error {
	contains := strings.TrimSpace(c.FormValue("contains"))
	if contains == "" {
		return app.renderCategories(c, "A rule needs some text to match.")
	}

	settings, err := app.store.GetCategorySettings()
	if err != nil {
		return err
	}
	txs, err := app.store.ReadTransactions()
	if err != nil {
		return err
	}
	target, ok := categoryCatalogue(settings.Categories, txs)[strings.ToLower(c.FormValue("category"))]
	if !ok {
		return app.renderCategories(c, "Pick a category for the rule to file transactions under.")
	}

	settings.Rules = append(settings.Rules, models.CategoryRule{
		ID:       newID(),
		Contains: contains,
		Type:     c.FormValue("type"),
		Broad:    target.Broad,
		Specific: target.Specific,
	})
	if err := app.store.SaveCategorySettings(settings); err != nil {
		return err
	}
	return app.renderCategories(c, "")
}

func (app *Application) SettingsDeleteRule(c echo.Context) error {
	settings, err := app.store.GetCategorySettings()
	if err != nil {
		return err
	}
	id := c.Param("id")
	kept := settings.Rules[:0]
	for _, rule := range settings.Rules {
		if rule.ID != id {
			kept = append(kept, rule)
		}
	}
	settings.Rules = kept
	if err := app.store.SaveCategorySettings(settings); err != nil {
		return err
	}
	return app.renderCategories(c, "")
}
