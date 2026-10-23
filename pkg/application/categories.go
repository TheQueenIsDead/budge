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
	// Suggestions autofill the rule's match text, uncategorised first.
	Suggestions []string
	Error       string
}

// maxRuleSuggestions bounds the autofill list; past a few hundred the browser
// list is too long to be useful and typing narrows it anyway.
const maxRuleSuggestions = 300

// ruleSuggestions offers match text for a new rule: each transaction's
// merchant, or its description cleaned of card terminal noise. The cleaning
// only trims, so a suggestion is always text the transaction contains and a
// rule built from it matches. Transactions a rule already files are left out,
// and uncategorised ones come first, being what rules are mostly for.
func ruleSuggestions(txs []models.Transaction, settings models.CategorySettings) []string {
	type suggestion struct {
		text          string
		count         int
		uncategorised bool
	}
	byText := make(map[string]*suggestion)
	for _, tx := range txs {
		if tx.Type == "TRANSFER" || settings.MatchingRule(tx) >= 0 {
			continue
		}
		text := strings.TrimSpace(spendMerchant(tx))
		if text == "" {
			continue
		}
		key := strings.ToLower(text)
		if byText[key] == nil {
			byText[key] = &suggestion{text: text}
		}
		byText[key].count++
		if tx.Category.Groups.PersonalFinance.Name == "" {
			byText[key].uncategorised = true
		}
	}
	all := make([]*suggestion, 0, len(byText))
	for _, s := range byText {
		all = append(all, s)
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].uncategorised != all[j].uncategorised {
			return all[i].uncategorised
		}
		if all[i].count != all[j].count {
			return all[i].count > all[j].count
		}
		return all[i].text < all[j].text
	})
	if len(all) > maxRuleSuggestions {
		all = all[:maxRuleSuggestions]
	}
	texts := make([]string, len(all))
	for i, s := range all {
		texts[i] = s.text
	}
	return texts
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

	view := CategoriesView{Custom: settings.Categories, Suggestions: ruleSuggestions(txs, settings)}

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

// rulePreviewSamples is how many matching transactions the preview lists.
const rulePreviewSamples = 5

// RulePreview shows what a rule would match before it is saved.
type RulePreview struct {
	Contains string
	Matches  int // transactions the new rule would file
	Taken    int // matching transactions an earlier rule already files
	Samples  []WeekTransaction
}

func buildRulePreview(rule models.CategoryRule, txs []models.Transaction, settings models.CategorySettings) RulePreview {
	preview := RulePreview{Contains: rule.Contains}
	var matched []models.Transaction
	for _, tx := range txs {
		if !rule.Matches(tx) {
			continue
		}
		// A new rule goes last, so anything an existing rule matches stays put.
		if settings.MatchingRule(tx) >= 0 {
			preview.Taken++
			continue
		}
		preview.Matches++
		matched = append(matched, tx)
	}
	sort.Slice(matched, func(i, j int) bool { return matched[i].Date.After(matched[j].Date) })
	for i, tx := range matched {
		if i == rulePreviewSamples {
			break
		}
		category := tx.Category.Name
		if category == "" {
			category = tx.Category.Groups.PersonalFinance.Name
		}
		preview.Samples = append(preview.Samples, WeekTransaction{
			Date: tx.Date, Title: tx.Description, Category: category, Amount: tx.Amount,
		})
	}
	return preview
}

// SettingsPreviewRule answers the rule form as it is typed into.
func (app *Application) SettingsPreviewRule(c echo.Context) error {
	rule := models.CategoryRule{Contains: strings.TrimSpace(c.QueryParam("contains")), Type: c.QueryParam("type")}
	if rule.Contains == "" {
		return c.Render(http.StatusOK, "settings.rules.preview", RulePreview{})
	}
	settings, err := app.store.GetCategorySettings()
	if err != nil {
		return err
	}
	txs, err := app.store.ReadTransactions()
	if err != nil {
		return err
	}
	return c.Render(http.StatusOK, "settings.rules.preview", buildRulePreview(rule, txs, settings))
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
