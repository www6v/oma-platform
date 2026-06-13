package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/runtime"
	"github.com/open-ma/oma-building/internal/store"
)

func mountRuntimeDaemonRoutes(r chi.Router, deps runtimesDeps) {
	if deps.Runtimes == nil || deps.ApiKeys == nil {
		return
	}

	r.Get("/_attach", func(w http.ResponseWriter, req *http.Request) {
		if !websocketUpgrade(req) {
			writeError(w, http.StatusBadRequest, "WebSocket only")
			return
		}
		auth, ok := authenticateRuntimeBearer(w, req, deps.Runtimes)
		if !ok {
			return
		}
		if deps.Rooms == nil {
			writeError(w, http.StatusServiceUnavailable, "runtime rooms not configured")
			return
		}
		room := deps.Rooms.Room(auth.RuntimeID, auth.UserID)
		if err := room.AttachDaemon(w, req); err != nil {
			if errors.Is(err, runtime.ErrDaemonAlreadyAttached) {
				writeError(w, http.StatusConflict, "daemon already attached")
			}
		}
	})

	r.Post("/exchange", func(w http.ResponseWriter, req *http.Request) {
		handleRuntimeExchange(w, req, deps)
	})

	r.Get("/me", func(w http.ResponseWriter, req *http.Request) {
		auth, ok := authenticateRuntimeBearer(w, req, deps.Runtimes)
		if !ok {
			return
		}
		rt, err := deps.Runtimes.GetRuntime(req.Context(), auth.RuntimeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if rt == nil {
			writeError(w, http.StatusNotFound, "runtime not found")
			return
		}
		tenants, err := deps.Runtimes.ListRuntimeTenantsForMe(
			req.Context(), auth.RuntimeID, auth.UserID,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		tenantWire := make([]map[string]any, 0, len(tenants))
		for _, t := range tenants {
			tenantWire = append(tenantWire, map[string]any{
				"id":   t.ID,
				"name": t.Name,
				"role": t.Role,
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"runtime": map[string]any{
				"id":              rt.ID,
				"machine_id":      rt.MachineID,
				"hostname":        rt.Hostname,
				"os":              rt.OS,
				"version":         rt.Version,
				"status":          rt.Status,
				"last_heartbeat":  rt.LastHeartbeat,
				"created_at":      rt.CreatedAt,
			},
			"tenants": tenantWire,
		})
	})

	r.Post("/{id}/refresh", func(w http.ResponseWriter, req *http.Request) {
		auth, ok := authenticateRuntimeBearer(w, req, deps.Runtimes)
		if !ok {
			return
		}
		runtimeID := chi.URLParam(req, "id")
		if auth.RuntimeID != runtimeID {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		handleRuntimeRefresh(w, req, deps, auth)
	})
}

type runtimeExchangeBody struct {
	Code        string `json:"code"`
	State       string `json:"state"`
	MachineID   string `json:"machine_id"`
	Hostname    string `json:"hostname"`
	OS          string `json:"os"`
	Version     string `json:"version"`
	MultiTenant *bool  `json:"multi_tenant"`
}

func handleRuntimeExchange(
	w http.ResponseWriter,
	req *http.Request,
	deps runtimesDeps,
) {
	var body runtimeExchangeBody
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON body")
		return
	}
	if body.Code == "" || body.State == "" || body.MachineID == "" ||
		body.Hostname == "" || body.OS == "" || body.Version == "" {
		writeError(
			w,
			http.StatusBadRequest,
			"code, state, machine_id, hostname, os, version all required",
		)
		return
	}

	now := time.Now().Unix()
	row, err := deps.Runtimes.GetConnectCode(req.Context(), body.Code)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		writeError(w, http.StatusBadRequest, "invalid code")
		return
	}
	if row.UsedAt != nil {
		writeError(w, http.StatusBadRequest, "code already used")
		return
	}
	if row.ExpiresAt < now {
		writeError(w, http.StatusBadRequest, "code expired")
		return
	}
	if row.State != body.State {
		writeError(w, http.StatusBadRequest, "state mismatch")
		return
	}
	if err := deps.Runtimes.MarkConnectCodeUsed(req.Context(), body.Code, now); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	runtimeID, exists, err := deps.Runtimes.FindRuntimeByUserMachine(
		req.Context(), row.UserID, body.MachineID,
	)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if exists {
		if err := deps.Runtimes.UpdateRuntimeMeta(
			req.Context(), runtimeID, body.Hostname, body.OS, body.Version,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else {
		runtimeID = newRuntimeID()
		if err := deps.Runtimes.InsertRuntime(req.Context(), &store.Runtime{
			ID:            runtimeID,
			OwnerUserID:   row.UserID,
			OwnerTenantID: row.TenantID,
			MachineID:     body.MachineID,
			Hostname:      body.Hostname,
			OS:            body.OS,
			Version:       body.Version,
			Status:        "offline",
			CreatedAt:     now,
		}); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	tokenPlain, err := generateRuntimeToken()
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	tokenHash := hashRuntimeToken(tokenPlain)
	if err := deps.Runtimes.InsertRuntimeToken(
		req.Context(), newRuntimeTokenID(), runtimeID, tokenHash, row.UserID, now,
	); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	memberships, err := listRuntimeMemberships(req.Context(), deps, row.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if len(memberships) == 0 {
		memberships = []store.TenantMembership{{
			TenantID: row.TenantID,
			Name:     row.TenantID,
			Role:     "owner",
		}}
	}

	mintedKeys := make(map[string]string, len(memberships))
	for _, m := range memberships {
		plain, keyID, err := mintRuntimeAgentKey(
			req.Context(), deps, row.UserID, m.TenantID, body.Hostname,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		mintedKeys[m.TenantID] = plain
		if err := deps.Runtimes.UpsertRuntimeTenant(
			req.Context(), runtimeID, m.TenantID, keyID, now,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	multiTenant := body.MultiTenant != nil && *body.MultiTenant
	if multiTenant {
		tenants := make([]map[string]any, 0, len(memberships))
		for _, m := range memberships {
			name := m.Name
			if name == "" {
				name = m.TenantID
			}
			tenants = append(tenants, map[string]any{
				"id":            m.TenantID,
				"name":          name,
				"role":          m.Role,
				"agent_api_key": mintedKeys[m.TenantID],
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"runtime_id": runtimeID,
			"token":      tokenPlain,
			"tenants":    tenants,
		})
		return
	}

	v1Key := mintedKeys[row.TenantID]
	if v1Key == "" {
		for _, plain := range mintedKeys {
			v1Key = plain
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"runtime_id":    runtimeID,
		"token":         tokenPlain,
		"agent_api_key": v1Key,
	})
}

func handleRuntimeRefresh(
	w http.ResponseWriter,
	req *http.Request,
	deps runtimesDeps,
	auth *store.RuntimeTokenAuth,
) {
	rt, err := deps.Runtimes.GetRuntime(req.Context(), auth.RuntimeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if rt == nil {
		writeError(w, http.StatusNotFound, "runtime not found")
		return
	}

	memberships, err := listRuntimeMemberships(req.Context(), deps, auth.UserID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	membershipByID := make(map[string]store.TenantMembership, len(memberships))
	for _, m := range memberships {
		membershipByID[m.TenantID] = m
	}

	liveRows, err := deps.Runtimes.ListLiveRuntimeTenants(req.Context(), auth.RuntimeID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	liveByID := make(map[string]string, len(liveRows))
	for _, row := range liveRows {
		liveByID[row.TenantID] = row.AgentAPIKeyID
	}

	var toAdd []store.TenantMembership
	for _, m := range memberships {
		if _, ok := liveByID[m.TenantID]; !ok {
			toAdd = append(toAdd, m)
		}
	}
	var toRevoke []store.RuntimeTenantRow
	for tid, akid := range liveByID {
		if _, ok := membershipByID[tid]; !ok {
			toRevoke = append(toRevoke, store.RuntimeTenantRow{
				TenantID:      tid,
				AgentAPIKeyID: akid,
			})
		}
	}

	now := time.Now().Unix()
	mintedKeys := make(map[string]string)

	for _, r := range toRevoke {
		_, _ = deps.ApiKeys.DeleteByID(req.Context(), r.AgentAPIKeyID)
		if err := deps.Runtimes.RevokeRuntimeTenant(
			req.Context(), auth.RuntimeID, r.TenantID, now,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	if deps.Rooms != nil {
		_ = deps.Rooms.Room(auth.RuntimeID, auth.UserID).
			RefreshAuthorizedTenants(req.Context())
	}

	var toRotate []store.TenantMembership
	for _, m := range memberships {
		if _, ok := liveByID[m.TenantID]; ok {
			toRotate = append(toRotate, m)
		}
	}
	for _, m := range toRotate {
		oldID := liveByID[m.TenantID]
		_, _ = deps.ApiKeys.DeleteByID(req.Context(), oldID)
		plain, keyID, err := mintRuntimeAgentKey(
			req.Context(), deps, auth.UserID, m.TenantID, rt.Hostname,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		mintedKeys[m.TenantID] = plain
		if err := deps.Runtimes.UpdateRuntimeTenantKey(
			req.Context(), auth.RuntimeID, m.TenantID, keyID,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	for _, m := range toAdd {
		plain, keyID, err := mintRuntimeAgentKey(
			req.Context(), deps, auth.UserID, m.TenantID, rt.Hostname,
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		mintedKeys[m.TenantID] = plain
		if err := deps.Runtimes.UpsertRuntimeTenant(
			req.Context(), auth.RuntimeID, m.TenantID, keyID, now,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}

	tenantWire := make([]map[string]any, 0, len(memberships))
	for _, m := range memberships {
		name := m.Name
		if name == "" {
			name = m.TenantID
		}
		tenantWire = append(tenantWire, map[string]any{
			"id":            m.TenantID,
			"name":          name,
			"role":          m.Role,
			"agent_api_key": mintedKeys[m.TenantID],
		})
	}
	added := make([]string, 0, len(toAdd))
	for _, m := range toAdd {
		added = append(added, m.TenantID)
	}
	revoked := make([]string, 0, len(toRevoke))
	for _, r := range toRevoke {
		revoked = append(revoked, r.TenantID)
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"tenants": tenantWire,
		"added":   added,
		"revoked": revoked,
	})
}

func listRuntimeMemberships(
	ctx context.Context,
	deps runtimesDeps,
	userID string,
) ([]store.TenantMembership, error) {
	if deps.Tenants == nil {
		return []store.TenantMembership{{
			TenantID: defaultTenant,
			Name:     "Default",
			Role:     "owner",
		}}, nil
	}
	return deps.Tenants.ListForUser(ctx, userID)
}

func authenticateRuntimeBearer(
	w http.ResponseWriter,
	req *http.Request,
	runtimes *store.RuntimeRepo,
) (*store.RuntimeTokenAuth, bool) {
	token := parseBearerToken(req.Header.Get("Authorization"))
	if token == "" || !strings.HasPrefix(token, "sk_machine_") {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	hash := hashRuntimeToken(token)
	auth, err := runtimes.AuthenticateToken(req.Context(), hash)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	if auth == nil {
		writeError(w, http.StatusUnauthorized, "unauthorized")
		return nil, false
	}
	runtimes.TouchTokenLastUsed(req.Context(), hash, time.Now().Unix())
	return auth, true
}

func parseBearerToken(header string) string {
	header = strings.TrimSpace(header)
	if strings.HasPrefix(strings.ToLower(header), "bearer ") {
		return strings.TrimSpace(header[7:])
	}
	return header
}

func websocketUpgrade(req *http.Request) bool {
	return strings.EqualFold(req.Header.Get("Upgrade"), "websocket")
}
