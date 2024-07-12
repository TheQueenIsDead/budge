package pkg

import (
	"errors"
)

var (
	EmptyFileClassifierError          = errors.New("empty file parsed")
	BankHeaderNotFoundClassifierError = errors.New("bank header not found")
	NoBankParsingStrategyError        = errors.New("no bank parsing strategy")
)
