// Crash-recovery integration tests (ported from main-node/test/crash-recovery.test.ts).
//
// Spawns the real oma-server binary, injects orphan session rows into SQLite
// (simulating SIGKILL mid-turn), restarts the process, and asserts the boot
// recovery hook (RecoverRunning in cmd/oma-server/main.go) reconciles state.
//
// P1 scope: running → interrupted, turn_id cleared. Stream finalization and
// tool_use placeholder injection are main-node P2; oma leaves event log as-is.

package api_test

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

const (
	crashSeedAgentID = "agent_crash_seed"
	crashSeedSnap    = `{"id":"agent_crash_seed","name":"crash","model":"claude-sonnet-4-6","version":1}`
)

var omaServerBin string

func TestMain(m *testing.M) {
	root := moduleRootDir()
	bin, err := buildOmaServer(root)
	if err != nil {
		fmt.Fprintf(os.Stderr, "build oma-server: %v\n", err)
		os.Exit(1)
	}
	omaServerBin = bin
	code := m.Run()
	_ = os.Remove(bin)
	os.Exit(code)
}

func moduleRootDir() string {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		return "."
	}
	return filepath.Clean(filepath.Join(filepath.Dir(file), "..", ".."))
}

func buildOmaServer(root string) (string, error) {
	bin := filepath.Join(os.TempDir(),
		fmt.Sprintf("oma-server-crash-%d", os.Getpid()))
	cmd := exec.Command("go", "build", "-o", bin, "./cmd/oma-server")
	cmd.Dir = root
	out, err := cmd.CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("%w: %s", err, out)
	}
	return bin, nil
}

type processHandle struct {
	cmd     *exec.Cmd
	port    int
	dataDir string
	logs    bytes.Buffer
}

func startOmaServer(t *testing.T, dataDir string) *processHandle {
	t.Helper()
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatal(err)
	}
	port := pickFreePort(t)
	cmd := exec.Command(omaServerBin)
	cmd.Env = append(os.Environ(),
		"OMA_LISTEN_ADDR=:"+strconv.Itoa(port),
		"DATABASE_PATH="+filepath.Join(dataDir, "oma.db"),
		"SANDBOX_WORKDIR="+filepath.Join(dataDir, "sandboxes"),
		"OMA_FAKE_HARNESS=1",
		"OMA_API_KEY=dev-key",
	)
	h := &processHandle{cmd: cmd, port: port, dataDir: dataDir}
	cmd.Stdout = &h.logs
	cmd.Stderr = &h.logs
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}

	waitHealthOK(t, h.baseURL(), 60*time.Second)
	// Boot recovery runs before Listen; brief pause for log flush.
	time.Sleep(200 * time.Millisecond)
	return h
}

func (h *processHandle) baseURL() string {
	return "http://127.0.0.1:" + strconv.Itoa(h.port)
}

func pickFreePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	return ln.Addr().(*net.TCPAddr).Port
}

func waitHealthOK(t *testing.T, baseURL string, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(baseURL + "/health")
		if err == nil && resp.StatusCode == http.StatusOK {
			_ = resp.Body.Close()
			return
		}
		if resp != nil {
			_ = resp.Body.Close()
		}
		time.Sleep(200 * time.Millisecond)
	}
	t.Fatalf("server %s did not become healthy within %s", baseURL, timeout)
}

func killHard(t *testing.T, h *processHandle) {
	t.Helper()
	if h == nil || h.cmd.Process == nil {
		return
	}
	_ = h.cmd.Process.Kill()
	_ = h.cmd.Wait()
}

func withCrashDB(t *testing.T, dataDir string, fn func(*sql.DB)) {
	t.Helper()
	db, err := sql.Open("sqlite", filepath.Join(dataDir, "oma.db"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := db.Exec(`PRAGMA busy_timeout = 5000`); err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	fn(db)
}

func ensureCrashSeedAgent(t *testing.T, db *sql.DB, now int64) {
	t.Helper()
	_, err := db.Exec(`
		INSERT OR IGNORE INTO agents
		  (id, tenant_id, config, version, created_at, updated_at)
		VALUES (?, 'default', ?, 1, ?, ?)`,
		crashSeedAgentID, crashSeedSnap, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func seedOrphanSession(
	t *testing.T,
	db *sql.DB,
	sessionID, turnID string,
	startedAt int64,
) {
	t.Helper()
	ensureCrashSeedAgent(t, db, startedAt)
	_, err := db.Exec(`
		INSERT INTO sessions
		  (id, tenant_id, agent_id, agent_version, agent_snapshot,
		   title, status, turn_id, created_at, updated_at,
		   environment_id, environment_snapshot)
		VALUES (?, 'default', ?, 1, ?, '', 'running', ?, ?, ?, '', '{}')`,
		sessionID, crashSeedAgentID, crashSeedSnap,
		turnID, startedAt, startedAt,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func seedIdleSession(t *testing.T, db *sql.DB, sessionID string, now int64) {
	t.Helper()
	ensureCrashSeedAgent(t, db, now)
	_, err := db.Exec(`
		INSERT INTO sessions
		  (id, tenant_id, agent_id, agent_version, agent_snapshot,
		   title, status, turn_id, created_at, updated_at,
		   environment_id, environment_snapshot)
		VALUES (?, 'default', ?, 1, ?, '', 'idle', NULL, ?, ?, '', '{}')`,
		sessionID, crashSeedAgentID, crashSeedSnap, now, now,
	)
	if err != nil {
		t.Fatal(err)
	}
}

func seedSessionEvent(
	t *testing.T,
	db *sql.DB,
	sessionID string,
	seq int,
	eventType string,
	payload map[string]any,
) {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	eventID := fmt.Sprintf("evt_crash_%s_%d", sessionID, seq)
	_, err = db.Exec(`
		INSERT INTO session_events
		  (session_id, seq, event_id, type, payload, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		sessionID, seq, eventID, eventType, string(body), time.Now().UnixMilli(),
	)
	if err != nil {
		t.Fatal(err)
	}
}

func querySessionRow(
	t *testing.T,
	db *sql.DB,
	sessionID string,
) (status string, turnID sql.NullString) {
	t.Helper()
	err := db.QueryRow(`
		SELECT status, turn_id FROM sessions WHERE id = ?`, sessionID,
	).Scan(&status, &turnID)
	if err != nil {
		t.Fatal(err)
	}
	return status, turnID
}

func countRunningSessions(t *testing.T, db *sql.DB) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM sessions WHERE status = 'running'`,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func countSessionEvents(t *testing.T, db *sql.DB, sessionID string) int {
	t.Helper()
	var n int
	if err := db.QueryRow(
		`SELECT COUNT(*) FROM session_events WHERE session_id = ?`,
		sessionID,
	).Scan(&n); err != nil {
		t.Fatal(err)
	}
	return n
}

func dumpLogsOnFail(t *testing.T, h *processHandle) {
	t.Helper()
	if h != nil && h.logs.Len() > 0 {
		t.Logf("oma-server logs:\n%s", h.logs.String())
	}
}

func TestCrashRecoveryOrphanRunningToInterruptedOnRestart(t *testing.T) {
	dataDir := t.TempDir()
	h := startOmaServer(t, dataDir)
	t.Cleanup(func() { killHard(t, h) })
	now := time.Now().UnixMilli()
	withCrashDB(t, dataDir, func(db *sql.DB) {
		seedOrphanSession(t, db, "sess_orphan", "turn_dead", now-60_000)
	})
	killHard(t, h)

	h = startOmaServer(t, dataDir)
	withCrashDB(t, dataDir, func(db *sql.DB) {
		status, turnID := querySessionRow(t, db, "sess_orphan")
		if status != "interrupted" {
			dumpLogsOnFail(t, h)
			t.Fatalf("status=%q want interrupted", status)
		}
		if turnID.Valid {
			t.Fatalf("turn_id=%q want NULL", turnID.String)
		}
	})
}

func TestCrashRecoveryMultipleOrphansOneBootstrap(t *testing.T) {
	dataDir := t.TempDir()
	h := startOmaServer(t, dataDir)
	t.Cleanup(func() { killHard(t, h) })
	now := time.Now().UnixMilli()
	withCrashDB(t, dataDir, func(db *sql.DB) {
		for i := 0; i < 3; i++ {
			seedOrphanSession(
				t, db,
				fmt.Sprintf("sess_orphan_%d", i),
				fmt.Sprintf("turn_dead_%d", i),
				now-60_000,
			)
		}
	})
	killHard(t, h)

	h = startOmaServer(t, dataDir)
	withCrashDB(t, dataDir, func(db *sql.DB) {
		if n := countRunningSessions(t, db); n != 0 {
			dumpLogsOnFail(t, h)
			t.Fatalf("running=%d want 0", n)
		}
	})
}

func TestCrashRecoveryIdleSessionUnchanged(t *testing.T) {
	dataDir := t.TempDir()
	h := startOmaServer(t, dataDir)
	t.Cleanup(func() { killHard(t, h) })
	now := time.Now().UnixMilli()
	withCrashDB(t, dataDir, func(db *sql.DB) {
		seedIdleSession(t, db, "sess_clean", now)
	})
	killHard(t, h)

	h = startOmaServer(t, dataDir)
	withCrashDB(t, dataDir, func(db *sql.DB) {
		status, turnID := querySessionRow(t, db, "sess_clean")
		if status != "idle" {
			dumpLogsOnFail(t, h)
			t.Fatalf("status=%q want idle", status)
		}
		if turnID.Valid {
			t.Fatalf("turn_id=%q want NULL", turnID.String)
		}
	})
}

func TestCrashRecoveryStaleOrphanNoTimeCutoff(t *testing.T) {
	dataDir := t.TempDir()
	h := startOmaServer(t, dataDir)
	t.Cleanup(func() { killHard(t, h) })
	dayAgo := time.Now().Add(-24 * time.Hour).UnixMilli()
	withCrashDB(t, dataDir, func(db *sql.DB) {
		seedOrphanSession(t, db, "sess_ancient", "turn_ancient", dayAgo)
	})
	killHard(t, h)

	h = startOmaServer(t, dataDir)
	withCrashDB(t, dataDir, func(db *sql.DB) {
		status, turnID := querySessionRow(t, db, "sess_ancient")
		if status != "interrupted" {
			dumpLogsOnFail(t, h)
			t.Fatalf("status=%q want interrupted", status)
		}
		if turnID.Valid {
			t.Fatalf("turn_id=%q want NULL", turnID.String)
		}
	})
}

func TestCrashRecoveryOrphanToolUseEventsUnchanged(t *testing.T) {
	// P1: no recoverInterruptedState — dangling tool_use stays; session reconciled.
	dataDir := t.TempDir()
	h := startOmaServer(t, dataDir)
	t.Cleanup(func() { killHard(t, h) })
	now := time.Now().UnixMilli()
	withCrashDB(t, dataDir, func(db *sql.DB) {
		seedOrphanSession(t, db, "sess_tool_orphan", "turn_tool", now)
		seedSessionEvent(t, db, "sess_tool_orphan", 1, "agent.tool_use", map[string]any{
			"type":  "agent.tool_use",
			"id":    "use_dangling",
			"name":  "bash",
			"input": map[string]string{"command": "echo hi"},
		})
	})
	killHard(t, h)

	h = startOmaServer(t, dataDir)
	withCrashDB(t, dataDir, func(db *sql.DB) {
		status, turnID := querySessionRow(t, db, "sess_tool_orphan")
		if status != "interrupted" {
			dumpLogsOnFail(t, h)
			t.Fatalf("status=%q want interrupted", status)
		}
		if turnID.Valid {
			t.Fatalf("turn_id=%q want NULL", turnID.String)
		}
		if n := countSessionEvents(t, db, "sess_tool_orphan"); n != 1 {
			t.Fatalf("events=%d want 1 (no placeholder tool_result in P1)", n)
		}
	})
}

func TestCrashRecoveryMixedStateBootstrap(t *testing.T) {
	dataDir := t.TempDir()
	h := startOmaServer(t, dataDir)
	t.Cleanup(func() { killHard(t, h) })
	now := time.Now().UnixMilli()
	withCrashDB(t, dataDir, func(db *sql.DB) {
		seedIdleSession(t, db, "sess_clean", now)
		seedOrphanSession(t, db, "sess_orphan_a", "turn_a", now)
		seedOrphanSession(t, db, "sess_orphan_b", "turn_b", now)
		seedIdleSession(t, db, "sess_clean2", now)
	})
	killHard(t, h)

	h = startOmaServer(t, dataDir)
	withCrashDB(t, dataDir, func(db *sql.DB) {
		if n := countRunningSessions(t, db); n != 0 {
			dumpLogsOnFail(t, h)
			t.Fatalf("running=%d want 0", n)
		}
		for _, id := range []string{"sess_orphan_a", "sess_orphan_b"} {
			status, _ := querySessionRow(t, db, id)
			if status != "interrupted" {
				t.Fatalf("%s status=%q want interrupted", id, status)
			}
		}
		for _, id := range []string{"sess_clean", "sess_clean2"} {
			status, _ := querySessionRow(t, db, id)
			if status != "idle" {
				t.Fatalf("%s status=%q want idle", id, status)
			}
		}
	})
}

func TestCrashRecoveryDoubleRestartIdempotent(t *testing.T) {
	dataDir := t.TempDir()
	h := startOmaServer(t, dataDir)
	t.Cleanup(func() { killHard(t, h) })
	now := time.Now().UnixMilli()
	withCrashDB(t, dataDir, func(db *sql.DB) {
		seedOrphanSession(t, db, "sess_d1", "turn_d1", now)
		seedSessionEvent(t, db, "sess_d1", 1, "agent.tool_use", map[string]any{
			"type": "agent.tool_use",
			"id":   "u_d1",
			"name": "bash",
		})
	})
	killHard(t, h)

	h = startOmaServer(t, dataDir)
	withCrashDB(t, dataDir, func(db *sql.DB) {
		status, _ := querySessionRow(t, db, "sess_d1")
		if status != "interrupted" {
			dumpLogsOnFail(t, h)
			t.Fatalf("first restart status=%q want interrupted", status)
		}
	})
	killHard(t, h)

	h = startOmaServer(t, dataDir)
	withCrashDB(t, dataDir, func(db *sql.DB) {
		if n := countRunningSessions(t, db); n != 0 {
			dumpLogsOnFail(t, h)
			t.Fatalf("second restart running=%d want 0", n)
		}
		if n := countSessionEvents(t, db, "sess_d1"); n != 1 {
			t.Fatalf("events=%d want 1 (recovery must not re-inject)", n)
		}
	})
}

func crashHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

func crashPostJSON(
	t *testing.T,
	client *http.Client,
	url, body string,
	wantStatus int,
) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewBufferString(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "dev-key")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != wantStatus {
		t.Fatalf("POST %s status=%d want %d", url, resp.StatusCode, wantStatus)
	}
}

func crashCreateAgent(t *testing.T, client *http.Client, base string) string {
	t.Helper()
	body := `{"name":"crash-e2e","model":"claude-sonnet-4-20250514"}`
	req, err := http.NewRequest(
		http.MethodPost,
		base+"/v1/agents",
		bytes.NewBufferString(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "dev-key")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create agent status=%d", resp.StatusCode)
	}
	var agent map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&agent); err != nil {
		t.Fatal(err)
	}
	id, _ := agent["id"].(string)
	if id == "" {
		t.Fatalf("agent id=%v", agent["id"])
	}
	return id
}

func crashCreateSession(
	t *testing.T,
	client *http.Client,
	base, agentID string,
) string {
	t.Helper()
	body := `{"agent":"` + agentID + `","title":"crash-e2e"}`
	req, err := http.NewRequest(
		http.MethodPost,
		base+"/v1/sessions",
		bytes.NewBufferString(body),
	)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("x-api-key", "dev-key")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("create session status=%d", resp.StatusCode)
	}
	var sess map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&sess); err != nil {
		t.Fatal(err)
	}
	id, _ := sess["id"].(string)
	if id == "" {
		t.Fatalf("session id=%v", sess["id"])
	}
	return id
}

func TestCrashRecoverySessionUsableAfterRestart(t *testing.T) {
	dataDir := t.TempDir()
	h := startOmaServer(t, dataDir)
	t.Cleanup(func() { killHard(t, h) })
	base := h.baseURL()
	client := crashHTTPClient()

	agentID := crashCreateAgent(t, client, base)
	sessionID := crashCreateSession(t, client, base, agentID)
	now := time.Now().UnixMilli()
	withCrashDB(t, dataDir, func(db *sql.DB) {
		_, err := db.Exec(`
			UPDATE sessions SET status = 'running', turn_id = 'turn_crash', updated_at = ?
			WHERE id = ?`, now, sessionID,
		)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`PRAGMA wal_checkpoint(TRUNCATE)`); err != nil {
			t.Fatal(err)
		}
	})
	killHard(t, h)
	time.Sleep(300 * time.Millisecond)
	dbPath := filepath.Join(dataDir, "oma.db")
	for _, suffix := range []string{"-wal", "-shm"} {
		_ = os.Remove(dbPath + suffix)
	}

	h = startOmaServer(t, dataDir)
	base = h.baseURL()
	client = crashHTTPClient()

	withCrashDB(t, dataDir, func(db *sql.DB) {
		status, turnID := querySessionRow(t, db, sessionID)
		if status != "interrupted" {
			dumpLogsOnFail(t, h)
			t.Fatalf("status=%q want interrupted", status)
		}
		if turnID.Valid {
			t.Fatalf("turn_id=%q want NULL", turnID.String)
		}
	})

	eventsURL := base + "/v1/sessions/" + sessionID + "/events"
	crashPostJSON(t, client, eventsURL,
		`{"events":[{"type":"user.message","content":[{"type":"text","text":"after crash"}]}]}`,
		http.StatusAccepted,
	)

	deadline := time.Now().Add(5 * time.Second)
	var lastStatus string
	for time.Now().Before(deadline) {
		withCrashDB(t, dataDir, func(db *sql.DB) {
			lastStatus, _ = querySessionRow(t, db, sessionID)
		})
		if lastStatus == "idle" {
			return
		}
		time.Sleep(100 * time.Millisecond)
	}
	dumpLogsOnFail(t, h)
	t.Fatalf("session never returned to idle after post-crash message; status=%q",
		lastStatus)
}
