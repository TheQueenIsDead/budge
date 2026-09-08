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
	LastSync  time.Time
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
