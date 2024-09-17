package models

import (
	"errors"
	"strings"
)

type IntegrationAkahuSettings struct {
	AppToken  string
	UserToken string
}

// TODO: Validate user and app tokens are correct length
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
