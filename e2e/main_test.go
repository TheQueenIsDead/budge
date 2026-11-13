//go:build e2e

// Package e2e drives budge in a browser: the image budge is released as, run
// on made-up data, in a pinned Chrome, with the mouse and keyboard a person
// uses. budge and Chrome each run in a container, on a network of their own.
//
// BUDGE_E2E_IMAGE names the budge image to test; see the README.
package e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TheQueenIsDead/budge/pkg/clock"
	"github.com/TheQueenIsDead/budge/pkg/database"
	"github.com/TheQueenIsDead/budge/pkg/test"
	"github.com/chromedp/chromedp"
	"github.com/chromedp/chromedp/remote"
	"github.com/testcontainers/testcontainers-go"
	"github.com/testcontainers/testcontainers-go/network"
	"github.com/testcontainers/testcontainers-go/wait"
)

// chrome is the Chrome the tests drive. Renovate keeps it current.
// renovate: datasource=docker depName=docker.io/chromedp/headless-shell
const chrome = "docker.io/chromedp/headless-shell:159.0.8144.0"

var (
	// image is the budge image under test.
	image string
	// net is the network budge and Chrome share.
	net *testcontainers.DockerNetwork
	// browser is Chrome; each test opens tabs in it.
	browser context.Context
	// seeded is a database file holding test.Seed's data, which each budge
	// starts from a copy of.
	seeded []byte
)

func TestMain(m *testing.M) {
	os.Exit(run(m))
}

func run(m *testing.M) int {
	image = os.Getenv("BUDGE_E2E_IMAGE")
	if image == "" {
		fmt.Fprintln(os.Stderr, "BUDGE_E2E_IMAGE must name the budge image to test, such as budge:e2e; see the README")
		return 1
	}
	ctx := context.Background()
	fail := func(what string, err error) int {
		fmt.Fprintf(os.Stderr, "%s: %v\n", what, err)
		return 1
	}

	var err error
	// Seeded as of the e2e build's stopped clock, which the budge under test
	// is built with too, so both agree on what today is, on any day.
	if seeded, err = seed(clock.Now()); err != nil {
		return fail("seeding a database", err)
	}

	if net, err = network.New(ctx); err != nil {
		return fail("creating a network", err)
	}
	defer func() { _ = net.Remove(ctx) }()

	browserContainer, err := testcontainers.Run(ctx, chrome,
		network.WithNetwork([]string{"chrome"}, net),
		testcontainers.WithExposedPorts("9222/tcp"),
		testcontainers.WithWaitStrategy(wait.ForListeningPort("9222/tcp")),
	)
	defer func() { _ = testcontainers.TerminateContainer(browserContainer) }()
	if err != nil {
		return fail("starting Chrome", err)
	}
	endpoint, err := browserContainer.PortEndpoint(ctx, "9222/tcp", "http")
	if err != nil {
		return fail("finding Chrome's port", err)
	}

	allocator, cancelAllocator := remote.NewAllocator(ctx, endpoint)
	defer cancelAllocator()
	b, cancelBrowser := chromedp.NewContext(allocator)
	defer cancelBrowser()
	if err := chromedp.Do(b); err != nil {
		return fail("connecting to Chrome", err)
	}
	browser = b

	return m.Run()
}

// seed returns a database file holding test.Seed's data as of now.
func seed(now time.Time) ([]byte, error) {
	dir, err := os.MkdirTemp("", "budge-e2e-")
	if err != nil {
		return nil, err
	}
	defer func() { _ = os.RemoveAll(dir) }()

	// The store finds its file through the environment, as budge does.
	// Nothing else in the tests reads it.
	if err := os.Setenv("BUDGE_BOLT_PATH", dir); err != nil {
		return nil, err
	}

	store, err := database.NewStore()
	if err != nil {
		return nil, err
	}
	if err := test.Seed(context.Background(), store, now); err != nil {
		_ = store.Close()
		return nil, err
	}
	if err := store.Close(); err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, "budge.bolt.db"))
}

var started atomic.Int32

// startBudge starts budge on a fresh copy of the seeded data, for this test
// alone, and returns its address as Chrome sees it. Its log is shown if the
// test fails.
func startBudge(t *testing.T) string {
	t.Helper()
	host := fmt.Sprintf("budge-%d", started.Add(1))

	logs := &logBuffer{}
	budge, err := testcontainers.Run(context.Background(), image,
		network.WithNetwork([]string{host}, net),
		// Telemetry is left on, as it is by default, which the admin pages
		// need. With no collector named, it is kept in budge alone.
		testcontainers.WithEnv(map[string]string{"BUDGE_BOLT_PATH": "/data"}),
		testcontainers.WithFiles(testcontainers.ContainerFile{
			Reader:            bytes.NewReader(seeded),
			ContainerFilePath: "/data/budge.bolt.db",
			FileMode:          0o600,
		}),
		testcontainers.WithWaitStrategy(wait.ForLog("http server started")),
		testcontainers.WithLogConsumers(logs),
	)
	testcontainers.CleanupContainer(t, budge)
	if err != nil {
		t.Fatalf("starting budge: %v", err)
	}
	t.Cleanup(func() {
		if t.Failed() {
			t.Logf("budge's log:\n%s", logs)
		}
	})
	return "http://" + host + ":1337"
}

// logBuffer keeps a container's log.
type logBuffer struct {
	mu  sync.Mutex
	log strings.Builder
}

func (b *logBuffer) Accept(l testcontainers.Log) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.log.Write(l.Content)
}

func (b *logBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.log.String()
}
