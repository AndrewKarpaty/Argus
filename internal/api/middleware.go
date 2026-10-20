package api

import (
	"context"
	"log/slog"
	"net/http"
	"runtime/debug"
	"time"

	"github.com/AndrewKarpaty/Argus/internal/domain"
)

type ctxKey int

const (
	ctxKeyRequestID ctxKey = iota
	ctxKeyLogger
)

// RequestIDFrom returns the request id attached to ctx, if any.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(ctxKeyRequestID).(string)
	return id
}

// LoggerFrom returns the request-scoped logger attached to ctx, if any.
func LoggerFrom(ctx context.Context) *slog.Logger {
	l, _ := ctx.Value(ctxKeyLogger).(*slog.Logger)
	return l
}

// statusRecorder captures the status code for the access log.
type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (s *statusRecorder) WriteHeader(code int) {
	if s.status == 0 {
		s.status = code
	}
	s.ResponseWriter.WriteHeader(code)
}

func (s *statusRecorder) Write(b []byte) (int, error) {
	if s.status == 0 {
		s.status = http.StatusOK
	}
	n, err := s.ResponseWriter.Write(b)
	s.bytes += n
	return n, err
}

// Unwrap lets http.ResponseController reach the underlying writer, which keeps
// flushing and hijacking working through this middleware.
func (s *statusRecorder) Unwrap() http.ResponseWriter { return s.ResponseWriter }

// withObservability attaches a request id and a request-scoped logger, logs
// the outcome, and turns a panic into a 500 instead of a dropped connection.
func withObservability(log *slog.Logger, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()

		id := r.Header.Get("X-Request-Id")
		if id == "" {
			id = domain.NewID("req")
		}
		reqLog := log.With("request_id", id, "method", r.Method, "path", r.URL.Path)

		ctx := context.WithValue(r.Context(), ctxKeyRequestID, id)
		ctx = context.WithValue(ctx, ctxKeyLogger, reqLog)
		w.Header().Set("X-Request-Id", id)

		rec := &statusRecorder{ResponseWriter: w}
		defer func() {
			if v := recover(); v != nil {
				reqLog.Error("panic while handling request",
					"panic", v, "stack", string(debug.Stack()))
				if rec.status == 0 {
					writeError(rec, r, http.StatusInternalServerError, "internal error", nil)
				}
			}
			level := slog.LevelInfo
			if rec.status >= 500 {
				level = slog.LevelError
			}
			reqLog.Log(r.Context(), level, "request",
				"status", rec.status,
				"bytes", rec.bytes,
				"duration", time.Since(started).Round(time.Millisecond))
		}()

		next.ServeHTTP(rec, r.WithContext(ctx))
	})
}

// withTimeout bounds how long a handler may take. Investigations are started
// in the background precisely so that they are not subject to this.
func withTimeout(d time.Duration, next http.Handler) http.Handler {
	if d <= 0 {
		return next
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx, cancel := context.WithTimeout(r.Context(), d)
		defer cancel()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}
