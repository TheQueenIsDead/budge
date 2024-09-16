package models

import "errors"

type IntegrationAkahuSettings struct {
	AppToken  string
	UserToken string
}

// TODO: Validate user and app tokens are correct length and start with certain characters
func (ias *IntegrationAkahuSettings) Validate() error {
	if ias.AppToken == "" {
		return errors.New("AppToken is required but was empty")
	}
	if ias.UserToken == "" {
		return errors.New("UserToken is required but was empty")
	}
	return nil
}
