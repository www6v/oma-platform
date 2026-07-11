package api

import (
	"encoding/json"
	"net/http"
	"path/filepath"
	"time"

	"github.com/open-ma/oma-building/internal/sandbox"
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
	var ex sandbox.Executor
	var err error
	if h.workdirs.Sandbox != nil && h.workdirs.Sandbox.Config().IsRemote() {
		ex, err = h.workdirs.Sandbox.Acquire(req.Context(), sandbox.AcquireOpts{
			SessionID:   sess.ID,
			WorkdirPath: workdirPath,
			TenantID:    tenantID(req),
			MemoryRoot:  h.workdirs.MemoryRoot(),
			OutputsRoot: h.workdirs.OutputsRoot(),
		})
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
	} else if h.workdirs.Sandbox != nil {
		ex = h.workdirs.Sandbox.Get(sess.ID, workdirPath)
	} else {
		ex = sandbox.NewLocalExecutor(workdirPath)
	}
	out, err := ex.Exec(req.Context(), body.Command, timeout)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, sessionExecResponse{Output: out})
}
