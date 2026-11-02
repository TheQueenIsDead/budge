package pkg

import (
	"context"
	"log/slog"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/application"
	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/integrations"
	"github.com/TheQueenIsDead/budge/pkg/telemetry"
)

// Budge is an application container that holds references to the http server, data store, central logging configuration,
// and clients for third party integrations. It is responsible for wiring dependencies and orchestrating server start and
// close.
type Budge struct {
	Application  *application.Application
	Store        *database.Store
	Logger       *slog.Logger
	Integrations *integrations.Integrations
	Telemetry    *telemetry.Telemetry
}

// NewBudge returns an application container after calling initialisation methods on the application components.
func NewBudge() (*Budge, error) {

	// Telemetry first, so everything after it is traced, measured and logged
	// through it; slog is the default logger from here on.
	tel, err := telemetry.Setup(context.Background(), telemetry.DefaultOptions())
	if err != nil {
		return nil, err
	}
	logger := slog.Default()

	store, err := database.NewStore()
	if err != nil {
		logger.With("error", err).Error("could not initialise database")
		return nil, err
	}

	app, err := application.NewApplication(
		store,
		integrations.NewIntegrations(store),
		tel,
	)
	if err != nil {
		logger.With("error", err).Error("could not initialise application")
		return nil, err
	}

	budge := &Budge{
		Application: app,
		Store:       store,
		Logger:      logger,
		Telemetry:   tel,
	}

	return budge, nil
}

// Start begins the application http listener
func (b *Budge) Start() error {
	return b.Application.Start()
}

// Teardown stops the application components by gracefully closing them where possible.
func (b *Budge) Teardown() error {

	// Telemetry last, so what the others do while closing is still recorded
	// and anything waiting to be exported is flushed.
	funcs := []func() error{
		b.Application.Close,
		b.Store.Close,
		func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return b.Telemetry.Shutdown(ctx)
		},
	}
	for _, f := range funcs {
		if err := f(); err != nil {
			// TODO: Attempt to run all close handlers and gather a list of errors rather than singular
			return err
		}
	}
	return nil
}
