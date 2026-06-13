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
	"github.com/open-ma/oma-building/internal/dream"
	"github.com/open-ma/oma-building/internal/eval"
	"github.com/open-ma/oma-building/internal/fileblob"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/memory"
	"github.com/open-ma/oma-building/internal/memoryblob"
	"github.com/open-ma/oma-building/internal/modelresolve"
	"github.com/open-ma/oma-building/internal/outbound"
	"github.com/open-ma/oma-building/internal/runtime"
	"github.com/open-ma/oma-building/internal/session"
	"github.com/open-ma/oma-building/internal/sessionoutputs"
	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/stream"
	"github.com/open-ma/oma-building/internal/workdir"
)

func main() {
	addr := envOrDefault("OMA_LISTEN_ADDR", ":8787")
	dbPath := envOrDefault("DATABASE_PATH", "./data/oma.db")
	workdirBase := envOrDefault("SANDBOX_WORKDIR", "./data/sandboxes")
	skillsDataDir := envOrDefault("SKILLS_DATA_DIR", "./data/skills")
	filesDataDir := envOrDefault("FILES_DATA_DIR", "./data/files")
	outputsDir := envOrDefault("SESSION_OUTPUTS_DIR", "./data/session-outputs")
	absWorkdir, err := filepath.Abs(workdirBase)
	if err != nil {
		log.Fatal(err)
	}
	workdirBase = absWorkdir
	harnessURL := envOrDefault("HARNESS_URL", "http://127.0.0.1:8090")
	apiKey := os.Getenv("OMA_API_KEY")
	consoleDir := os.Getenv("CONSOLE_DIR")
	authDisabled := os.Getenv("AUTH_DISABLED") == "1"
	authUpstream := envOrDefault("AUTH_UPSTREAM_URL", "http://127.0.0.1:8788")

	if consoleDir != "" {
		absConsole, err := filepath.Abs(consoleDir)
		if err != nil {
			log.Fatal(err)
		}
		consoleDir = absConsole
	}
	if authDisabled && consoleDir != "" {
		log.Print("warning: AUTH_DISABLED=1 — using auth stubs; not for production")
	}
	if !authDisabled && consoleDir != "" && authUpstream == "" {
		log.Print("warning: console mounted without AUTH_UPSTREAM_URL — cookie auth disabled")
	}

	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(workdirBase, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(skillsDataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(filesDataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(outputsDir, 0o755); err != nil {
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
	vaults := store.NewVaultRepo(db)
	credentials := store.NewCredentialRepo(db)
	skillFiles := store.NewSkillFileStore(skillsDataDir)
	fileBlobs := fileblob.NewStore(filesDataDir)
	files := store.NewFileRepo(db)
	skills := store.NewSkillRepo(db, skillFiles)
	sessionOutputs := sessionoutputs.NewStore(outputsDir)
	apiKeys := store.NewApiKeyRepo(db)
	tenants := store.NewTenantRepo(db)
	runtimes := store.NewRuntimeRepo(db)
	runtimeRooms := runtime.NewRegistry(runtimes)
	integrations := store.NewIntegrationRepo(db)
	memoryDataDir := envOrDefault("MEMORY_DATA_DIR", "./data/memory")
	if err := os.MkdirAll(memoryDataDir, 0o755); err != nil {
		log.Fatal(err)
	}
	memoryBlobs := memoryblob.NewStore(memoryDataDir)
	memoryStores := store.NewMemoryStoreRepo(db, memoryBlobs)
	evalRuns := store.NewEvalRunRepo(db)
	dreams := store.NewDreamRepo(db)
	modelResolver := &modelresolve.Resolver{Cards: modelCards}
	sessions := store.NewSessionRepo(db, agents, environments)
	if n, err := sessions.RecoverRunning(context.Background()); err != nil {
		log.Fatal(err)
	} else if n > 0 {
		log.Printf("recovered %d orphan running sessions", n)
	}

	events := store.NewEventRepo(db)
	pending := store.NewPendingRepo(db)
	hub := stream.NewHub()
	registry := session.NewRegistry()
	workdirs := workdir.NewManager(workdirBase, outputsDir)

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

	publicURL := envOrDefault("OMA_PUBLIC_URL", "http://127.0.0.1:8787")
	outboundAddr := envOrDefault("OMA_OUTBOUND_PROXY_ADDR", ":8790")
	internalSecret := os.Getenv("OMA_INTERNAL_SECRET")
	resourceResolver := &harness.ResourceResolver{
		Files:        files,
		FileBlobs:    fileBlobs,
		MemoryStores: memoryStores,
	}
	sessionHandlers := api.NewSessionHandlers(
		sessions, agents, events, pending, hub, registry, workdirs,
		sessionOutputs, harnessClient, modelResolver, resourceResolver,
		publicURL, apiKey,
		outbound.HostForHarness(outboundAddr), apiKey,
	)
	evalWorker := &eval.Worker{
		EvalRuns:  evalRuns,
		Sessions:  api.NewEvalSessionRunner(sessionHandlers),
		Events:    events,
		Agents:    agents,
		Models:    modelResolver,
		Evaluator: harness.AsOutcomeEvaluator(harnessClient),
	}
	if os.Getenv("OMA_EVAL_WORKER_DISABLED") != "1" {
		interval := 30 * time.Second
		if raw := os.Getenv("OMA_EVAL_WORKER_INTERVAL"); raw != "" {
			if d, err := time.ParseDuration(raw); err == nil && d > 0 {
				interval = d
			}
		}
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				if _, err := evalWorker.Tick(context.Background()); err != nil {
					log.Printf("eval worker tick: %v", err)
				}
			}
		}()
		log.Printf("eval worker enabled (interval=%s)", interval)
	}
	dreamWorker := &dream.Worker{
		Dreams:       dreams,
		MemoryStores: memoryStores,
		Sessions:     sessions,
	}
	if os.Getenv("OMA_DREAM_WORKER_DISABLED") != "1" {
		interval := 30 * time.Second
		if raw := os.Getenv("OMA_DREAM_WORKER_INTERVAL"); raw != "" {
			if d, err := time.ParseDuration(raw); err == nil && d > 0 {
				interval = d
			}
		}
		go func() {
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			for range ticker.C {
				if _, err := dreamWorker.Tick(context.Background()); err != nil {
					log.Printf("dream worker tick: %v", err)
				}
			}
		}()
		log.Printf("dream worker enabled (interval=%s)", interval)
	}
	memoryRetention := &memory.RetentionWorker{
		MemoryStores: memoryStores,
	}
	if os.Getenv("OMA_MEMORY_RETENTION_DISABLED") != "1" {
		go func() {
			ticker := time.NewTicker(time.Minute)
			defer ticker.Stop()
			for range ticker.C {
				result, err := memoryRetention.Tick(context.Background())
				if err != nil {
					log.Printf("memory retention tick: %v", err)
				} else if result.Ran && result.Removed > 0 {
					log.Printf(
						"memory retention pruned %d version rows",
						result.Removed,
					)
				}
			}
		}()
		log.Print("memory retention worker enabled (daily 03:00 UTC)")
	}
	linearGateway := api.NewLinearGatewayHandler(
		integrations, sessionHandlers, publicURL, internalSecret,
	)
	githubGateway := api.NewGitHubGatewayHandler(
		integrations, sessionHandlers, publicURL,
	)
	slackGateway := api.NewSlackGatewayHandler(
		integrations, sessionHandlers, publicURL,
	)
	handler := api.NewRouter(api.Deps{
		Agents:       agents,
		Environments: environments,
		ModelCards:   modelCards,
		Vaults:       vaults,
		Credentials:  credentials,
		Skills:         skills,
		SkillFiles:     skillFiles,
		Files:          files,
		FileBlobs:      fileBlobs,
		SessionOutputs: sessionOutputs,
		ApiKeys:        apiKeys,
		Tenants:        tenants,
		Runtimes:       runtimes,
		RuntimeRooms:   runtimeRooms,
		Integrations:   integrations,
		MemoryStores:   memoryStores,
		EvalRuns:       evalRuns,
		Dreams:         dreams,
		DreamWorker:    dreamWorker,
		Events:         events,
		Sessions:      sessionHandlers,
		APIKey:       apiKey,
		ConsoleDir:   consoleDir,
		AuthDisabled: authDisabled,
		AuthUpstream: authUpstream,
		McpProxyBase: publicURL,
		McpProxyKey:  apiKey,
		OutboundProxyAddr: outboundAddr,
		OutboundProxyKey:  apiKey,
		InternalSecret:    internalSecret,
		ModelResolver:     modelResolver,
		LinearGateway:     linearGateway,
		GitHubGateway:     githubGateway,
		SlackGateway:      slackGateway,
	})

	log.Printf("oma-server listening on %s", addr)
	if outboundAddr != "" {
		go func() {
			proxy := outbound.NewProxy(outbound.ProxyDeps{
				Resolver: &outbound.Resolver{
					Sessions:    sessions,
					Credentials: credentials,
				},
				ApiKeys: apiKeys,
				APIKey:  apiKey,
			})
			if err := outbound.ListenAndServe(
				context.Background(), outboundAddr, proxy,
			); err != nil {
				log.Printf("outbound proxy stopped: %v", err)
			}
		}()
	}
	if consoleDir != "" {
		log.Printf(
			"console UI mounted from %s (auth_disabled=%v upstream=%s)",
			consoleDir,
			authDisabled,
			authUpstream,
		)
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
