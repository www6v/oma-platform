package runtime

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"github.com/open-ma/oma-building/internal/store"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512 * 1024
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool { return true },
}

// Room routes WebSocket traffic between one local daemon and harness peers.
type Room struct {
	runtimes *store.RuntimeRepo
	id       string
	userID   string

	mu                sync.Mutex
	daemon            *websocket.Conn
	harness           map[string]map[*websocket.Conn]struct{}
	sessionState      map[string]json.RawMessage
	acpSession        map[string]string
	sessionTenant     map[string]string
	authorizedTenants map[string]struct{}
}

func newRoom(
	runtimes *store.RuntimeRepo,
	runtimeID, userID string,
) *Room {
	return &Room{
		runtimes:          runtimes,
		id:                runtimeID,
		userID:            userID,
		harness:           make(map[string]map[*websocket.Conn]struct{}),
		sessionState:      make(map[string]json.RawMessage),
		acpSession:        make(map[string]string),
		sessionTenant:     make(map[string]string),
		authorizedTenants: nil,
	}
}

// AttachDaemon upgrades the request and binds the local runtime daemon.
func (room *Room) AttachDaemon(
	w http.ResponseWriter,
	req *http.Request,
) error {
	room.mu.Lock()
	if room.daemon != nil {
		if err := room.daemon.WriteControl(
			websocket.PingMessage, nil, time.Now().Add(writeWait),
		); err == nil {
			room.mu.Unlock()
			return ErrDaemonAlreadyAttached
		}
		_ = room.daemon.Close()
		room.daemon = nil
	}
	room.mu.Unlock()

	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		return err
	}
	conn.SetReadLimit(maxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(pongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(pongWait))
	})

	room.mu.Lock()
	room.daemon = conn
	room.mu.Unlock()

	now := time.Now().Unix()
	if err := room.runtimes.MarkRuntimeOnline(req.Context(), room.id, now); err != nil {
		log.Printf("runtime room mark online: %v", err)
	}
	_ = room.ensureAuthorizedTenants(req.Context())

	go room.readDaemon(conn)
	return nil
}

// AttachHarness upgrades a harness WebSocket for one session.
func (room *Room) AttachHarness(
	w http.ResponseWriter,
	req *http.Request,
	sessionID, harnessTenant string,
) error {
	if sessionID == "" {
		return ErrMissingSessionID
	}

	conn, err := upgrader.Upgrade(w, req, nil)
	if err != nil {
		return err
	}
	conn.SetReadLimit(maxMessageSize)

	room.mu.Lock()
	if room.harness[sessionID] == nil {
		room.harness[sessionID] = make(map[*websocket.Conn]struct{})
	}
	room.harness[sessionID][conn] = struct{}{}
	if harnessTenant != "" {
		room.sessionTenant[sessionID] = harnessTenant
	}
	daemonUp := room.daemon != nil
	replay, hasReplay := room.sessionState[sessionID]
	room.mu.Unlock()

	attached := map[string]any{
		"type":          "attached",
		"daemon_online": daemonUp,
	}
	_ = room.writeJSON(conn, attached)
	if hasReplay {
		_ = conn.WriteMessage(websocket.TextMessage, replay)
	}

	go room.readHarness(sessionID, conn)
	return nil
}

// RefreshAuthorizedTenants reloads tenant membership for validation.
func (room *Room) RefreshAuthorizedTenants(ctx context.Context) error {
	room.mu.Lock()
	room.authorizedTenants = nil
	room.mu.Unlock()
	return room.ensureAuthorizedTenants(ctx)
}

// SendToDaemon forwards a JSON message to the attached daemon.
func (room *Room) SendToDaemon(msg map[string]any) bool {
	payload, err := json.Marshal(msg)
	if err != nil {
		return false
	}
	room.mu.Lock()
	conn := room.daemon
	room.mu.Unlock()
	if conn == nil {
		return false
	}
	return conn.WriteMessage(websocket.TextMessage, payload) == nil
}

var (
	// ErrDaemonAlreadyAttached is returned when a daemon is already connected.
	ErrDaemonAlreadyAttached = errors.New("daemon already attached")
	// ErrMissingSessionID is returned when harness attach lacks session id.
	ErrMissingSessionID = errors.New("missing session id")
)

func (room *Room) readDaemon(conn *websocket.Conn) {
	defer func() {
		room.mu.Lock()
		if room.daemon == conn {
			room.daemon = nil
		}
		room.mu.Unlock()
		_ = conn.Close()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := room.runtimes.MarkRuntimeOffline(ctx, room.id); err != nil {
			log.Printf("runtime room mark offline: %v", err)
		}
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(pongWait))
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			continue
		}
		room.onDaemonMessage(conn, parsed)
	}
}

func (room *Room) readHarness(sessionID string, conn *websocket.Conn) {
	defer func() {
		room.mu.Lock()
		if peers, ok := room.harness[sessionID]; ok {
			delete(peers, conn)
			if len(peers) == 0 {
				delete(room.harness, sessionID)
				delete(room.sessionTenant, sessionID)
			}
		}
		room.mu.Unlock()
		_ = conn.Close()
	}()

	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var parsed map[string]any
		if err := json.Unmarshal(data, &parsed); err != nil {
			continue
		}
		room.onHarnessMessage(sessionID, parsed)
	}
}

func (room *Room) onDaemonMessage(
	conn *websocket.Conn,
	parsed map[string]any,
) {
	msgType, _ := parsed["type"].(string)
	ctx := context.Background()
	now := time.Now().Unix()

	switch msgType {
	case "hello":
		agentsJSON := "[]"
		if agents, ok := parsed["agents"].([]any); ok {
			if b, err := json.Marshal(agents); err == nil {
				agentsJSON = string(b)
			}
		}
		localSkillsJSON := "{}"
		if skills, ok := parsed["local_skills"].(map[string]any); ok {
			if b, err := json.Marshal(skills); err == nil {
				localSkillsJSON = string(b)
			}
		}
		version, _ := parsed["version"].(string)
		if version == "" {
			version = "unknown"
		}
		var hostname, osName *string
		if h, ok := parsed["hostname"].(string); ok && h != "" {
			hostname = &h
		}
		if o, ok := parsed["os"].(string); ok && o != "" {
			osName = &o
		}
		if err := room.runtimes.UpdateRuntimeHello(
			ctx, room.id, agentsJSON, version, localSkillsJSON,
			hostname, osName, now,
		); err != nil {
			log.Printf("runtime hello update: %v", err)
		}
		_ = room.writeJSON(conn, map[string]any{
			"type":       "welcome",
			"runtime_id": room.id,
		})
	case "ping":
		if err := room.runtimes.TouchRuntimeHeartbeat(ctx, room.id, now); err != nil {
			log.Printf("runtime ping update: %v", err)
		}
		_ = room.writeJSON(conn, map[string]any{"type": "pong"})
	default:
		if len(msgType) >= 8 && msgType[:8] == "session." {
			sid, _ := parsed["session_id"].(string)
			if sid == "" {
				return
			}
			if reported, ok := parsed["tenant_id"].(string); ok && reported != "" {
				if !room.tenantAllowed(ctx, sid, reported) {
					return
				}
			}
			if msgType == "session.ready" || msgType == "session.error" {
				if b, err := json.Marshal(parsed); err == nil {
					room.mu.Lock()
					room.sessionState[sid] = b
					room.mu.Unlock()
				}
			}
			if msgType == "session.ready" {
				if acpSID, ok := parsed["acp_session_id"].(string); ok && acpSID != "" {
					room.mu.Lock()
					room.acpSession[sid] = acpSID
					room.mu.Unlock()
				}
			}
			if msgType == "session.disposed" {
				room.mu.Lock()
				delete(room.sessionState, sid)
				delete(room.acpSession, sid)
				room.mu.Unlock()
			}
			room.broadcastToHarness(sid, parsed)
		}
	}
}

func (room *Room) onHarnessMessage(sessionID string, parsed map[string]any) {
	room.mu.Lock()
	daemon := room.daemon
	pinnedTenant := room.sessionTenant[sessionID]
	acpSID := room.acpSession[sessionID]
	room.mu.Unlock()

	if daemon == nil {
		room.broadcastToHarness(sessionID, map[string]any{
			"type":       "session.error",
			"session_id": sessionID,
			"message":    "runtime daemon offline",
		})
		return
	}

	out := make(map[string]any, len(parsed)+2)
	for k, v := range parsed {
		out[k] = v
	}
	out["session_id"] = sessionID
	if _, ok := out["tenant_id"]; !ok && pinnedTenant != "" {
		out["tenant_id"] = pinnedTenant
	}
	if msgType, _ := parsed["type"].(string); msgType == "session.start" {
		resume, _ := out["resume"].(map[string]any)
		hasACP := false
		if resume != nil {
			if existing, ok := resume["acp_session_id"].(string); ok && existing != "" {
				hasACP = true
			}
		}
		if !hasACP && acpSID != "" {
			out["resume"] = map[string]any{"acp_session_id": acpSID}
		}
	}
	payload, err := json.Marshal(out)
	if err != nil {
		return
	}
	_ = daemon.WriteMessage(websocket.TextMessage, payload)
}

func (room *Room) broadcastToHarness(sessionID string, msg map[string]any) {
	payload, err := json.Marshal(msg)
	if err != nil {
		return
	}
	room.mu.Lock()
	peers := room.harness[sessionID]
	conns := make([]*websocket.Conn, 0, len(peers))
	for c := range peers {
		conns = append(conns, c)
	}
	room.mu.Unlock()
	for _, c := range conns {
		_ = c.WriteMessage(websocket.TextMessage, payload)
	}
}

func (room *Room) tenantAllowed(
	ctx context.Context,
	sessionID, reported string,
) bool {
	if err := room.ensureAuthorizedTenants(ctx); err != nil {
		return true
	}
	room.mu.Lock()
	defer room.mu.Unlock()
	if room.authorizedTenants != nil {
		if _, ok := room.authorizedTenants[reported]; !ok {
			return false
		}
	}
	if pinned, ok := room.sessionTenant[sessionID]; ok && pinned != reported {
		return false
	}
	return true
}

func (room *Room) ensureAuthorizedTenants(ctx context.Context) error {
	room.mu.Lock()
	if room.authorizedTenants != nil {
		room.mu.Unlock()
		return nil
	}
	room.mu.Unlock()

	ids, err := room.runtimes.ListAuthorizedTenantIDs(ctx, room.id)
	if err != nil {
		return err
	}
	set := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		set[id] = struct{}{}
	}
	room.mu.Lock()
	room.authorizedTenants = set
	room.mu.Unlock()
	return nil
}

func (room *Room) writeJSON(conn *websocket.Conn, msg map[string]any) error {
	payload, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, payload)
}

// DaemonOnline reports whether a daemon WebSocket is connected.
func (room *Room) DaemonOnline() bool {
	room.mu.Lock()
	defer room.mu.Unlock()
	return room.daemon != nil
}
