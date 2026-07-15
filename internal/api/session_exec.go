package api

import (
	"context"
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/open-ma/oma-building/internal/sandbox"
	"github.com/open-ma/oma-building/internal/store"
)

type sessionExecRequest struct {
	Command   string `json:"command"`
	TimeoutMS int    `json:"timeout_ms"`
}

type sessionExecResponse struct {
	Output string `json:"output"`
}

func (h *sessionHandlers) handleSessionExec(
	w http.ResponseWriter,
	req *http.Request,
) {
	sess, ok := h.requireSession(w, req)
	if !ok {
		return
	}
	var body sessionExecRequest
	if err := json.NewDecoder(req.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid json")
		return
	}
	if body.Command == "" {
		writeError(w, http.StatusBadRequest, "command is required")
		return
	}
	if h.workdirs == nil {
		writeError(w, http.StatusInternalServerError, "workdirs not configured")
		return
	}
	timeout := 120 * time.Second
	if body.TimeoutMS > 0 {
		timeout = time.Duration(body.TimeoutMS) * time.Millisecond
	}
	workdirPath := filepath.Join(h.workdirs.BaseDir(), sess.ID)

	// Resolve the sandbox config from the session's bound Environment.
	// This mirrors Machine's turn-start resolution so /exec honours
	// per-env binding even when called outside a full RunTurn.
	//
	// When the resolver or the env repo isn't wired (tests, legacy
	// callers), fall back to the registry's global Config — identical
	// to pre-environment behaviour.
	var resolved sandbox.Config
	if h.workdirs.Sandbox != nil {
		if h.sandboxResolver != nil && h.environments != nil {
			envView := loadEnvViewForExec(
				req.Context(), tenantID(req),
				sess.EnvironmentID, h.environments,
			)
			resolved, _ = h.sandboxResolver.Resolve(envView)
		} else {
			resolved = h.workdirs.Sandbox.Config()
		}
	}

	var ex sandbox.Executor
	var err error
	switch {
	case h.workdirs.Sandbox == nil:
		ex = sandbox.NewLocalExecutor(workdirPath)
	case resolved.IsRemote():
		ex, err = h.workdirs.Sandbox.AcquireWith(
			req.Context(), resolved, sandbox.AcquireOpts{
				SessionID:   sess.ID,
				WorkdirPath: workdirPath,
				TenantID:    tenantID(req),
				MemoryRoot:  h.workdirs.MemoryRoot(),
				OutputsRoot: h.workdirs.OutputsRoot(),
			},
		)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	default:
		ex = h.workdirs.Sandbox.Get(sess.ID, workdirPath)
	}
	out, err := ex.Exec(req.Context(), body.Command, timeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sessionExecResponse{Output: out})
}

// loadEnvViewForExec projects a store.Environment into a
// sandbox.EnvironmentView. On any error it returns a view with empty
// ConfigJSON — the resolver treats that as "no environment config" and
// falls back to the global sandbox config. Mirrors
// session.Machine.loadEnvironmentView without pulling the Machine into
// the api package's dep graph.
func loadEnvViewForExec(
	ctx context.Context,
	tenantID, envID string,
	envs *store.EnvironmentRepo,
) *sandbox.EnvironmentView {
	out := &sandbox.EnvironmentView{ID: envID}
	if envID == "" || envs == nil {
		return out
	}
	env, err := envs.Get(ctx, tenantID, envID)
	if err != nil || env == nil {
		return out
	}
	out.ID = env.ID
	out.ConfigJSON = []byte(env.Config)
	return out
}
