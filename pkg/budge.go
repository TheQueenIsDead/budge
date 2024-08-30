package pkg

import (
	"github.com/TheQueenIsDead/budge/pkg/application"
	"github.com/TheQueenIsDead/budge/pkg/database"

	"log/slog"
)

type Budge struct {
	Application *application.Application
	Store       *database.Store
	Logger      *slog.Logger
}

func NewBudge() (*Budge, error) {

	logger := slog.Logger{}

	store, err := database.NewStore()
	if err != nil {
		logger.Error(err.Error())
		return nil, err
	}

	app, err := application.NewApplication(store)
	if err != nil {
		logger.Error(err.Error())
		return nil, err
	}

	budge := &Budge{
		Application: app,
		Store:       store,
		Logger:      &logger,
	}

	return budge, nil
}

func (b *Budge) Start() error {
	return b.Application.Start()
}

func (b *Budge) Teardown() error {

	funcs := []func() error{
		b.Application.Close,
		b.Store.Close,
	}
	for _, f := range funcs {
		if err := f(); err != nil {
			return err
		}
	}
	return nil
}