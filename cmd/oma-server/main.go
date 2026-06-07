package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/open-ma/oma-building/internal/api"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

func main() {
	addr := envOrDefault("OMA_LISTEN_ADDR", ":8787")
	dbPath := envOrDefault("DATABASE_PATH", "./data/oma.db")
	workdirBase := envOrDefault("SANDBOX_WORKDIR", "./data/sandboxes")
	absWorkdir, err := filepath.Abs(workdirBase)
	if err != nil {
		log.Fatal(err)
	}
	workdirBase = absWorkdir
	harnessURL := envOrDefault("HARNESS_URL", "http://127.0.0.1:8090")
	apiKey := os.Getenv("OMA_API_KEY")
	consoleDir := os.Getenv("CONSOLE_DIR")
	consoleDev := os.Getenv("OMA_CONSOLE_DEV") == "1"
	if consoleDir != "" {
		absConsole, err := filepath.Abs(consoleDir)
		if err != nil {
			log.Fatal(err)
		}
		consoleDir = absConsole
	}
	if consoleDev && consoleDir == "" {
		log.Print("warning: OMA_CONSOLE_DEV=1 without CONSOLE_DIR — auth stubs only")
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(workdirBase, 0o755); err != nil {
		log.Fatal(err)
	}

	db, err := store.Open(dbPath)
	if err != nil {
		log.Fatal(err)
	}
	defer store.Close(db)

	agents := store.NewAgentRepo(db)
	environments := store.NewEnvironmentRepo(db)
	if err := environments.EnsureDefault(context.Background()); err != nil {
		log.Fatal(err)
	}
	modelCards := store.NewModelCardRepo(db)
	modelResolver := &modelresolve.Resolver{Cards: modelCards}
	sessions := store.NewSessionRepo(db, agents, environments)
	if n, err := sessions.RecoverRunning(context.Background()); err != nil {
		log.Fatal(err)
	} else if n > 0 {
		log.Printf("recovered %d orphan running sessions", n)
	}

	events := store.NewEventRepo(db)
	hub := stream.NewHub()
	registry := session.NewRegistry()
	workdirs := workdir.NewManager(workdirBase)

	harnessTimeout := 10 * time.Minute
	if raw := os.Getenv("HARNESS_HTTP_TIMEOUT_SEC"); raw != "" {
		if sec, err := strconv.Atoi(raw); err == nil && sec > 0 {
			harnessTimeout = time.Duration(sec) * time.Second
		}
	}
	var harnessClient harness.Client = &harness.HTTPClient{
		BaseURL: harnessURL,
		HTTP:    &http.Client{Timeout: harnessTimeout},
	}
	if os.Getenv("OMA_FAKE_HARNESS") == "1" {
		harnessClient = &harness.FakeClient{}
	}

	handler := api.NewRouter(api.Deps{
		Agents:       agents,
		Environments: environments,
		ModelCards:   modelCards,
		Sessions: api.NewSessionHandlers(
			sessions, events, hub, registry, workdirs, harnessClient, modelResolver,
		),
		APIKey:     apiKey,
		ConsoleDir: consoleDir,
		ConsoleDev: consoleDev,
	})

	log.Printf("oma-server listening on %s", addr)
	if consoleDir != "" {
		log.Printf("console UI mounted from %s (dev=%v)", consoleDir, consoleDev)
	}
	if err := http.ListenAndServe(addr, handler); err != nil {
		log.Fatal(err)
	}
}

func envOrDefault(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
