package api

import (
	"encoding/base64"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/sessionoutputs"
)

type filesDeps struct {
	Outputs *sessionoutputs.Store
}

func mountFileRoutes(r chi.Router, deps filesDeps) {
	if deps.Outputs == nil {
		r.Get("/", writeEmptyFilesList)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", handleStubNotFound)
			r.Get("/content", handleStubNotFound)
			r.Delete("/", handleStubNotImplemented)
		})
		return
	}

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		scopeID := req.URL.Query().Get("scope_id")
		limit := parseFilesLimit(req.URL.Query().Get("limit"))

		data := make([]map[string]any, 0)
		if scopeID != "" {
			files, err := deps.Outputs.List(tenantID(req), scopeID)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, file := range files {
				data = append(data, outputAsFileRecord(scopeID, file))
			}
		}

		hasMore := len(data) > limit
		if hasMore {
			data = data[:limit]
		}
		resp := map[string]any{
			"data":     data,
			"has_more": hasMore,
		}
		if len(data) > 0 {
			resp["first_id"] = data[0]["id"]
			resp["last_id"] = data[len(data)-1]["id"]
		}
		writeJSON(w, http.StatusOK, resp)
	})

	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			decoded := decodeOutputID(id)
			if decoded == nil {
				writeError(w, http.StatusNotFound, "File not found")
				return
			}
			files, err := deps.Outputs.List(
				tenantID(req), decoded.sessionID,
			)
			if err != nil {
				writeError(w, http.StatusInternalServerError, err.Error())
				return
			}
			for _, file := range files {
				if file.Filename != decoded.filename {
					continue
				}
				writeJSON(w, http.StatusOK, outputAsFileRecord(
					decoded.sessionID, file,
				))
				return
			}
			writeError(w, http.StatusNotFound, "File not found")
		})

		r.Get("/content", func(w http.ResponseWriter, req *http.Request) {
			id := chi.URLParam(req, "id")
			decoded := decodeOutputID(id)
			if decoded == nil {
				writeError(w, http.StatusNotFound, "File content not found")
				return
			}
			body, _, mediaType, err := deps.Outputs.Read(
				tenantID(req), decoded.sessionID, decoded.filename,
			)
			if err != nil {
				writeError(w, http.StatusNotFound, "File content not found")
				return
			}
			defer body.Close()
			w.Header().Set("Content-Type", mediaType)
			_, _ = io.Copy(w, body)
		})

		r.Delete("/", handleStubNotImplemented)
	})
}

func writeEmptyFilesList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"data":     []any{},
		"has_more": false,
	})
}

func parseFilesLimit(raw string) int {
	if raw == "" {
		return 100
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 1 {
		return 100
	}
	if n > 1000 {
		return 1000
	}
	return n
}

func outputAsFileRecord(
	sessionID string,
	file sessionoutputs.File,
) map[string]any {
	return map[string]any{
		"id":           encodeOutputID(sessionID, file.Filename),
		"type":         "file",
		"filename":     file.Filename,
		"media_type":   file.MediaType,
		"size_bytes":   file.SizeBytes,
		"created_at":   file.UploadedAt,
		"scope_id":     sessionID,
		"downloadable": true,
		"scope": map[string]any{
			"type": "session",
			"id":   sessionID,
		},
	}
}

func encodeOutputID(sessionID, filename string) string {
	b64 := base64.RawURLEncoding.EncodeToString([]byte(filename))
	return "out:" + sessionID + ":" + b64
}

type outputIDParts struct {
	sessionID string
	filename  string
}

func decodeOutputID(id string) *outputIDParts {
	if !strings.HasPrefix(id, "out:") {
		return nil
	}
	rest := id[len("out:"):]
	sep := strings.Index(rest, ":")
	if sep < 0 {
		return nil
	}
	sessionID := rest[:sep]
	b64 := rest[sep+1:]
	raw, err := base64.RawURLEncoding.DecodeString(b64)
	if err != nil {
		padded := b64 + strings.Repeat("=", (4-len(b64)%4)%4)
		raw, err = base64.URLEncoding.DecodeString(padded)
		if err != nil {
			return nil
		}
	}
	return &outputIDParts{
		sessionID: sessionID,
		filename:  string(raw),
	}
}
