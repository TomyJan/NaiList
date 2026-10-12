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
	"log"
	"nailist/internal/groups"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	PublicURL, QQAppID, QQAppKey, WebDir string
	QQUseUnionID                         bool
	OIDC                                 OIDCConfig
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
}

func New(cfg Config, db *pgxpool.Pool, store GroupStore) *Server {
	return &Server{cfg: cfg, db: db, store: store, client: &http.Client{Timeout: 15 * time.Second}}
}
func (s *Server) Routes() http.Handler {
	r := chi.NewRouter()
	r.Use(middleware.Recoverer)
	r.Use(middleware.RequestID)
	r.Use(middleware.Timeout(20 * time.Second))
	r.Use(s.headers)
	r.Use(s.session)
	r.Get("/api/groups", s.list)
	r.Get("/api/me", s.me)
	r.Get("/api/auth/providers", s.authProviders)
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
				respond(w, 503, map[string]string{"error": "数据库不可用"})
				return
			}
		}
		respond(w, 200, map[string]string{"status": "ok"})
	})
	r.NotFound(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			respond(w, 404, map[string]string{"error": "接口不存在"})
			return
		}
		serveWeb(w, r, s.cfg.WebDir)
	})
	return r
}
func respond(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
func failure(w http.ResponseWriter, err error) {
	status := 500
	msg := "服务暂不可用"
	switch {
	case errors.Is(err, groups.ErrNotFound):
		status = 404
		msg = err.Error()
	case errors.Is(err, groups.ErrDuplicate):
		status = 409
		msg = err.Error()
	default:
		log.Printf("request error: %v", err)
	}
	respond(w, status, map[string]string{"error": msg})
}
func (s *Server) list(w http.ResponseWriter, r *http.Request) {
	g, err := s.store.List(r.Context(), 0)
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, g)
}
func (s *Server) mine(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(userKey).(User)
	g, err := s.store.List(r.Context(), u.ID)
	if err != nil {
		failure(w, err)
		return
	}
	respond(w, 200, g)
}
func groupID(r *http.Request) (int64, error) { return strconv.ParseInt(chi.URLParam(r, "id"), 10, 64) }
func (s *Server) save(w http.ResponseWriter, r *http.Request) {
	var id int64
	if r.Method == "PUT" {
		var err error
		id, err = groupID(r)
		if err != nil || id < 1 {
			respond(w, 400, map[string]string{"error": "无效群 ID"})
			return
		}
	}
	var in groups.Input
	r.Body = http.MaxBytesReader(w, r.Body, 16384)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&in); err != nil {
		respond(w, 400, map[string]string{"error": "表单格式错误"})
		return
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		respond(w, 400, map[string]string{"error": "表单格式错误"})
		return
	}
	g, err := groups.Normalize(in)
	if err != nil {
		respond(w, 400, map[string]string{"error": err.Error()})
		return
	}
	u := r.Context().Value(userKey).(User)
	g, err = s.store.Save(r.Context(), u.ID, id, g)
	if err != nil {
		failure(w, err)
		return
	}
	status := 200
	if id == 0 {
		status = 201
	}
	respond(w, status, g)
}
func (s *Server) remove(w http.ResponseWriter, r *http.Request) {
	id, err := groupID(r)
	if err != nil || id < 1 {
		respond(w, 400, map[string]string{"error": "无效群 ID"})
		return
	}
	u := r.Context().Value(userKey).(User)
	if err := s.store.Delete(r.Context(), u.ID, id); err != nil {
		failure(w, err)
		return
	}
	w.WriteHeader(204)
}
func (s *Server) requireUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if _, ok := r.Context().Value(userKey).(User); !ok {
			respond(w, 401, map[string]string{"error": "请先登录"})
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
		if (origin != "" && origin != expected.Scheme+"://"+expected.Host) || r.Header.Get("Sec-Fetch-Site") == "cross-site" || subtle.ConstantTimeCompare([]byte(r.Header.Get("X-CSRF-Token")), []byte(u.CSRF)) != 1 {
			respond(w, 403, map[string]string{"error": "登录校验失败，请刷新"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) headers(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self'; style-src 'self'; img-src 'self' https: data:; connect-src 'self'; frame-ancestors 'none'; base-uri 'self'; form-action 'self'")
		if strings.HasPrefix(r.URL.Path, "/api") || strings.HasPrefix(r.URL.Path, "/auth") {
			w.Header().Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}
