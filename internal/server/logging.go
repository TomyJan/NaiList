package server

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"log/slog"
	"nailist/internal/logging"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

type requestStateKey struct{}
type requestState struct {
	mu                      sync.Mutex
	logger                  *slog.Logger
	userID                  int64
	outcome, reason         string
	errorLogged, writeError bool
	contextError            error
	details                 []any
}

func requestLogger(r *http.Request) *slog.Logger {
	logger := logging.FromContext(r.Context())
	if state, ok := r.Context().Value(requestStateKey{}).(*requestState); ok {
		state.mu.Lock()
		logger = state.logger
		userID := state.userID
		state.mu.Unlock()
		if userID != 0 {
			logger = logger.With("user_id", userID)
		}
		return logger
	}
	if user, ok := r.Context().Value(userKey).(User); ok {
		logger = logger.With("user_id", user.ID)
	}
	return logger
}

func requestUser(r *http.Request, userID int64) *http.Request {
	if state, ok := r.Context().Value(requestStateKey{}).(*requestState); ok {
		state.mu.Lock()
		state.userID = userID
		state.mu.Unlock()
	}
	return r.WithContext(logging.WithLogger(r.Context(), requestLogger(r)))
}
func requestResult(r *http.Request, outcome, reason string) {
	if state, ok := r.Context().Value(requestStateKey{}).(*requestState); ok {
		state.mu.Lock()
		state.outcome = outcome
		state.reason = reason
		state.mu.Unlock()
	}
}
func requestDetails(r *http.Request, args ...any) {
	if state, ok := r.Context().Value(requestStateKey{}).(*requestState); ok {
		state.mu.Lock()
		state.details = append(state.details, args...)
		state.mu.Unlock()
	}
}
func requestErrorLogged(r *http.Request) {
	if state, ok := r.Context().Value(requestStateKey{}).(*requestState); ok {
		state.mu.Lock()
		state.errorLogged = true
		state.mu.Unlock()
	}
}

func requestOperation(r *http.Request) (string, string) {
	path := r.URL.Path
	switch {
	case path == "/api/admin/stats" && r.Method == "GET":
		return "admin.stats", "获取站点统计与每日新增趋势"
	case path == "/api/admin/users" && r.Method == "GET":
		return "admin.users.list", "获取全站账号与登录方式"
	case path == "/api/admin/groups" && r.Method == "GET":
		return "admin.groups.list", "获取全站奶群与归属"
	case strings.HasPrefix(path, "/api/admin/users/") && strings.Contains(path, "/identities/"):
		return "admin.identity.unbind", "管理员解绑登录方式"
	case strings.HasPrefix(path, "/api/admin/users/"):
		return "admin.user.delete", "管理员删除账号"
	case strings.HasPrefix(path, "/api/admin/groups/"):
		return "admin.group.update", "管理员更新奶群与归属"
	case path == "/api/groups" && r.Method == "GET":
		return "groups.list", "获取公开奶群列表"
	case path == "/api/my/groups":
		return "groups.mine", "获取本人奶群列表"
	case path == "/api/groups" && r.Method == "POST":
		return "groups.create", "添加奶群"
	case strings.HasPrefix(path, "/api/groups/") && r.Method == "PUT":
		return "groups.update", "修改奶群"
	case strings.HasPrefix(path, "/api/groups/") && r.Method == "DELETE":
		return "groups.delete", "删除奶群"
	case path == "/api/me":
		return "account.me", "获取当前账号及绑定信息"
	case path == "/api/auth/providers":
		return "auth.providers", "获取可用登录方式"
	case path == "/api/auth/logout":
		return "auth.logout", "退出登录"
	case strings.HasPrefix(path, "/api/auth/bind/"):
		return "auth.bind", "绑定登录身份"
	case strings.HasPrefix(path, "/auth/") && strings.HasSuffix(path, "/callback"):
		return "auth.callback", "处理登录服务回调"
	case strings.HasPrefix(path, "/auth/"):
		return "auth.begin", "发起登录授权"
	case path == "/api/health":
		return "health.check", "检查服务与数据库健康"
	case path == "/" || path == "/manage" || path == "/admin" || path == "/admin/" || path == "/admin/users" || path == "/admin/groups" || path == "/index.html":
		return "web.page", "加载应用页面"
	case strings.HasPrefix(path, "/api/"):
		return "http.unmatched", "访问未匹配接口"
	default:
		return "web.asset", "加载前端资源"
	}
}

func logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		// Caller-supplied IDs and forwarding headers are not trusted log identity.
		requestID := rand.Text()
		ctx := context.WithValue(r.Context(), middleware.RequestIDKey, requestID)
		operation, description := requestOperation(r)
		peer := r.RemoteAddr
		if host, _, err := net.SplitHostPort(peer); err == nil {
			peer = host
		}
		path := r.URL.Path
		if len(path) > 1024 {
			path = path[:1024] + "[truncated]"
		}
		logger := logging.FromContext(ctx).With("request_id", requestID, "operation", operation, "method", r.Method, "path", path, "peer_ip", peer)
		state := &requestState{logger: logger}
		if user, ok := ctx.Value(userKey).(User); ok {
			state.userID = user.ID
		}
		ctx = context.WithValue(ctx, requestStateKey{}, state)
		ctx = logging.WithOperation(logging.WithLogger(ctx, logger), operation)
		r = r.WithContext(ctx)
		w.Header().Set("X-Request-ID", requestID)
		wrapped := observeResponse(middleware.NewWrapResponseWriter(w, r.ProtoMajor), r)
		requestLogger(r).DebugContext(ctx, "开始处理请求："+description, "component", "http", "event", "http.request.started", "protocol", r.Proto, "request_bytes", r.ContentLength, "has_session_cookie", hasSessionCookie(r))
		defer func() {
			status := wrapped.Status()
			responseStarted := status != 0
			state.mu.Lock()
			outcome, reason, errorLogged, writeError, contextError := state.outcome, state.reason, state.errorLogged, state.writeError, state.contextError
			details := append([]any{}, state.details...)
			state.mu.Unlock()
			if status == 0 && outcome != "canceled" && outcome != "failed" {
				status = 200
			}
			if contextError == nil {
				contextError = r.Context().Err()
			}
			if outcome == "" {
				switch {
				case status >= 500:
					outcome = "failed"
				case status >= 400:
					outcome = "rejected"
				case status >= 300:
					outcome = "redirected"
				default:
					outcome = "success"
				}
			}
			if reason == "" {
				switch {
				case errors.Is(contextError, context.Canceled):
					reason = "client_canceled"
					outcome = "canceled"
				case errors.Is(contextError, context.DeadlineExceeded):
					reason = "deadline_exceeded"
					outcome = "timeout"
				case status == 429:
					reason = "concurrency_limit"
				case status == 405:
					reason = "method_not_allowed"
				case status == 404:
					reason = "not_found"
				case status >= 500:
					reason = "internal_failure"
				default:
					reason = "completed"
				}
			}
			level := slog.LevelInfo
			if operation == "health.check" || strings.HasPrefix(operation, "web.") {
				level = slog.LevelDebug
			}
			switch {
			case errors.Is(contextError, context.Canceled) || outcome == "canceled":
				level = slog.LevelInfo
			case outcome == "timeout":
				level = slog.LevelWarn
			case errorLogged:
				level = slog.LevelWarn
			case status >= 500 && !errorLogged:
				level = slog.LevelError
			case status >= 500 || status >= 400 && status != 404 || time.Since(started) >= time.Second || writeError:
				level = slog.LevelWarn
			}
			route := "unmatched"
			if rc := chi.RouteContext(ctx); rc != nil && rc.RoutePattern() != "" {
				route = rc.RoutePattern()
			}
			args := []any{"component", "http", "event", "http.request.completed", "route", route, "status", status, "outcome", outcome, "reason", reason, "duration_ms", logging.DurationMS(started), "response_bytes", wrapped.BytesWritten(), "response_started", responseStarted}
			args = append(args, details...)
			if time.Since(started) >= time.Second {
				args = append(args, "slow", true, "slow_threshold_ms", 1000)
			}
			requestLogger(r).Log(ctx, level, "请求结束："+description, args...)
		}()
		next.ServeHTTP(wrapped, r)
	})
}
func hasSessionCookie(r *http.Request) bool { _, err := r.Cookie("nailist_session"); return err == nil }

func requestTimeout(timeout time.Duration) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithTimeout(r.Context(), timeout)
			defer cancel()
			defer func() {
				if state, ok := ctx.Value(requestStateKey{}).(*requestState); ok {
					state.mu.Lock()
					state.contextError = ctx.Err()
					state.mu.Unlock()
				}
				if errors.Is(ctx.Err(), context.DeadlineExceeded) {
					requestResult(r, "timeout", "deadline_exceeded")
					if writer, ok := w.(middleware.WrapResponseWriter); ok && writer.Status() == 0 {
						w.WriteHeader(http.StatusGatewayTimeout)
					}
				}
			}()
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func recoverRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if value := recover(); value != nil {
				if value == http.ErrAbortHandler {
					requestResult(r, "canceled", "handler_aborted")
					panic(value)
				}
				cause, ok := value.(error)
				if !ok {
					cause = errors.New(fmt.Sprint(value))
				}
				requestResult(r, "failed", "panic")
				requestErrorLogged(r)
				args := append([]any{"component", "http", "event", "http.request.panic", "reason", "panic", "stack", string(debug.Stack())}, logging.ErrorAttrs(cause)...)
				requestLogger(r).ErrorContext(r.Context(), "处理请求发生未恢复异常，请根据堆栈定位", args...)
				if writer, ok := w.(middleware.WrapResponseWriter); !ok || writer.Status() == 0 {
					respond(w, r, 500, map[string]string{"error": "服务暂不可用"})
				}
			}
		}()
		next.ServeHTTP(w, r)
	})
}

func rejectRequest(w http.ResponseWriter, r *http.Request, status int, event, reason, message string, args ...any) {
	requestResult(r, "rejected", reason)
	level := slog.LevelWarn
	if status == 404 {
		level = slog.LevelDebug
	}
	component := strings.SplitN(event, ".", 2)[0]
	attrs := append([]any{"component", component, "event", event, "reason", reason, "status", status}, args...)
	requestLogger(r).Log(r.Context(), level, message, attrs...)
	respond(w, r, status, map[string]string{"error": message})
}

func authFailure(r *http.Request, provider, stage, reason string, err error) {
	level := slog.LevelWarn
	outcome := "rejected"
	if err != nil {
		outcome = "failed"
		switch logging.ErrorKind(err) {
		case "canceled":
			level = slog.LevelInfo
			outcome = "canceled"
			reason = "client_canceled"
		case "timeout":
			level = slog.LevelWarn
			outcome = "timeout"
			reason = "deadline_exceeded"
		default:
			level = slog.LevelError
			requestErrorLogged(r)
		}
	}
	requestResult(r, outcome, reason)
	args := []any{"component", "auth", "event", "auth.failed", "provider", provider, "stage", stage, "reason", reason, "outcome", outcome}
	args = append(args, logging.ErrorAttrs(err)...)
	requestLogger(r).Log(r.Context(), level, "登录或绑定未完成，检查失败阶段及原因", args...)
}
