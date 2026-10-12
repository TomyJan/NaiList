package server

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func requestLogger(r *http.Request) *slog.Logger {
	logger := slog.Default().With("request_id", middleware.GetReqID(r.Context()))
	if user, ok := r.Context().Value(userKey).(User); ok {
		logger = logger.With("user_id", user.ID)
	}
	return logger
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		w.Header().Set("X-Request-ID", middleware.GetReqID(r.Context()))
		wrapped := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
		defer func() {
			status := wrapped.Status()
			if status == 0 {
				status = http.StatusOK
			}
			duration := time.Since(started)
			level := slog.LevelInfo
			if !strings.HasPrefix(r.URL.Path, "/api/") && !strings.HasPrefix(r.URL.Path, "/auth/") || r.URL.Path == "/api/health" {
				level = slog.LevelDebug
			}
			switch {
			case status >= 500:
				level = slog.LevelError
			case status >= 400 || duration >= time.Second:
				level = slog.LevelWarn
			}
			requestLogger(r).Log(r.Context(), level, "HTTP 请求", "method", r.Method, "path", r.URL.Path, "status", status, "duration_ms", float64(duration.Microseconds())/1000, "bytes", wrapped.BytesWritten())
		}()
		next.ServeHTTP(wrapped, r)
	})
}

func recoverRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				if value == http.ErrAbortHandler {
					panic(value)
				}
				cause, ok := value.(error)
				if !ok {
					cause = errors.New(fmt.Sprint(value))
				}
				requestLogger(r).ErrorContext(r.Context(), "请求发生 panic", "error", cause, "stack", string(debug.Stack()))
				respond(w, r, http.StatusInternalServerError, map[string]string{"error": "服务暂不可用"})
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func authFailure(r *http.Request, provider, stage, reason string, err error) {
	level := slog.LevelWarn
	if err != nil && !errors.Is(err, context.Canceled) {
		level = slog.LevelError
	}
	args := []any{"provider", provider, "stage", stage, "reason", reason}
	if err != nil {
		args = append(args, "error", err)
	}
	requestLogger(r).Log(r.Context(), level, "登录或绑定失败", args...)
}
