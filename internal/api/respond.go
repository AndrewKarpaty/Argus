package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"

	"github.com/AndrewKarpaty/Argus/internal/store"
)

// maxBodyBytes bounds request bodies. Incidents are small; anything larger is
// a mistake or an attack.
const maxBodyBytes = 1 << 20 // 1 MiB

// errorBody is the single error shape the API returns.
type errorBody struct {
	Error     string `json:"error"`
	Detail    string `json:"detail,omitempty"`
	RequestID string `json:"request_id,omitempty"`
}

func writeJSON(w http.ResponseWriter, r *http.Request, status int, payload any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if payload == nil || status == http.StatusNoContent {
		return
	}
	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	if err := enc.Encode(payload); err != nil {
		// The status line is already out; all that is left is to say so.
		loggerFrom(r).Error("encoding the response failed", "error", err)
	}
}

func writeText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = io.WriteString(w, body)
}

func writeError(w http.ResponseWriter, r *http.Request, status int, msg string, detail error) {
	body := errorBody{Error: msg, RequestID: RequestIDFrom(r.Context())}
	if detail != nil {
		body.Detail = detail.Error()
	}
	if status >= http.StatusInternalServerError {
		loggerFrom(r).Error("request failed", "status", status, "error", msg, "detail", body.Detail)
		// Do not leak internals to the caller; the log has the detail.
		body.Detail = ""
	}
	writeJSON(w, r, status, body)
}

// writeStoreError maps store errors onto status codes.
func writeStoreError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, store.ErrNotFound):
		writeError(w, r, http.StatusNotFound, "not found", nil)
	case errors.Is(err, store.ErrConflict):
		writeError(w, r, http.StatusConflict, "already exists", nil)
	default:
		writeError(w, r, http.StatusInternalServerError, "internal error", err)
	}
}

// decodeJSON reads a size-limited, strict JSON body.
func decodeJSON(w http.ResponseWriter, r *http.Request, dst any) error {
	r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
	dec := json.NewDecoder(r.Body)
	dec.DisallowUnknownFields()
	if err := dec.Decode(dst); err != nil {
		return fmt.Errorf("invalid JSON body: %w", err)
	}
	if err := dec.Decode(&struct{}{}); err != io.EOF {
		return errors.New("body must contain a single JSON object")
	}
	return nil
}

func loggerFrom(r *http.Request) *slog.Logger {
	if l := LoggerFrom(r.Context()); l != nil {
		return l
	}
	return slog.Default()
}
