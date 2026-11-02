package application

import (
	"errors"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// RuleView is a rule with how many transactions it currently files, so a
// rule that matches nothing stands out as a typo.
type RuleView struct {
	models.CategoryRule
	Matches int
	// Into names where it files, when that is a subcategory.
	Into string
}

// SettingsCategoryView is one budge category in settings: its subcategories,
// how much is filed under it, and the rules that file things there.
type SettingsCategoryView struct {
	models.BudgetCategory
	Subs  []models.BudgetCategory
	Count int // transactions filed under it or its subcategories
	Rules []RuleView
	// Options are where a new rule from here can file: the category and
	// each of its subcategories.
	Options []CategoryPickerOption
	// MoveTo are the categories its transactions can go to if it is deleted.
	MoveTo []CategoryPickerOption
	Fixed  bool // Uncategorised, which can be neither moved nor deleted
	Open   bool
}

// State is what the page keeps of the category while it is edited, so its
// heading follows the fields as they change.
func (v SettingsCategoryView) State() map[string]any {
	subs := make([]string, 0, len(v.Subs))
	for _, sub := range v.Subs {
		subs = append(subs, sub.Label)
	}
	return map[string]any{"label": v.Label, "blurb": v.Blurb, "icon": v.Icon, "accent": v.Accent, "subs": subs}
}

// SettingsCategoryGroupView is the categories on budget, or those off it, in
// settings.
type SettingsCategoryGroupView struct {
	Label      string
	Categories []SettingsCategoryView
}

// CategoriesView is the view-model for the settings.categories partial.
type CategoriesView struct {
	Groups []SettingsCategoryGroupView
	// What a category can be dressed in.
	Icons   []string
	Accents []string
	Types   []string // transaction types seen, for narrowing a rule
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
// and uncategorised ones come first, being what rules are mostly for. Money
// leaving by transfer is offered too, for rules that count it, but never the
// leg where it arrives, which no rule can match.
func ruleSuggestions(txs []models.Transaction, settings models.CategorySettings) []string {
	type suggestion struct {
		text          string
		count         int
		uncategorised bool
	}
	byText := make(map[string]*suggestion)
	for _, tx := range txs {
		if (tx.IsTransfer() && tx.Amount >= 0) || settings.MatchingRule(tx) >= 0 {
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
		if tx.BudgetCategory() == models.CategoryUncategorised {
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

// buildCategoriesView lays out every budge category with the rules that file
// into it. open names a category to draw expanded, the one just changed.
func (app *Application) buildCategoriesView(open string) (CategoriesView, error) {
	settings, err := app.store.GetCategorySettings()
	if err != nil {
		return CategoriesView{}, err
	}
	txs, err := app.store.ReadTransactions()
	if err != nil {
		return CategoriesView{}, err
	}

	view := CategoriesView{
		Suggestions: ruleSuggestions(txs, settings),
		Icons:       models.BudgetCategoryIcons,
		Accents:     models.BudgetCategoryAccents,
	}
	top := models.TopBudgetCategories()

	// Rules match on description, merchant and type, none of which a rule
	// changes, so matching the already-filed copies counts correctly.
	matches := make([]int, len(settings.Rules))
	filed := make(map[string]int)
	types := make(map[string]bool)
	for _, tx := range txs {
		if n := settings.MatchingRule(tx); n >= 0 {
			matches[n]++
		}
		if category, ok := debitCategory(tx); ok {
			filed[category.Key]++
		}
		if tx.Type != "" && !tx.IsTransfer() {
			types[tx.Type] = true
		}
	}
	for t := range types {
		view.Types = append(view.Types, t)
	}
	sort.Strings(view.Types)

	rules := make(map[string][]RuleView)
	for i, rule := range settings.Rules {
		target := rule.Target()
		top := models.TopBudgetCategory(target)
		ruleView := RuleView{CategoryRule: rule, Matches: matches[i]}
		if sub, ok := models.BudgetCategoryByKey(target); ok && sub.IsSub() {
			ruleView.Into = sub.Label
		}
		rules[top.Key] = append(rules[top.Key], ruleView)
	}

	for _, onBudget := range []bool{true, false} {
		section := SettingsCategoryGroupView{Label: "On budget"}
		if !onBudget {
			section.Label = "Off budget"
		}
		for _, category := range top {
			if category.Budgeted() != onBudget {
				continue
			}
			var moveTo []CategoryPickerOption
			for _, other := range top {
				if other.Key != category.Key {
					moveTo = append(moveTo, CategoryPickerOption{Key: other.Key, Label: other.Label})
				}
			}
			subs := models.BudgetSubcategories(category.Key)
			options := []CategoryPickerOption{{Key: category.Key, Label: category.Label}}
			for _, sub := range subs {
				options = append(options, CategoryPickerOption{Key: sub.Key, Label: sub.Label})
			}
			section.Categories = append(section.Categories, SettingsCategoryView{
				BudgetCategory: category,
				Subs:           subs,
				Count:          filed[category.Key],
				Rules:          rules[category.Key],
				Options:        options,
				MoveTo:         moveTo,
				Fixed:          category.Key == models.CategoryUncategorised,
				Open:           category.Key == open,
			})
		}
		view.Groups = append(view.Groups, section)
	}
	return view, nil
}

// renderCategories answers every categories action with the whole card, so
// rule counts always reflect the change. A message explains an action that
// was refused; open keeps the category being worked on expanded.
func (app *Application) renderCategories(c echo.Context, message, open string) error {
	view, err := app.buildCategoriesView(open)
	if err != nil {
		return err
	}
	view.Error = message
	return c.Render(http.StatusOK, "settings.categories", view)
}

// editBudgetCategories applies an edit to the category list and saves it,
// answering with the card and the category the edit names kept open. A
// refused edit is explained and changes nothing.
func (app *Application) editBudgetCategories(c echo.Context, edit func(models.BudgetCategorySettings) (models.BudgetCategorySettings, string, error)) error {
	return app.saveBudgetCategories(c, false, edit)
}

// saveBudgetCategories is editBudgetCategories, but a quiet save answers
// with nothing: the page already shows the change, made as it was typed, and
// redrawing the card would take the field being typed in away. A refused
// quiet save still redraws the card, putting back what was there.
func (app *Application) saveBudgetCategories(c echo.Context, quiet bool, edit func(models.BudgetCategorySettings) (models.BudgetCategorySettings, string, error)) error {
	settings, err := app.store.GetBudgetCategorySettings()
	if err != nil {
		return err
	}
	next, open, err := edit(settings)
	var refused models.BudgetCategoryError
	if errors.As(err, &refused) {
		if quiet {
			c.Response().Header().Set("HX-Retarget", "#categories")
			c.Response().Header().Set("HX-Reswap", "outerHTML")
		}
		return app.renderCategories(c, refused.Error(), open)
	}
	if err != nil {
		return err
	}
	if err := app.store.SaveBudgetCategorySettings(next); err != nil {
		return err
	}
	if quiet {
		return c.NoContent(http.StatusNoContent)
	}
	return app.renderCategories(c, "", open)
}

// SettingsAddBudgetCategory adds a category, off budget until it is put on,
// or a subcategory to a category. A new category is drawn open, ready to be
// dressed.
func (app *Application) SettingsAddBudgetCategory(c echo.Context) error {
	return app.editBudgetCategories(c, func(settings models.BudgetCategorySettings) (models.BudgetCategorySettings, string, error) {
		parent := c.FormValue("parent")
		next, key, err := settings.AddBudgetCategory(c.FormValue("label"), parent)
		if parent != "" {
			return next, parent, err
		}
		return next, key, err
	})
}

// SettingsUpdateBudgetCategory renames a category or changes its look, as
// each field is changed. Only putting it on or off budget redraws the card,
// asked for by "render", since the category moves to the other list.
func (app *Application) SettingsUpdateBudgetCategory(c echo.Context) error {
	key := c.Param("key")
	return app.saveBudgetCategories(c, c.FormValue("render") != "card", func(settings models.BudgetCategorySettings) (models.BudgetCategorySettings, string, error) {
		change := models.BudgetCategoryChange{
			Label:  c.FormValue("label"),
			Blurb:  c.FormValue("blurb"),
			Icon:   c.FormValue("icon"),
			Accent: c.FormValue("accent"),
		}
		if budget := c.FormValue("budget"); budget != "" {
			off := budget == "off"
			change.OffBudget = &off
		}
		next, err := settings.UpdateBudgetCategory(key, change)
		return next, models.TopBudgetCategory(key).Key, err
	})
}

// SettingsDeleteBudgetCategory deletes a category, sending everything filed
// under it to the category named by "into". Deleting a subcategory keeps its
// category open; deleting a category opens the one its transactions went to.
func (app *Application) SettingsDeleteBudgetCategory(c echo.Context) error {
	key := c.Param("key")
	return app.editBudgetCategories(c, func(settings models.BudgetCategorySettings) (models.BudgetCategorySettings, string, error) {
		category, _ := models.BudgetCategoryByKey(key)
		next, err := settings.DeleteBudgetCategory(key, c.FormValue("into"))
		if category.Parent != "" {
			return next, category.Parent, err
		}
		return next, c.FormValue("into"), err
	})
}

// SettingsAddRule adds a rule filing matching transactions under a category
// or subcategory. A new rule goes last, so it never takes transactions an
// earlier rule already files.
func (app *Application) SettingsAddRule(c echo.Context) error {
	target, ok := models.BudgetCategoryByKey(c.FormValue("category"))
	if !ok {
		return app.renderCategories(c, "Pick a category for the rule to file transactions under.", "")
	}
	open := models.TopBudgetCategory(target.Key).Key

	contains := strings.TrimSpace(c.FormValue("contains"))
	if contains == "" {
		return app.renderCategories(c, "A rule needs some text to match.", open)
	}

	settings, err := app.store.GetCategorySettings()
	if err != nil {
		return err
	}
	settings.Rules = append(settings.Rules, models.CategoryRule{
		ID:               newID(),
		Contains:         contains,
		Type:             c.FormValue("type"),
		Category:         target.Key,
		IncludeTransfers: c.FormValue("include_transfers") != "",
	})
	if err := app.store.SaveCategorySettings(settings); err != nil {
		return err
	}
	return app.renderCategories(c, "", open)
}

// rulePreviewSamples is how many matching transactions the preview lists.
const rulePreviewSamples = 5

// RulePreview shows what a rule would match before it is saved.
type RulePreview struct {
	Contains string
	Matches  int // transactions the new rule would file
	Taken    int // matching transactions an earlier rule already files
	// Transfers counts outgoing transfers the text matches that the rule
	// would leave alone because it does not include transfers, so the form
	// can say why a repayment it was typed for is missing.
	Transfers int
	Samples   []RuleSample
}

// RuleSample is one of the transactions a rule preview lists.
type RuleSample struct {
	Date     time.Time
	Title    string
	Category string
	Amount   float64
}

func buildRulePreview(rule models.CategoryRule, txs []models.Transaction, settings models.CategorySettings) RulePreview {
	preview := RulePreview{Contains: rule.Contains}
	withTransfers := rule
	withTransfers.IncludeTransfers = true
	var matched []models.Transaction
	for _, tx := range txs {
		if !rule.Matches(tx) {
			if withTransfers.Matches(tx) {
				preview.Transfers++
			}
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
		preview.Samples = append(preview.Samples, RuleSample{
			Date: tx.Date, Title: tx.Description, Category: strings.Join(tx.Categories(), " › "), Amount: tx.Amount,
		})
	}
	return preview
}

// SettingsPreviewRule answers the rule form as it is typed into.
func (app *Application) SettingsPreviewRule(c echo.Context) error {
	rule := models.CategoryRule{
		Contains:         strings.TrimSpace(c.QueryParam("contains")),
		Type:             c.QueryParam("type"),
		IncludeTransfers: c.QueryParam("include_transfers") != "",
	}
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
	var open string
	kept := settings.Rules[:0]
	for _, rule := range settings.Rules {
		if rule.ID != id {
			kept = append(kept, rule)
		} else {
			open = models.TopBudgetCategory(rule.Target()).Key
		}
	}
	settings.Rules = kept
	if err := app.store.SaveCategorySettings(settings); err != nil {
		return err
	}
	return app.renderCategories(c, "", open)
}
