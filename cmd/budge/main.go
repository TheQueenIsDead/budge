package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/TheQueenIsDead/budge/pkg"
)

// version is stamped in at release with -ldflags "-X main.version=...".
// Without it, the version Go recorded from the checkout stands in.
var version string

func main() {
	// An interrupt or a terminate, from a terminal, docker stop or a reload,
	// shuts budge down cleanly rather than cutting it off, so the telemetry
	// file records the stop and nothing is lost midway through writing.
	stopping, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	budge, err := pkg.NewBudge(version)
	if err != nil {
		panic(err)
	}

	served := make(chan error, 1)
	go func() { served <- budge.Start() }()

	var reason string
	failed := false
	select {
	case <-stopping.Done():
		// "interrupt signal received", or "terminated signal received".
		reason = context.Cause(stopping).Error()
	case err := <-served:
		reason = "server stopped"
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			reason = "server failed: " + err.Error()
			failed = true
			slog.Error("server failed", "error", err)
		}
	}
	// A second signal while shutting down stops it at once.
	stop()

	if err := budge.Teardown(reason); err != nil {
		slog.Error("could not shut down cleanly", "error", err)
		failed = true
	}
	// A server that failed exits as a failure, so whatever runs budge can
	// tell and start it again.
	if failed {
		os.Exit(1)
	}
}
