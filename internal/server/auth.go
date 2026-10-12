package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"strings"
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
		if s.db != nil {
			if c, err := r.Cookie("nailist_session"); err == nil {
				var u User
				err = s.db.QueryRow(r.Context(), `SELECT u.id,u.nickname,u.avatar_url,s.csrf_token FROM sessions s JOIN users u ON u.id=s.user_id WHERE s.token_hash=$1 AND s.expires_at>now()`, hashToken(c.Value)).Scan(&u.ID, &u.Nickname, &u.AvatarURL, &u.CSRF)
				if err == nil {
					r = r.WithContext(context.WithValue(r.Context(), userKey, u))
				}
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
	res, err := s.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		return nil, fmt.Errorf("QQ request failed")
	}
	return io.ReadAll(io.LimitReader(res.Body, 1024*1024))
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if c, err := r.Cookie("nailist_session"); err == nil && s.db != nil {
		if _, err := s.db.Exec(r.Context(), `DELETE FROM sessions WHERE token_hash=$1`, hashToken(c.Value)); err != nil {
			failure(w, err)
			return
		}
	}
	s.cookie(w, "nailist_session", "", -1)
	w.WriteHeader(204)
}
