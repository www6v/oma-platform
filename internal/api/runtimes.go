package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/open-ma/oma-building/internal/store"
	"github.com/open-ma/oma-building/internal/runtime"
)

const connectRuntimeCodeTTL = 5 * time.Minute

type runtimesDeps struct {
	Runtimes       *store.RuntimeRepo
	ApiKeys        *store.ApiKeyRepo
	Tenants        *store.TenantRepo
	Rooms          *runtime.Registry
	InternalSecret string
}

func mountRuntimeRoutes(r chi.Router, deps runtimesDeps) {
	if deps.Runtimes == nil {
		return
	}

	r.Post("/connect-runtime", func(w http.ResponseWriter, req *http.Request) {
		uid := userID(req)
		tid := tenantID(req)
		if uid == "" || tid == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}

		var body struct {
			State string `json:"state"`
		}
		if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
			writeError(w, http.StatusBadRequest, "invalid JSON body")
			return
		}
		if len(body.State) < 8 {
			writeError(w, http.StatusBadRequest, "state required (>= 8 chars)")
			return
		}

		code, err := generateConnectCode()
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		expiresAt := time.Now().Unix() + int64(connectRuntimeCodeTTL/time.Second)
		if err := deps.Runtimes.InsertConnectCode(
			req.Context(), code, uid, tid, body.State, expiresAt,
		); err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"code":       code,
			"expires_at": expiresAt,
		})
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		uid := userID(req)
		if uid == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		list, err := deps.Runtimes.ListByOwnerUser(req.Context(), uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		out := make([]map[string]any, 0, len(list))
		for _, rt := range list {
			out = append(out, runtimeListWire(rt))
		}
		writeJSON(w, http.StatusOK, map[string]any{"runtimes": out})
	})

	r.Delete("/{id}", func(w http.ResponseWriter, req *http.Request) {
		uid := userID(req)
		if uid == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized")
			return
		}
		id := chi.URLParam(req, "id")
		ok, err := deps.Runtimes.OwnedByUser(req.Context(), id, uid)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		now := time.Now().Unix()
		deleted, err := deps.Runtimes.RevokeAndDeleteRuntime(req.Context(), id, now)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !deleted {
			writeError(w, http.StatusNotFound, "not found")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
	})
}

func runtimeListWire(rt store.Runtime) map[string]any {
	item := map[string]any{
		"id":         rt.ID,
		"machine_id": rt.MachineID,
		"hostname":   rt.Hostname,
		"os":         rt.OS,
		"agents":     safeJSONArray(rt.AgentsJSON),
		"local_skills": safeJSONObject(rt.LocalSkillsJSON),
		"version":    rt.Version,
		"status":     rt.Status,
		"created_at": rt.CreatedAt,
	}
	if rt.LastHeartbeat != nil {
		item["last_heartbeat"] = *rt.LastHeartbeat
	} else {
		item["last_heartbeat"] = nil
	}
	return item
}

func safeJSONArray(raw string) any {
	if raw == "" {
		return []any{}
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return []any{}
	}
	return v
}

func safeJSONObject(raw string) any {
	if raw == "" {
		return map[string]any{}
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return map[string]any{}
	}
	return v
}

func generateConnectCode() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

func generateRuntimeToken() (string, error) {
	b := make([]byte, 30)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return "sk_machine_" + hex.EncodeToString(b), nil
}

func hashRuntimeToken(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func mintRuntimeAgentKey(
	ctx context.Context,
	deps runtimesDeps,
	userID, tenantID, hostname string,
) (plain, id string, err error) {
	label := "Local runtime (" + hostname + ")"
	minted, err := deps.ApiKeys.Mint(ctx, tenantID, userID, label, "runtime")
	if err != nil {
		return "", "", err
	}
	return minted.Key, minted.ID, nil
}

func newRuntimeID() string {
	return uuid.NewString()
}

func newRuntimeTokenID() string {
	return "rtok_" + randomHex(16)
}

func randomHex(n int) string {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
