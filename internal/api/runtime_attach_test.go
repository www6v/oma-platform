package api_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"

	"github.com/open-ma/oma-building/internal/api"
	"github.com/open-ma/oma-building/internal/harness"
	"github.com/open-ma/oma-building/internal/store"
)

func TestRuntimeDaemonAttachHelloAndPing(t *testing.T) {
	handler, token, runtimeID := runtimeTestHandler(t)

	server := httptest.NewServer(handler)
	defer server.Close()

	conn := dialRuntimeAttach(t, server.URL, token)
	defer conn.Close()

	hello := map[string]any{
		"type":    "hello",
		"version": "0.1.0-test",
		"agents":  []any{},
	}
	if err := conn.WriteJSON(hello); err != nil {
		t.Fatal(err)
	}

	var welcome map[string]any
	if err := conn.ReadJSON(&welcome); err != nil {
		t.Fatal(err)
	}
	if welcome["type"] != "welcome" {
		t.Fatalf("welcome type=%v", welcome["type"])
	}
	if welcome["runtime_id"] != runtimeID {
		t.Fatalf("runtime_id=%v want %s", welcome["runtime_id"], runtimeID)
	}

	if err := conn.WriteJSON(map[string]any{"type": "ping"}); err != nil {
		t.Fatal(err)
	}
	var pong map[string]any
	if err := conn.ReadJSON(&pong); err != nil {
		t.Fatal(err)
	}
	if pong["type"] != "pong" {
		t.Fatalf("pong type=%v", pong["type"])
	}

	req := httptest.NewRequest(http.MethodGet, "/v1/runtimes", nil)
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Tenant-Id", "default")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("list status=%d", rec.Code)
	}
	var list map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &list)
	items := list["runtimes"].([]any)
	rt := items[0].(map[string]any)
	if rt["status"] != "online" {
		t.Fatalf("status=%v want online", rt["status"])
	}
}

func TestRuntimeDaemonAttachRejectsSecondConnection(t *testing.T) {
	handler, token, _ := runtimeTestHandler(t)
	server := httptest.NewServer(handler)
	defer server.Close()

	conn := dialRuntimeAttach(t, server.URL, token)
	defer conn.Close()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/agents/runtime/_attach"
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	_, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err == nil {
		t.Fatal("expected second attach to fail")
	}
	if resp == nil || resp.StatusCode != http.StatusConflict {
		t.Fatalf("status=%v err=%v", resp, err)
	}
}

func TestRuntimeHarnessRelayToDaemon(t *testing.T) {
	db, err := store.Open(":memory:")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = store.Close(db) })

	const internalSecret = "test-internal-secret"
	deps, _ := testRouterDeps(t, db, &harness.FakeClient{}, "")
	deps.InternalSecret = internalSecret
	handler := api.NewRouter(deps)
	token, runtimeID := exchangeRuntimeToken(t, handler)

	server := httptest.NewServer(handler)
	defer server.Close()

	daemon := dialRuntimeAttach(t, server.URL, token)
	defer daemon.Close()

	done := make(chan map[string]any, 1)
	go func() {
		var msg map[string]any
		if err := daemon.ReadJSON(&msg); err != nil {
			return
		}
		done <- msg
	}()

	wsURL := "ws" + strings.TrimPrefix(server.URL, "http") +
		"/v1/internal/runtimes/" + runtimeID + "/attach-harness"
	header := http.Header{}
	header.Set("x-internal-secret", internalSecret)
	header.Set("x-session-id", "sess-relay-1")
	header.Set("x-harness-tenant", "default")
	harnessConn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("harness dial: %v status=%v", err, resp)
	}
	defer harnessConn.Close()

	var attached map[string]any
	if err := harnessConn.ReadJSON(&attached); err != nil {
		t.Fatal(err)
	}
	if attached["type"] != "attached" || attached["daemon_online"] != true {
		t.Fatalf("attached=%v", attached)
	}

	prompt := map[string]any{
		"type":    "session.prompt",
		"turn_id": "turn-1",
		"text":    "hello from harness",
	}
	if err := harnessConn.WriteJSON(prompt); err != nil {
		t.Fatal(err)
	}

	select {
	case got := <-done:
		if got["type"] != "session.prompt" {
			t.Fatalf("type=%v", got["type"])
		}
		if got["session_id"] != "sess-relay-1" {
			t.Fatalf("session_id=%v", got["session_id"])
		}
		if got["tenant_id"] != "default" {
			t.Fatalf("tenant_id=%v", got["tenant_id"])
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not receive relayed prompt")
	}
}

func runtimeTestHandler(t *testing.T) (http.Handler, string, string) {
	t.Helper()
	handler := testRouter(t)
	token, runtimeID := exchangeRuntimeToken(t, handler)
	return handler, token, runtimeID
}

func exchangeRuntimeToken(t *testing.T, handler http.Handler) (string, string) {
	t.Helper()
	req := httptest.NewRequest(
		http.MethodPost,
		"/v1/runtimes/connect-runtime",
		strings.NewReader(`{"state":"state-attach-test"}`),
	)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-User-Id", "user-1")
	req.Header.Set("X-Tenant-Id", "default")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("connect status=%d body=%s", rec.Code, rec.Body.String())
	}
	var connect map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &connect)
	code := connect["code"].(string)

	exchangeBody := `{
		"code":"` + code + `",
		"state":"state-attach-test",
		"machine_id":"machine-attach",
		"hostname":"devbox",
		"os":"darwin",
		"version":"0.1.0"
	}`
	req = httptest.NewRequest(
		http.MethodPost,
		"/agents/runtime/exchange",
		strings.NewReader(exchangeBody),
	)
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("exchange status=%d body=%s", rec.Code, rec.Body.String())
	}
	var exchanged map[string]any
	_ = json.Unmarshal(rec.Body.Bytes(), &exchanged)
	return exchanged["token"].(string), exchanged["runtime_id"].(string)
}

func dialRuntimeAttach(
	t *testing.T,
	serverURL, token string,
) *websocket.Conn {
	t.Helper()
	wsURL := "ws" + strings.TrimPrefix(serverURL, "http") +
		"/agents/runtime/_attach"
	header := http.Header{}
	header.Set("Authorization", "Bearer "+token)
	conn, resp, err := websocket.DefaultDialer.Dial(wsURL, header)
	if err != nil {
		t.Fatalf("dial attach: %v status=%v", err, resp)
	}
	return conn
}
