package pkg

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
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
// version is what the release stamped in, and may be empty.
func NewBudge(version string) (*Budge, error) {

	// Telemetry first, so everything after it is traced, measured and logged
	// through it; slog is the default logger from here on. Turned off, logs
	// still go to stdout, and nothing else is kept.
	enabled, err := telemetryEnabled()
	if err != nil {
		return nil, err
	}
	var tel *telemetry.Telemetry
	if enabled {
		opts, err := telemetryOptions(version)
		if err != nil {
			return nil, err
		}
		if tel, err = telemetry.Setup(context.Background(), opts); err != nil {
			return nil, err
		}
	} else {
		slog.SetDefault(slog.New(slog.NewTextHandler(os.Stdout, nil)))
		version, revision := telemetry.BuildVersion(version)
		slog.Info("budge started", "version", version, "revision", revision, "telemetry", "off")
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

// Teardown stops the application components, gracefully where possible,
// noting why. Every component is closed even if one fails to, and what
// failed is reported together.
func (b *Budge) Teardown(reason string) error {
	if b.Telemetry == nil {
		slog.Info("budge stopping", "reason", reason)
		return errors.Join(b.Application.Close(), b.Store.Close())
	}
	b.Telemetry.StoppingBecause(reason)

	// Telemetry last, so what the others do while closing is still recorded,
	// anything waiting is flushed, and the stop is marked.
	return errors.Join(
		b.Application.Close(),
		b.Store.Close(),
		func() error {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			return b.Telemetry.Shutdown(ctx)
		}(),
	)
}

// telemetryEnabled reads whether telemetry runs: unless BUDGE_TELEMETRY
// says false, or OpenTelemetry's own OTEL_SDK_DISABLED says true.
func telemetryEnabled() (bool, error) {
	if disabled, err := envBool("OTEL_SDK_DISABLED"); err != nil || disabled {
		return false, err
	}
	if value, ok := os.LookupEnv("BUDGE_TELEMETRY"); ok && strings.TrimSpace(value) != "" {
		enabled, err := envBool("BUDGE_TELEMETRY")
		return enabled, err
	}
	return true, nil
}

// envBool reads a variable as true or false, taking on and off as well. An
// unset variable is false.
func envBool(name string) (bool, error) {
	value := strings.ToLower(strings.TrimSpace(os.Getenv(name)))
	switch value {
	case "", "false", "0", "off", "no":
		return false, nil
	case "true", "1", "on", "yes":
		return true, nil
	}
	return false, fmt.Errorf("%s: %q is not true or false", name, value)
}

// telemetryOptions keeps samples, logs and traces beside the database for
// BUDGE_TELEMETRY_RETENTION, a week unless that says otherwise: "36h", "14d",
// or "0" to keep nothing past a restart.
func telemetryOptions(version string) (telemetry.Options, error) {
	opts := telemetry.DefaultOptions()
	opts.Version = version
	dir, ok := os.LookupEnv("BUDGE_BOLT_PATH")
	if !ok {
		dir = "."
	}
	opts.Path = filepath.Join(dir, "budge.telemetry.db")
	// Left empty, as BUDGE_TELEMETRY may be, it is as good as unset.
	if value, ok := os.LookupEnv("BUDGE_TELEMETRY_RETENTION"); ok && strings.TrimSpace(value) != "" {
		retention, err := parseRetention(value)
		if err != nil {
			return opts, fmt.Errorf("BUDGE_TELEMETRY_RETENTION: %w", err)
		}
		opts.Retention = retention
	}
	return opts, nil
}

// parseRetention reads a duration as Go writes one, or a whole number of
// days, which is how a retention is usually thought of.
func parseRetention(value string) (time.Duration, error) {
	value = strings.TrimSpace(value)
	if days, ok := strings.CutSuffix(value, "d"); ok {
		n, err := strconv.Atoi(days)
		if err != nil || n < 0 {
			return 0, fmt.Errorf("%q is not a number of days", value)
		}
		// Past about 292 years a duration wraps negative, which would read
		// as no retention and quietly keep telemetry in memory only.
		if n > int((1<<63-1)/(24*time.Hour)) {
			return 0, fmt.Errorf("%q is more days than can be kept", value)
		}
		return time.Duration(n) * 24 * time.Hour, nil
	}
	if value == "0" {
		return 0, nil
	}
	retention, err := time.ParseDuration(value)
	if err != nil || retention < 0 {
		return 0, fmt.Errorf("%q is not a duration such as 36h or 7d", value)
	}
	return retention, nil
}
