package application

import (
	"net/http"
	"strings"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/database/models"
	"github.com/labstack/echo/v4"
)

// ScheduleView is the schedule as the settings page reads it: what is on, how
// often, and when it will next happen.
type ScheduleView struct {
	Settings       models.ScheduleSettings
	AkahuIntervals []models.ScheduleInterval
	AssetIntervals []models.ScheduleInterval

	// Next is zero when a job has never run, in which case it is due at the next
	// tick and the page says so rather than showing a date in the past.
	AkahuNext  time.Time
	AssetsNext time.Time
}

func buildScheduleView(schedule models.ScheduleSettings) ScheduleView {
	view := ScheduleView{
		Settings:       schedule,
		AkahuIntervals: models.AkahuIntervals,
		AssetIntervals: models.AssetIntervals,
	}
	if !schedule.AkahuLastRun.IsZero() {
		view.AkahuNext = schedule.AkahuLastRun.Add(schedule.AkahuEvery().Every)
	}
	if !schedule.AssetsLastRun.IsZero() {
		view.AssetsNext = schedule.AssetsLastRun.Add(schedule.AssetsEvery().Every)
	}
	return view
}

func (app *Application) Settings(c echo.Context) error {
	accounts, err := app.store.ReadAccounts()
	if err != nil {
		app.Toast(c, "Error", "Could not read accounts.")
		return c.NoContent(http.StatusInternalServerError)
	}

	akahuConfig, err := app.store.GetAkahuSettings()
	if err != nil {
		app.Toast(c, "Error", "Could not get Akahu settings.")
		return c.NoContent(http.StatusInternalServerError)
	}

	schedule, err := app.store.GetScheduleSettings()
	if err != nil {
		app.Toast(c, "Error", "Could not read the schedule.")
		return c.NoContent(http.StatusInternalServerError)
	}

	removed, err := app.store.GetRemovedAccounts()
	if err != nil {
		app.Toast(c, "Error", "Could not read untracked accounts.")
		return c.NoContent(http.StatusInternalServerError)
	}

	return c.Render(http.StatusOK, "settings", map[string]interface{}{
		"accounts":       accounts,
		"removed":        removed.Accounts,
		"restoring":      removed.Restoring(),
		"spendRules":     spendRuleViews(),
		"akahuAppToken":  akahuConfig.AppToken,
		"akahuUserToken": akahuConfig.UserToken,
		"sync":           app.syncStatus(),
		"schedule":       buildScheduleView(schedule),
	})
}

func (app *Application) SettingsDeleteSynced(c echo.Context) error {

	err := app.store.DeleteSynced()
	if err != nil {
		return err
	}
	_ = app.store.ResetAkahuLastSync()

	app.Toast(c, "Success", "All synced data removed.")
	return nil
}

func (app *Application) PutAkahuSettings(c echo.Context) error {

	settings, err := app.store.GetAkahuSettings()
	if err != nil {
		return err
	}

	settings.AppToken = c.FormValue("akahuAppToken")
	settings.UserToken = c.FormValue("akahuUserToken")
	if err := settings.Validate(); err != nil {
		return err
	}

	err = app.integrations.PutAkahuSettings(settings)
	if err != nil {
		return err
	}
	app.Toast(c, "Success", "Akahu settings saved successfully!")
	return nil
}

// SettingsSaveSchedule stores what should run by itself. Last-run times are
// carried across rather than reset, so changing a cadence does not fire a job
// immediately as a side effect of saving.
func (app *Application) SettingsSaveSchedule(c echo.Context) error {

	schedule, err := app.store.GetScheduleSettings()
	if err != nil {
		app.Toast(c, "Error", "Could not read the schedule.")
		return c.NoContent(http.StatusInternalServerError)
	}

	schedule.AkahuEnabled = c.FormValue("akahu_enabled") != ""
	schedule.AssetsEnabled = c.FormValue("assets_enabled") != ""
	schedule.AkahuInterval = models.ResolveInterval(
		models.AkahuIntervals, c.FormValue("akahu_interval"), schedule.AkahuEvery()).Key
	schedule.AssetsInterval = models.ResolveInterval(
		models.AssetIntervals, c.FormValue("assets_interval"), schedule.AssetsEvery()).Key

	if err := app.store.SaveScheduleSettings(schedule); err != nil {
		app.Toast(c, "Error", "Could not save the schedule.")
		return c.NoContent(http.StatusInternalServerError)
	}

	app.Toast(c, "Success", "Schedule saved.")
	return c.Render(http.StatusOK, "settings.schedule", buildScheduleView(schedule))
}

// renderSettingsAccounts answers a tracking switch with the accounts card.
func (app *Application) renderSettingsAccounts(c echo.Context) error {
	accounts, err := app.store.ReadAccounts()
	if err != nil {
		app.Toast(c, "Error", "Could not read accounts.")
		return c.NoContent(http.StatusInternalServerError)
	}
	removed, err := app.store.GetRemovedAccounts()
	if err != nil {
		app.Toast(c, "Error", "Could not read untracked accounts.")
		return c.NoContent(http.StatusInternalServerError)
	}
	return c.Render(http.StatusOK, "settings.accounts", map[string]interface{}{
		"accounts":  accounts,
		"removed":   removed.Accounts,
		"restoring": removed.Restoring(),
	})
}

// SettingsAccounts answers the accounts card's poll while an account is on
// its way back.
func (app *Application) SettingsAccounts(c echo.Context) error {
	return app.renderSettingsAccounts(c)
}

// SettingsUntrackAccount stops tracking an account Akahu shares, such as one
// held jointly or for someone else, dropping what has been synced from it.
func (app *Application) SettingsUntrackAccount(c echo.Context) error {
	if err := app.store.RemoveAccount(c.Param("id")); err != nil {
		app.Toast(c, "Error", "Could not stop tracking the account.")
		return c.NoContent(http.StatusInternalServerError)
	}
	app.Toast(c, "Success", "Account no longer tracked.")
	return app.renderSettingsAccounts(c)
}

// SettingsTrackAccount tracks an account again and starts the sync that
// brings back its history. If a sync is already running it is left to
// finish, and the next one fetches the account.
func (app *Application) SettingsTrackAccount(c echo.Context) error {
	if err := app.store.RestoreAccount(c.Param("id")); err != nil {
		app.Toast(c, "Error", "Could not track the account.")
		return c.NoContent(http.StatusInternalServerError)
	}
	if app.akahuSync.begin() {
		go func() {
			if err := app.runAkahuSync(); err != nil {
				app.Notify(models.NotificationError, SourceAkahuSync, "Sync failed", err.Error())
			}
		}()
		app.Toast(c, "Success", "Account tracked again. Syncing its history back.")
	} else {
		app.Toast(c, "Success", "Account tracked again. It comes back with the next sync.")
	}
	return app.renderSettingsAccounts(c)
}

// SpendRuleView is one spend group's rule as the settings page edits it.
type SpendRuleView struct {
	Group     SpendGroup
	Keywords  string
	Merchants string
	Custom    bool
}

func spendRuleViews() []SpendRuleView {
	groups := SpendGroups()
	views := make([]SpendRuleView, len(groups))
	for i, group := range groups {
		rule, custom := GroupRule(group)
		views[i] = SpendRuleView{
			Group:     group,
			Keywords:  strings.Join(rule.Keywords, ", "),
			Merchants: strings.Join(rule.Merchants, ", "),
			Custom:    custom,
		}
	}
	return views
}

// parseRuleList reads a comma or line separated list as typed into a field,
// dropping blanks and repeats however they are cased.
func parseRuleList(text string) []string {
	seen := make(map[string]bool)
	list := []string{}
	for _, item := range strings.FieldsFunc(text, func(r rune) bool { return r == ',' || r == '\n' }) {
		item = strings.Join(strings.Fields(item), " ")
		if item == "" || seen[strings.ToLower(item)] {
			continue
		}
		seen[strings.ToLower(item)] = true
		list = append(list, item)
	}
	return list
}

// saveSpendRule stores a group's rule, or forgets it when rule is nil so the
// group falls back to its built in one, and puts it into effect at once.
func (app *Application) saveSpendRule(c echo.Context, rule *models.SpendRule) error {
	group, ok := SpendGroupByKey(c.Param("key"))
	if !ok {
		return echo.NewHTTPError(http.StatusNotFound, "no such spend group")
	}
	rules, err := app.store.GetSpendRules()
	if err != nil {
		return err
	}
	if rules.Groups == nil {
		rules.Groups = make(map[string]models.SpendRule)
	}
	if rule == nil {
		delete(rules.Groups, group.Key)
	} else {
		rules.Groups[group.Key] = *rule
	}
	if err := app.store.SaveSpendRules(rules); err != nil {
		app.Toast(c, "Error", "Could not save the insight rules.")
		return c.NoContent(http.StatusInternalServerError)
	}
	SetSpendRules(rules)
	return nil
}

// SettingsSaveSpendRule stores the keywords and merchants a spend group
// matches on.
func (app *Application) SettingsSaveSpendRule(c echo.Context) error {
	rule := models.SpendRule{
		Keywords:  parseRuleList(c.FormValue("keywords")),
		Merchants: parseRuleList(c.FormValue("merchants")),
	}
	if err := app.saveSpendRule(c, &rule); err != nil {
		return err
	}
	app.Toast(c, "Success", "Insight rules saved.")
	return c.Render(http.StatusOK, "settings.spend", spendRuleViews())
}

// SettingsResetSpendRule puts a spend group back on its built in rule.
func (app *Application) SettingsResetSpendRule(c echo.Context) error {
	if err := app.saveSpendRule(c, nil); err != nil {
		return err
	}
	app.Toast(c, "Success", "Insight rules reset.")
	return c.Render(http.StatusOK, "settings.spend", spendRuleViews())
}
