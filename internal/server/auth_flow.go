package server

import (
	"crypto/subtle"
	"errors"
	"github.com/go-chi/chi/v5"
	"golang.org/x/oauth2"
	"nailist/internal/accounts"
	"net/http"
	"net/url"
)

func stateCookie(provider string) string {
	if provider == "qq" {
		return "qq_state"
	}
	return "oidc_state"
}
func (s *Server) login(w http.ResponseWriter, r *http.Request)    { s.beginAuth(w, r, "qq", 0) }
func (s *Server) callback(w http.ResponseWriter, r *http.Request) { s.completeAuth(w, r, "qq") }
func (s *Server) providerLogin(w http.ResponseWriter, r *http.Request) {
	s.beginAuth(w, r, chi.URLParam(r, "provider"), 0)
}
func (s *Server) providerCallback(w http.ResponseWriter, r *http.Request) {
	s.completeAuth(w, r, chi.URLParam(r, "provider"))
}
func (s *Server) bind(w http.ResponseWriter, r *http.Request) {
	user := r.Context().Value(userKey).(User)
	provider := chi.URLParam(r, "provider")
	if !s.configured(provider) {
		respond(w, 404, map[string]string{"error": "登录方式未配置"})
		return
	}
	identities, err := (accounts.Store{DB: s.db}).List(r.Context(), user.ID)
	if err != nil {
		failure(w, err)
		return
	}
	for _, identity := range identities {
		if identity.ProviderID == provider {
			respond(w, 409, map[string]string{"error": "此账号已绑定该登录方式"})
			return
		}
	}
	s.beginAuth(w, r, provider, user.ID)
}
func (s *Server) beginAuth(w http.ResponseWriter, r *http.Request, provider string, bindUserID int64) {
	fail := func(code string) {
		if bindUserID != 0 {
			respond(w, 503, map[string]string{"error": "暂时无法发起绑定"})
		} else {
			http.Redirect(w, r, "/?auth_error="+code, http.StatusSeeOther)
		}
	}
	if !s.configured(provider) {
		fail("unconfigured")
		return
	}
	state, nonce, verifier := randomToken(), randomToken(), oauth2.GenerateVerifier()
	var destination string
	if provider == "qq" {
		q := url.Values{"response_type": {"code"}, "client_id": {s.cfg.QQAppID}, "redirect_uri": {s.cfg.PublicURL + "/auth/qq/callback"}, "state": {state}, "scope": {"get_user_info"}}
		destination = "https://graph.qq.com/oauth2.0/authorize?" + q.Encode()
	} else {
		_, config, err := s.oidcClient(r.Context())
		if err != nil {
			fail("failed")
			return
		}
		destination = config.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce), oauth2.S256ChallengeOption(verifier))
	}
	sessionHash := ""
	if bindUserID != 0 {
		cookie, err := r.Cookie("nailist_session")
		if err != nil {
			respond(w, 401, map[string]string{"error": "请重新登录"})
			return
		}
		sessionHash = hashToken(cookie.Value)
	}
	if s.db != nil {
		if _, err := s.db.Exec(r.Context(), `DELETE FROM auth_flows WHERE expires_at<now()`); err != nil {
			fail("failed")
			return
		}
		if _, err := s.db.Exec(r.Context(), `INSERT INTO auth_flows(state_hash,provider_id,bind_user_id,session_hash,nonce,verifier,expires_at) VALUES($1,$2,NULLIF($3,0),$4,$5,$6,now()+interval '10 minutes')`, hashToken(state), provider, bindUserID, sessionHash, nonce, verifier); err != nil {
			fail("failed")
			return
		}
	}
	s.cookie(w, stateCookie(provider), state, 600)
	if bindUserID != 0 {
		respond(w, 200, map[string]string{"url": destination})
		return
	}
	http.Redirect(w, r, destination, http.StatusFound)
}
func (s *Server) completeAuth(w http.ResponseWriter, r *http.Request, provider string) {
	target := "/"
	fail := func(code string) { http.Redirect(w, r, target+"?auth_error="+code, http.StatusSeeOther) }
	cookie, err := r.Cookie(stateCookie(provider))
	s.cookie(w, stateCookie(provider), "", -1)
	state := r.URL.Query().Get("state")
	if err != nil || state == "" || subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1 || s.db == nil || !s.configured(provider) {
		fail("failed")
		return
	}
	var bindUserID int64
	var sessionHash, nonce, verifier string
	err = s.db.QueryRow(r.Context(), `DELETE FROM auth_flows WHERE state_hash=$1 AND provider_id=$2 AND expires_at>now() RETURNING COALESCE(bind_user_id,0),session_hash,nonce,verifier`, hashToken(state), provider).Scan(&bindUserID, &sessionHash, &nonce, &verifier)
	if err != nil {
		fail("failed")
		return
	}
	if bindUserID != 0 {
		target = "/manage"
		user, ok := r.Context().Value(userKey).(User)
		session, err := r.Cookie("nailist_session")
		if !ok || user.ID != bindUserID || err != nil || subtle.ConstantTimeCompare([]byte(hashToken(session.Value)), []byte(sessionHash)) != 1 {
			fail("session_changed")
			return
		}
	}
	code := r.URL.Query().Get("code")
	if code == "" || r.URL.Query().Get("error") != "" {
		fail("failed")
		return
	}
	var identity accounts.Identity
	if provider == "qq" {
		identity, err = s.qqIdentity(r.Context(), code)
	} else {
		identity, err = s.oidcIdentity(r.Context(), code, nonce, verifier)
	}
	if err != nil {
		fail("failed")
		return
	}
	if bindUserID != 0 {
		// OAuth exchange is a network boundary: recheck that the initiating session is still live.
		var live bool
		err = s.db.QueryRow(r.Context(), `SELECT EXISTS(SELECT 1 FROM sessions WHERE token_hash=$1 AND user_id=$2 AND expires_at>now())`, sessionHash, bindUserID).Scan(&live)
		if err != nil || !live {
			fail("session_changed")
			return
		}
	}
	userID, err := (accounts.Store{DB: s.db}).Resolve(r.Context(), identity, bindUserID, s.cfg.QQUseUnionID)
	if err != nil {
		if errors.Is(err, accounts.ErrConflict) {
			fail("identity_conflict")
		} else if errors.Is(err, accounts.ErrAlreadyBound) {
			fail("already_bound")
		} else {
			fail("failed")
		}
		return
	}
	if bindUserID != 0 {
		http.Redirect(w, r, "/manage?bound=1", http.StatusSeeOther)
		return
	}
	session := randomToken()
	if _, err = s.db.Exec(r.Context(), `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,now()+interval '7 days')`, hashToken(session), userID, randomToken()); err != nil {
		fail("failed")
		return
	}
	_, _ = s.db.Exec(r.Context(), `DELETE FROM sessions WHERE expires_at<now()`)
	s.cookie(w, "nailist_session", session, 7*24*3600)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}
