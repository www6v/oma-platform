package api

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"net/http"
)

type apiErrorBody struct {
	Type string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
	RequestID string `json:"request_id,omitempty"`
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

func writeAPIError(
	w http.ResponseWriter,
	status int,
	errType string,
	message string,
) {
	body := apiErrorBody{Type: "error"}
	body.Error.Type = errType
	body.Error.Message = message
	body.RequestID = newRequestID()
	writeJSON(w, status, body)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeAPIError(w, status, errorTypeForStatus(status), msg)
}

func errorTypeForStatus(status int) string {
	switch status {
	case http.StatusBadRequest:
		return "invalid_request_error"
	case http.StatusUnauthorized:
		return "authentication_error"
	case http.StatusForbidden:
		return "permission_denied"
	case http.StatusNotFound:
		return "not_found_error"
	case http.StatusConflict:
		return "conflict_error"
	default:
		return "api_error"
	}
}

func newRequestID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "req_unknown"
	}
	return "req_" + hex.EncodeToString(b[:])
}

func decodeJSON(r *http.Request, dst any) error {
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	return dec.Decode(dst)
}

const defaultTenant = "default"
