package models

import (
	"errors"
	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	"strings"
	"time"
)

type IntegrationAkahuSettings struct {
	AppToken  string
	UserToken string

	// LastSync is when a sync last succeeded, for showing the owner.
	LastSync time.Time

	// DataThrough is how fresh Akahu's data was at that sync: when it last
	// refreshed transactions from the banks. The next sync starts from here
	// rather than from LastSync. Banks post transactions days late and Akahu
	// fetches them on its own schedule, so a purchase can reach Akahu long
	// after a sync has run, dated before it. Reading from the time of the
	// sync missed every one of those; reading from Akahu's own freshness
	// does not. Zero means nothing is known, and the whole history is read.
	DataThrough time.Time
}

func (ias IntegrationAkahuSettings) Key() []byte {
	return []byte("akahu")
}
func (ias IntegrationAkahuSettings) Bucket() []byte {
	return buckets.SettingsBucket
}

type BudgetSalary struct {
	Salary               float64
	SalaryFrequency      string
	IncludePAYE          bool
	KiwiSaverRate        float64
	StudentLoan          bool
	SavingsGoal          float64
	SavingsGoalFrequency string
}

func (bs BudgetSalary) Key() []byte    { return []byte("salary") }
func (bs BudgetSalary) Bucket() []byte { return buckets.SettingsBucket }

func (ias *IntegrationAkahuSettings) Validate() error {
	if ias.AppToken == "" {
		return errors.New("AppToken is required but was empty")
	}
	if ias.UserToken == "" {
		return errors.New("UserToken is required but was empty")
	}
	if !strings.HasPrefix(ias.AppToken, "app_") {
		return errors.New("AppToken does not start with 'app_'")
	}
	if !strings.HasPrefix(ias.UserToken, "user_") {
		return errors.New("UserToken does not start with 'user_'")
	}
	return nil
}

// ScheduleInterval is one of the cadences a job may be run at. They are a fixed
// set rather than free text so a stored value cannot become unparseable.
type ScheduleInterval struct {
	Key   string
	Label string
	Every time.Duration
}

// AkahuIntervals are the cadences offered for a bank sync. Transactions land
// through the day, so hours are the useful unit.
var AkahuIntervals = []ScheduleInterval{
	{"1h", "Every hour", time.Hour},
	{"6h", "Every 6 hours", 6 * time.Hour},
	{"12h", "Every 12 hours", 12 * time.Hour},
	{"24h", "Once a day", 24 * time.Hour},
}

// AssetIntervals are the cadences offered for property estimates. The sources
// revise them slowly, so anything more frequent than daily is just traffic.
var AssetIntervals = []ScheduleInterval{
	{"24h", "Once a day", 24 * time.Hour},
	{"168h", "Once a week", 7 * 24 * time.Hour},
	{"720h", "Once a month", 30 * 24 * time.Hour},
}

// ResolveInterval finds a cadence by key, falling back to the given default so a
// hand-edited value cannot disable a job silently.
func ResolveInterval(intervals []ScheduleInterval, key string, fallback ScheduleInterval) ScheduleInterval {
	for _, interval := range intervals {
		if interval.Key == key {
			return interval
		}
	}
	return fallback
}

// ScheduleSettings is what runs by itself, and how often.
type ScheduleSettings struct {
	AkahuEnabled  bool
	AkahuInterval string
	AkahuLastRun  time.Time

	AssetsEnabled  bool
	AssetsInterval string
	AssetsLastRun  time.Time
}

func (s ScheduleSettings) Key() []byte    { return []byte("schedule") }
func (s ScheduleSettings) Bucket() []byte { return buckets.SettingsBucket }

// AkahuEvery and AssetsEvery resolve the stored keys into durations.
func (s ScheduleSettings) AkahuEvery() ScheduleInterval {
	return ResolveInterval(AkahuIntervals, s.AkahuInterval, AkahuIntervals[1])
}

func (s ScheduleSettings) AssetsEvery() ScheduleInterval {
	return ResolveInterval(AssetIntervals, s.AssetsInterval, AssetIntervals[0])
}

// Due reports whether a job should run now. A job that has never run is due
// immediately, so switching it on does something visible rather than waiting a
// full cycle first.
func Due(lastRun time.Time, every time.Duration, now time.Time) bool {
	if every <= 0 {
		return false
	}
	if lastRun.IsZero() {
		return true
	}
	return !now.Before(lastRun.Add(every))
}

// RemovedAccount is an Akahu account the owner has taken out of Budge. Akahu
// still shares it, so without this a sync would bring it straight back. The
// name and connection are kept so the settings page can say what was removed
// after the account itself is gone.
type RemovedAccount struct {
	Id               string `json:"_id"`
	Name             string `json:"name"`
	Connection       string `json:"connection"`
	FormattedAccount string `json:"formatted_account"`

	// Restoring is set when the account has been switched back on but the
	// sync bringing it back has not landed yet. It stays listed until then,
	// rather than vanishing from the page in between.
	Restoring bool `json:"restoring,omitempty"`
}

// RemovedAccounts is every account a sync should skip, and those on their
// way back.
type RemovedAccounts struct {
	Accounts []RemovedAccount `json:"accounts"`
}

func (r RemovedAccounts) Key() []byte    { return []byte("removed_accounts") }
func (r RemovedAccounts) Bucket() []byte { return buckets.SettingsBucket }

// IDs is the set of accounts a sync should skip. Those being restored are
// left out, since the sync is what brings them back.
func (r RemovedAccounts) IDs() map[string]bool {
	ids := make(map[string]bool, len(r.Accounts))
	for _, account := range r.Accounts {
		if !account.Restoring {
			ids[account.Id] = true
		}
	}
	return ids
}

// Restoring reports whether any account is waiting on a sync to come back.
func (r RemovedAccounts) Restoring() bool {
	for _, account := range r.Accounts {
		if account.Restoring {
			return true
		}
	}
	return false
}

// SpendRule is what puts a transaction in a spend group besides Akahu's
// category: words in a bank transfer's reference, and merchants named in a
// card payment or a reference.
type SpendRule struct {
	Keywords  []string `json:"keywords"`
	Merchants []string `json:"merchants"`
}

// SpendRules are the owner's own rules, by spend group key. A group missing
// here uses its built in rule, so the defaults can improve under an owner who
// never changed them.
type SpendRules struct {
	Groups map[string]SpendRule `json:"groups"`

	// Custom are groups the owner made up, such as rent or lunches, which
	// match on keywords and merchants alone.
	Custom []CustomSpendGroup `json:"custom,omitempty"`
}

// CustomSpendGroup is a spend group the owner named. Its key starts "custom-"
// so it can never collide with a built in one.
type CustomSpendGroup struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	SpendRule
}

func (r SpendRules) Key() []byte    { return []byte("spend_rules") }
func (r SpendRules) Bucket() []byte { return buckets.SettingsBucket }

// InsightExclusions are transactions the owner has taken out of the spend
// insights, for the odd one a rule gets wrong: a large payment that happened
// to carry the usual "groceries" reference.
type InsightExclusions struct {
	TransactionIDs []string `json:"transaction_ids"`
}

func (e InsightExclusions) Key() []byte    { return []byte("insight_exclusions") }
func (e InsightExclusions) Bucket() []byte { return buckets.SettingsBucket }
