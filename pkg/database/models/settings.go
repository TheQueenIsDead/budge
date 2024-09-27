package models

import (
	"errors"
	"github.com/TheQueenIsDead/budge/pkg/database/buckets"
	"strings"
)

type IntegrationAkahuSettings struct {
	AppToken  string
	UserToken string
}

func (ias IntegrationAkahuSettings) Key() []byte {
	return []byte("akahu")
}
func (ias IntegrationAkahuSettings) Bucket() []byte {
	return buckets.SettingsBucket
}

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
