package api

import (
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/open-ma/oma-building/internal/fileblob"
	"github.com/open-ma/oma-building/internal/sessionoutputs"
	"github.com/open-ma/oma-building/internal/store"
)

type filesDeps struct {
	Files   *store.FileRepo
	Blobs   *fileblob.Store
	Outputs *sessionoutputs.Store
}

func mountFileRoutes(r chi.Router, deps filesDeps) {
	if deps.Files == nil || deps.Blobs == nil {
		r.Get("/", writeEmptyFilesList)
		r.Post("/", handleStubNotImplemented)
		r.Route("/{id}", func(r chi.Router) {
			r.Get("/", handleStubNotFound)
			r.Get("/content", handleStubNotFound)
			r.Delete("/", handleStubNotImplemented)
		})
		return
	}

	r.Post("/", func(w http.ResponseWriter, req *http.Request) {
		handleFileUpload(w, req, deps)
	})

	r.Get("/", func(w http.ResponseWriter, req *http.Request) {
		handleFileList(w, req, deps)
	})

	r.Route("/{id}", func(r chi.Router) {
		r.Get("/", func(w http.ResponseWriter, req *http.Request) {
			handleFileGet(w, req, deps)
		})
		r.Get("/content", func(w http.ResponseWriter, req *http.Request) {
			handleFileContent(w, req, deps)
		})
		r.Delete("/", func(w http.ResponseWriter, req *http.Request) {
			handleFileDelete(w, req, deps)
		})
	})
}

func handleFileUpload(
	w http.ResponseWriter,
	req *http.Request,
	deps filesDeps,
) {
	tenant := tenantID(req)
	var (
		filename     string
		mediaType    string
		body         []byte
		scopeID      *string
		downloadable bool
	)

	contentType := req.Header.Get("Content-Type")
	if strings.Contains(contentType, "multipart/form-data") {
		if err := req.ParseMultipartForm(32 << 20); err != nil {
			writeError(w, http.StatusBadRequest, "invalid multipart form")
			return
		}
		file, header, err := req.FormFile("file")
		if err != nil {
			writeError(
				w, http.StatusBadRequest,
				"file field is required in multipart upload",
			)
			return
		}
		defer file.Close()
		filename = header.Filename
		mediaType = header.Header.Get("Content-Type")
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		body, err = io.ReadAll(file)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if sc := req.FormValue("scope_id"); sc != "" {
			scopeID = &sc
		}
		downloadable = parseDownloadable(req.FormValue("downloadable"))
	} else {
		var jsonBody struct {
			Filename     string  `json:"filename"`
			Content      *string `json:"content"`
			MediaType    string  `json:"media_type"`
			ScopeID      string  `json:"scope_id"`
			Encoding     string  `json:"encoding"`
			Downloadable *bool   `json:"downloadable"`
		}
		if err := json.NewDecoder(req.Body).Decode(&jsonBody); err != nil {
			writeError(w, http.StatusBadRequest, "invalid json")
			return
		}
		if jsonBody.Filename == "" || jsonBody.Content == nil {
			writeError(w, http.StatusBadRequest, "filename and content are required")
			return
		}
		filename = jsonBody.Filename
		mediaType = jsonBody.MediaType
		if mediaType == "" {
			mediaType = "application/octet-stream"
		}
		if jsonBody.ScopeID != "" {
			scopeID = &jsonBody.ScopeID
		}
		if jsonBody.Downloadable != nil {
			downloadable = *jsonBody.Downloadable
		}
		encoding := jsonBody.Encoding
		if encoding == "" {
			if strings.HasPrefix(mediaType, "text/") {
				encoding = "utf8"
			} else {
				encoding = "base64"
			}
		}
		var err error
		switch encoding {
		case "utf8":
			body = []byte(*jsonBody.Content)
		case "base64":
			body, err = base64.StdEncoding.DecodeString(*jsonBody.Content)
			if err != nil {
				writeError(w, http.StatusBadRequest, "invalid base64 content")
				return
			}
		default:
			writeError(w, http.StatusBadRequest, "invalid encoding")
			return
		}
	}

	fileID := store.NewFileID()
	blobKey, err := deps.Blobs.Write(tenant, fileID, body)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	row, err := deps.Files.Insert(req.Context(), store.CreateFileInput{
		ID:           fileID,
		TenantID:     tenant,
		SessionID:    scopeID,
		Filename:     filename,
		MediaType:    mediaType,
		SizeBytes:    int64(len(body)),
		Downloadable: downloadable,
		BlobKey:      blobKey,
	})
	if err != nil {
		_ = deps.Blobs.Delete(blobKey)
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, fileRowToRecord(*row))
}

func handleFileList(
	w http.ResponseWriter,
	req *http.Request,
	deps filesDeps,
) {
	tenant := tenantID(req)
	scopeID := req.URL.Query().Get("scope_id")
	limit := parseFilesLimit(req.URL.Query().Get("limit"))
	order := "desc"
	if req.URL.Query().Get("order") == "asc" {
		order = "asc"
	}

	opts := store.FileListOptions{
		BeforeID: req.URL.Query().Get("before_id"),
		AfterID:  req.URL.Query().Get("after_id"),
		Order:    order,
		Limit:    limit + 1,
	}
	if scopeID != "" {
		opts.SessionID = &scopeID
	}

	rows, err := deps.Files.List(req.Context(), tenant, opts)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	hasMore := len(rows) > limit
	if hasMore {
		rows = rows[:limit]
	}

	data := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		data = append(data, fileRowToRecord(row))
	}

	if scopeID != "" && deps.Outputs != nil {
		outputs, err := deps.Outputs.List(tenant, scopeID)
		if err != nil {
			writeError(w, http.StatusInternalServerError, err.Error())
			return
		}
		for _, file := range outputs {
			data = append(data, outputAsFileRecord(scopeID, file))
		}
		if len(outputs) >= 1000 {
			hasMore = true
		}
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
}

func handleFileGet(
	w http.ResponseWriter,
	req *http.Request,
	deps filesDeps,
) {
	id := chi.URLParam(req, "id")
	tenant := tenantID(req)

	if decoded := decodeOutputID(id); decoded != nil {
		handleOutputFileGet(w, req, deps, decoded)
		return
	}

	row, err := deps.Files.Get(req.Context(), tenant, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "File not found")
		return
	}
	writeJSON(w, http.StatusOK, fileRowToRecord(*row))
}

func handleFileContent(
	w http.ResponseWriter,
	req *http.Request,
	deps filesDeps,
) {
	id := chi.URLParam(req, "id")
	tenant := tenantID(req)

	if decoded := decodeOutputID(id); decoded != nil {
		body, _, mediaType, err := deps.Outputs.Read(
			tenant, decoded.sessionID, decoded.filename,
		)
		if err != nil {
			writeError(w, http.StatusNotFound, "File content not found")
			return
		}
		defer body.Close()
		w.Header().Set("Content-Type", mediaType)
		_, _ = io.Copy(w, body)
		return
	}

	row, err := deps.Files.Get(req.Context(), tenant, id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "File not found")
		return
	}
	if !row.Downloadable {
		writeError(w, http.StatusForbidden, "This file is not downloadable")
		return
	}

	bytes, err := deps.Blobs.Read(row.BlobKey)
	if err != nil {
		writeError(w, http.StatusNotFound, "File content not found")
		return
	}
	w.Header().Set("Content-Type", row.MediaType)
	_, _ = w.Write(bytes)
}

func handleFileDelete(
	w http.ResponseWriter,
	req *http.Request,
	deps filesDeps,
) {
	id := chi.URLParam(req, "id")
	if strings.HasPrefix(id, "out:") {
		writeError(w, http.StatusNotFound, "File not found")
		return
	}

	row, err := deps.Files.Delete(req.Context(), tenantID(req), id)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if row == nil {
		writeError(w, http.StatusNotFound, "File not found")
		return
	}
	_ = deps.Blobs.Delete(row.BlobKey)
	writeJSON(w, http.StatusOK, map[string]any{
		"type": "file_deleted",
		"id":   row.ID,
	})
}

func handleOutputFileGet(
	w http.ResponseWriter,
	req *http.Request,
	deps filesDeps,
	decoded *outputIDParts,
) {
	if deps.Outputs == nil {
		writeError(w, http.StatusNotFound, "File not found")
		return
	}
	files, err := deps.Outputs.List(tenantID(req), decoded.sessionID)
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
}

func fileRowToRecord(row store.FileRow) map[string]any {
	rec := map[string]any{
		"id":           row.ID,
		"type":         "file",
		"filename":     row.Filename,
		"media_type":   row.MediaType,
		"size_bytes":   row.SizeBytes,
		"downloadable": row.Downloadable,
		"created_at": time.UnixMilli(row.CreatedAt).UTC().Format(
			"2006-01-02T15:04:05.000Z",
		),
	}
	if row.SessionID.Valid {
		rec["scope_id"] = row.SessionID.String
	}
	return rec
}

func parseDownloadable(raw string) bool {
	return raw == "true" || raw == "1"
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

