package server

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
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
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	if s.cfg.QQAppID == "" || s.cfg.QQAppKey == "" {
		http.Redirect(w, r, "/?auth_error=unconfigured", http.StatusSeeOther)
		return
	}
	state := randomToken()
	s.cookie(w, "qq_state", state, 600)
	q := url.Values{"response_type": {"code"}, "client_id": {s.cfg.QQAppID}, "redirect_uri": {s.cfg.PublicURL + "/auth/qq/callback"}, "state": {state}, "scope": {"get_user_info"}}
	http.Redirect(w, r, "https://graph.qq.com/oauth2.0/authorize?"+q.Encode(), http.StatusFound)
}
func (s *Server) callback(w http.ResponseWriter, r *http.Request) {
	fail := func() { http.Redirect(w, r, "/?auth_error=failed", http.StatusSeeOther) }
	state, err := r.Cookie("qq_state")
	s.cookie(w, "qq_state", "", -1)
	if err != nil || r.URL.Query().Get("state") == "" || subtle.ConstantTimeCompare([]byte(state.Value), []byte(r.URL.Query().Get("state"))) != 1 || r.URL.Query().Get("code") == "" || s.db == nil || s.cfg.QQAppID == "" {
		fail()
		return
	}
	q := url.Values{"grant_type": {"authorization_code"}, "client_id": {s.cfg.QQAppID}, "client_secret": {s.cfg.QQAppKey}, "code": {r.URL.Query().Get("code")}, "redirect_uri": {s.cfg.PublicURL + "/auth/qq/callback"}, "fmt": {"json"}}
	data, err := s.qqGet(r.Context(), "https://graph.qq.com/oauth2.0/token?"+q.Encode())
	if err != nil {
		fail()
		return
	}
	var token struct {
		AccessToken string `json:"access_token"`
	}
	if err = json.Unmarshal(data, &token); err != nil {
		v, _ := url.ParseQuery(string(data))
		token.AccessToken = v.Get("access_token")
	}
	if token.AccessToken == "" {
		fail()
		return
	}
	data, err = s.qqGet(r.Context(), "https://graph.qq.com/oauth2.0/me?"+url.Values{"access_token": {token.AccessToken}, "fmt": {"json"}}.Encode())
	if err != nil {
		fail()
		return
	}
	var identity struct {
		OpenID   string `json:"openid"`
		ClientID string `json:"client_id"`
	}
	if json.Unmarshal(data, &identity) != nil || identity.OpenID == "" || identity.ClientID != s.cfg.QQAppID {
		fail()
		return
	}
	data, err = s.qqGet(r.Context(), "https://graph.qq.com/user/get_user_info?"+url.Values{"access_token": {token.AccessToken}, "oauth_consumer_key": {s.cfg.QQAppID}, "openid": {identity.OpenID}}.Encode())
	if err != nil {
		fail()
		return
	}
	var profile struct {
		Ret         int    `json:"ret"`
		Nickname    string `json:"nickname"`
		Avatar      string `json:"figureurl_qq_2"`
		SmallAvatar string `json:"figureurl_qq_1"`
	}
	if json.Unmarshal(data, &profile) != nil || profile.Ret != 0 || profile.Nickname == "" {
		fail()
		return
	}
	if profile.Avatar == "" {
		profile.Avatar = profile.SmallAvatar
	}
	tx, err := s.db.Begin(r.Context())
	if err != nil {
		fail()
		return
	}
	defer tx.Rollback(r.Context())
	var id int64
	err = tx.QueryRow(r.Context(), `INSERT INTO users(qq_openid,nickname,avatar_url) VALUES($1,$2,$3) ON CONFLICT(qq_openid) DO UPDATE SET nickname=EXCLUDED.nickname,avatar_url=EXCLUDED.avatar_url RETURNING id`, identity.OpenID, profile.Nickname, profile.Avatar).Scan(&id)
	if err != nil {
		fail()
		return
	}
	session := randomToken()
	_, err = tx.Exec(r.Context(), `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,now()+interval '7 days')`, hashToken(session), id, randomToken())
	if err != nil {
		fail()
		return
	}
	_, err = tx.Exec(r.Context(), `DELETE FROM sessions WHERE expires_at<now()`)
	if err != nil {
		fail()
		return
	}
	if tx.Commit(r.Context()) != nil {
		fail()
		return
	}
	s.cookie(w, "nailist_session", session, 7*24*3600)
	http.Redirect(w, r, "/", http.StatusSeeOther)
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
