package server

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/jackc/pgx/v5/pgxpool"
	"io"
	"log/slog"
	"nailist/internal/groups"
	"nailist/internal/logging"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	PublicURL, QQAppID, QQAppKey            string
	Version, UmamiScriptURL, UmamiWebsiteID string
	QQUseUnionID                            bool
	OIDC                                    OIDCConfig
	AdminUserIDs                            map[int64]bool
}
type User struct {
	ID        int64  `json:"id"`
	Nickname  string `json:"nickname"`
	AvatarURL string `json:"avatarUrl"`
	CSRF      string `json:"csrfToken"`
}
type contextKey string

const userKey contextKey = "user"

type GroupStore interface {
	List(context.Context, int64) ([]groups.Group, error)
	Save(context.Context, int64, int64, groups.Group) (groups.Group, error)
	Delete(context.Context, int64, int64) error
}
type Server struct {
	cfg    Config
	db     *pgxpool.Pool
	store  GroupStore
	client *http.Client
	web    http.Handler
	csp    string
}

func New(cfg Config, db *pgxpool.Pool, store GroupStore) *Server {
	web, csp := newWebHandler(cfg)
	return &Server{cfg: cfg, db: db, store: store, client: &http.Client{Timeout: 15 * time.Second}, web: web, csp: csp}
}
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(logRequest)
	r.Use(recoverRequest)
	r.Use(requestTimeout(20 * time.Second))
	r.Use(s.headers)
	r.Use(s.session)
	r.Get("/api/groups", s.list)
	r.Get("/api/me", s.me)
	r.Get("/api/auth/providers", s.authProviders)
	r.Route("/api/admin", func(r chi.Router) {
		r.Use(s.requireUser)
		r.Use(s.requireAdmin)
		r.Get("/users", s.adminUsers)
		r.Get("/groups", s.adminGroups)
		r.Group(func(r chi.Router) {
			r.Use(s.protectWrite)
			r.Use(middleware.Throttle(32))
			r.Delete("/users/{id}/identities/{provider}", s.adminUnbind)
			r.Delete("/users/{id}", s.adminDeleteUser)
			r.Put("/groups/{id}", s.adminSaveGroup)
		})
	})
	r.Group(func(r chi.Router) {
		r.Use(s.requireUser)
		r.Get("/api/my/groups", s.mine)
		r.Group(func(r chi.Router) {
			r.Use(s.protectWrite)
			r.Use(middleware.Throttle(32))
			r.Post("/api/groups", s.save)
			r.Put("/api/groups/{id}", s.save)
			r.Delete("/api/groups/{id}", s.remove)
			r.Post("/api/auth/logout", s.logout)
			r.Post("/api/auth/bind/{provider}", s.bind)
		})
	})
	r.Group(func(r chi.Router) {
		r.Use(middleware.Throttle(16))
		r.Get("/auth/qq", s.login)
		r.Get("/auth/qq/callback", s.callback)
		r.Get("/auth/{provider}", s.providerLogin)
		r.Get("/auth/{provider}/callback", s.providerCallback)
	})
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		if s.db != nil {
			if err := s.db.Ping(r.Context()); err != nil {
				level, outcome, reason := slog.LevelError, "failed", "database_unavailable"
				switch logging.ErrorKind(err) {
				case "canceled":
					level, outcome, reason = slog.LevelInfo, "canceled", "client_canceled"
				case "timeout":
					level, outcome, reason = slog.LevelWarn, "timeout", "deadline_exceeded"
				default:
					requestErrorLogged(r)
				}
				requestResult(r, outcome, reason)
				requestLogger(r).Log(r.Context(), level, "健康检查未完成，请查看数据库错误及请求状态", append([]any{"component", "health", "event", "health.check.failed", "reason", reason, "outcome", outcome}, logging.ErrorAttrs(err)...)...)
				respond(w, r, 503, map[string]string{"error": "数据库不可用"})
				return
			}
		}
		requestLogger(r).DebugContext(r.Context(), "健康检查通过：HTTP 与数据库可用", "component", "health", "event", "health.check.completed", "database_checked", s.db != nil)
		respond(w, r, 200, map[string]string{"status": "ok"})
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			requestResult(r, "rejected", "route_not_found")
			requestLogger(r).DebugContext(r.Context(), "未找到对应 API 路由", "component", "http", "event", "http.route.unmatched", "reason", "route_not_found")
			respond(w, r, 404, map[string]string{"error": "接口不存在"})
			return
		}
		s.web.ServeHTTP(w, r)
	})
	return r
}
func respond(w http.ResponseWriter, r *http.Request, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		responseFailure(r, status, err)
	}
}
func failure(w http.ResponseWriter, r *http.Request, err error) {
	operation, _ := requestOperation(r)
	failOperation(w, r, err, operation+".failed", "store_failed")
}
func failOperation(w http.ResponseWriter, r *http.Request, err error, event, reason string, args ...any) {
	status := 500
	msg := "服务暂不可用"
	level := slog.LevelError
	outcome := "failed"
	switch {
	case errors.Is(err, groups.ErrNotFound):
		status = 404
		msg = err.Error()
		level = slog.LevelDebug
		outcome = "rejected"
		reason = "not_found_or_not_owned"
	case errors.Is(err, groups.ErrDuplicate):
		status = 409
		msg = err.Error()
		level = slog.LevelWarn
		outcome = "rejected"
		reason = "duplicate_qq_number"
	case errors.Is(err, context.Canceled):
		level = slog.LevelInfo
		reason = "client_canceled"
		outcome = "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		status = 504
		level = slog.LevelWarn
		reason = "deadline_exceeded"
		outcome = "timeout"
	}
	requestResult(r, outcome, reason)
	if level == slog.LevelError {
		requestErrorLogged(r)
	}
	operation, description := requestOperation(r)
	component := strings.SplitN(operation, ".", 2)[0]
	attrs := append([]any{"component", component, "event", event, "reason", reason, "outcome", outcome}, args...)
	attrs = append(attrs, logging.ErrorAttrs(err)...)
	requestLogger(r).Log(r.Context(), level, description+"未完成，请检查失败原因", attrs...)
	respond(w, r, status, map[string]string{"error": msg})
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	started := time.Now()
	requestLogger(r).DebugContext(r.Context(), "查询所有用户上传的有效奶群，不分页", "component", "groups", "event", "groups.list.started", "scope", "public")
	g, err := s.store.List(r.Context(), 0)
	if err != nil {
		failure(w, r, err)
		return
	}
	requestDetails(r, "count", len(g), "scope", "public")
	requestLogger(r).DebugContext(r.Context(), "已读取公开奶群列表", "component", "groups", "event", "groups.list.completed", "scope", "public", "count", len(g), "duration_ms", logging.DurationMS(started))
	respond(w, r, 200, g)
}
func (s *Server) mine(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(User)
	started := time.Now()
	requestLogger(r).DebugContext(r.Context(), "按当前账号查询本人有效奶群", "component", "groups", "event", "groups.list.started", "scope", "owner")
	g, err := s.store.List(r.Context(), u.ID)
	if err != nil {
		failure(w, r, err)
		return
	}
	requestDetails(r, "count", len(g), "scope", "owner")
	requestLogger(r).DebugContext(r.Context(), "已读取本人奶群列表", "component", "groups", "event", "groups.list.completed", "scope", "owner", "count", len(g), "duration_ms", logging.DurationMS(started))
	respond(w, r, 200, g)
}
func groupID(r *http.Request) (int64, error) { return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64) }
func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	var id int64
	if r.Method == "PUT" {
		var err error
		id, err = groupID(r)
		if err != nil || id < 1 {
			rejectRequest(w, r, 400, "groups.save.rejected", "invalid_group_id", "无效群 ID")
			return
		}
	}
	var in groups.Input
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		rejectRequest(w, r, 400, "groups.save.rejected", "invalid_json", "表单格式错误", "body_limit_bytes", 16384)
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		rejectRequest(w, r, 400, "groups.save.rejected", "trailing_json", "表单格式错误")
		return
	}
	g, err := groups.Normalize(in)
	if err != nil {
		rejectRequest(w, r, 400, "groups.save.rejected", "validation_failed", err.Error(), "group_id", id)
		return
	}
	u := r.Context().Value(userKey).(User)
	started := time.Now()
	requestLogger(r).DebugContext(r.Context(), "奶群表单已通过校验，开始持久化", "component", "groups", "event", "groups.save.started", "group_id", id, "organization", g.Organization, "qq_number", g.QQNumber, "keyword_count", len(g.Keywords), "link_format", joinLinkFormat(g.JoinURL))
	g, err = s.store.Save(r.Context(), u.ID, id, g)
	if err != nil {
		failOperation(w, r, err, "groups.save.failed", "store_failed", "group_id", id, "qq_number", in.QQNumber)
		return
	}
	status := 200
	if id == 0 {
		status = 201
	}
	action := "更新奶群"
	if id == 0 {
		action = "创建奶群"
	}
	requestDetails(r, "group_id", g.ID)
	requestLogger(r).InfoContext(r.Context(), action+"成功，数据库已保存", "component", "groups", "event", "groups.save.completed", "outcome", "success", "group_id", g.ID, "organization", g.Organization, "qq_number", g.QQNumber, "keyword_count", len(g.Keywords), "link_format", joinLinkFormat(g.JoinURL), "duration_ms", logging.DurationMS(started))
	respond(w, r, status, g)
}
func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	id, err := groupID(r)
	if err != nil || id < 1 {
		rejectRequest(w, r, 400, "groups.delete.rejected", "invalid_group_id", "无效群 ID")
		return
	}
	u := r.Context().Value(userKey).(User)
	started := time.Now()
	requestLogger(r).DebugContext(r.Context(), "按群 ID 和所属账号执行软删除", "component", "groups", "event", "groups.delete.started", "group_id", id)
	if err := s.store.Delete(r.Context(), u.ID, id); err != nil {
		failOperation(w, r, err, "groups.delete.failed", "store_failed", "group_id", id)
		return
	}
	requestDetails(r, "group_id", id)
	requestLogger(r).InfoContext(r.Context(), "奶群已软删除，将不再出现在公开列表", "component", "groups", "event", "groups.delete.completed", "outcome", "success", "group_id", id, "duration_ms", logging.DurationMS(started))
	w.WriteHeader(204)
}
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Value(userKey).(User); !ok {
			rejectRequest(w, r, 401, "auth.access.rejected", "authentication_required", "请先登录")
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) protectWrite(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u := r.Context().Value(userKey).(User)
		origin := r.Header.Get("Origin")
		expected, _ := url.Parse(s.cfg.PublicURL)
		reason := ""
		switch {
		case origin != "" && origin != expected.Scheme+"://"+expected.Host:
			reason = "origin_mismatch"
		case r.Header.Get("Sec-Fetch-Site") == "cross-site":
			reason = "cross_site_request"
		case subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(u.CSRF)) != 1:
			reason = "csrf_mismatch"
		}
		if reason != "" {
			rejectRequest(w, r, 403, "auth.write.rejected", reason, "登录校验失败，请刷新", "csrf_present", r.Header.Get("X-CSRF-Token") != "")
			return
		}
		requestLogger(r).DebugContext(r.Context(), "写入请求已通过来源和 CSRF 检查", "component", "auth", "event", "auth.write.verified")
		next.ServeHTTP(w, r)
	})
}

func joinLinkFormat(link string) string {
	if strings.HasPrefix(link, "https://qun.qq.com/") {
		return "legacy_share"
	}
	return "short"
}
func (s *Server) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", s.csp)
		if strings.HasPrefix(r.URL.Path, "/api") || strings.HasPrefix(r.URL.Path, "/auth") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
