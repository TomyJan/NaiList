package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"io"
	"log/slog"
	"nailist/internal/logging"
	"net/http"
	"strings"
	"time"
)

func randomToken() string {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return base64.RawURLEncoding.EncodeToString(b)
}
func hashToken(s string) string { h := sha256.Sum256([]byte(s)); return hex.EncodeToString(h[:]) }
func (s *Server) cookie(w http.ResponseWriter, name, value string, maxAge int) {
	http.SetCookie(w, &http.Cookie{Name: name, Value: value, Path: "/", HttpOnly: true, Secure: strings.HasPrefix(s.cfg.PublicURL, "https://"), SameSite: http.SameSiteLaxMode, MaxAge: maxAge})
}
func (s *Server) session(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		started := time.Now()
		ctx := logging.WithOperation(r.Context(), "session.lookup")
		logger := requestLogger(r).With("component", "auth", "event", "auth.session.lookup", "stage", "session_lookup")
		c, cookieErr := r.Cookie("nailist_session")
		switch {
		case cookieErr != nil:
			logger.DebugContext(ctx, "请求未携带登录会话", "outcome", "anonymous", "reason", "cookie_missing")
		case s.db == nil:
			logger.DebugContext(ctx, "数据库未配置，无法读取登录会话", "outcome", "anonymous", "reason", "database_unavailable")
		default:
			var u User
			var valid bool
			err := s.db.QueryRow(ctx, `SELECT u.id,u.nickname,u.avatar_url,s.csrf_token,s.expires_at>now() FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1`, hashToken(c.Value)).Scan(&u.ID, &u.Nickname, &u.AvatarURL, &u.CSRF, &valid)
			switch {
			case err == nil && valid:
				r = r.WithContext(context.WithValue(r.Context(), userKey, u))
				r = requestUser(r, u.ID)
				logger.DebugContext(ctx, "登录会话有效，已关联当前账号", "outcome", "authenticated", "reason", "session_valid", "user_id", u.ID, "duration_ms", logging.DurationMS(started))
			case errors.Is(err, pgx.ErrNoRows):
				logger.DebugContext(ctx, "登录会话不存在", "outcome", "anonymous", "reason", "session_unknown", "duration_ms", logging.DurationMS(started))
			case err == nil:
				logger.DebugContext(ctx, "登录会话已过期", "outcome", "anonymous", "reason", "session_expired", "duration_ms", logging.DurationMS(started))
			default:
				level, outcome, reason := slog.LevelError, "failed", "database_read_failed"
				message := "读取登录会话失败，当前请求按匿名处理"
				switch logging.ErrorKind(err) {
				case "canceled":
					level, outcome, reason = slog.LevelInfo, "canceled", "client_canceled"
					message = "请求已取消，停止读取登录会话"
				case "timeout":
					level, outcome, reason = slog.LevelWarn, "timeout", "deadline_exceeded"
					message = "读取登录会话超时，当前请求按匿名处理"
				default:
					requestErrorLogged(r)
				}
				args := append([]any{"outcome", outcome, "reason", reason, "duration_ms", logging.DurationMS(started)}, logging.ErrorAttrs(err)...)
				logger.Log(ctx, level, message, args...)
			}
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) qqGet(ctx context.Context, address string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", address, nil)
	if err != nil {
		return nil, err
	}
	res, err := logging.TraceClient(s.client).Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("QQ 请求 %s 失败: HTTP %d", req.URL.Path, res.StatusCode)
	}
	return io.ReadAll(io.LimitReader(res.Body, 1024*1024))
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	logger := requestLogger(r).With("component", "auth", "stage", "session_revoke")
	logger.DebugContext(r.Context(), "开始撤销登录会话", "event", "auth.logout.started")
	var revoked int64
	if c, err := r.Cookie("nailist_session"); err == nil && s.db != nil {
		result, err := s.db.Exec(logging.WithOperation(r.Context(), "session.revoke"), `DELETE FROM sessions WHERE token_hash=$1`, hashToken(c.Value))
		if err != nil {
			failOperation(w, r, err, "auth.logout.failed", "database_write_failed", "stage", "session_revoke")
			return
		}
		revoked = result.RowsAffected()
	}
	requestResult(r, "success", "session_revoked")
	logger.InfoContext(r.Context(), "退出登录，已清除浏览器会话", "event", "auth.logout.completed", "outcome", "success", "revoked_sessions", revoked)
	s.cookie(w, "nailist_session", "", -1)
	w.WriteHeader(204)
}
