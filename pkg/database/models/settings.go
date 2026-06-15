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
	Categories           []string
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
