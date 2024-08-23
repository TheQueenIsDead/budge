package database

import "errors"

var (
	BankHeaderNotFoundClassifierError = errors.New("bank header not found")
	EmptyFileClassifierError          = errors.New("empty file parsed")
	NoBankParsingStrategyError        = errors.New("no bank parsing strategy")
)
