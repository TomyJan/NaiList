package server

import (
	"crypto/subtle"
	"errors"
	"fmt"
	"github.com/go-chi/chi/v5"
	"github.com/jackc/pgx/v5"
	"golang.org/x/oauth2"
	"nailist/internal/accounts"
	"nailist/internal/logging"
	"net/http"
	"net/url"
	"time"
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
		authFailure(r, provider, "provider_config", "unconfigured", nil)
		respond(w, r, 404, map[string]string{"error": "登录方式未配置"})
		return
	}
	done := authFlowStage(r, provider, "bind", "binding_lookup")
	identities, err := (accounts.Store{DB: s.db}).List(logging.WithOperation(r.Context(), "account.identities.list"), user.ID)
	if err != nil {
		done("failed", "database_read_failed")
		failOperation(w, r, err, "auth.binding.lookup.failed", "database_read_failed", "stage", "binding_lookup", "provider", provider)
		return
	}
	for _, identity := range identities {
		if identity.ProviderID == provider {
			done("rejected", "already_bound")
			authFailure(r, provider, "binding_lookup", "already_bound", nil)
			respond(w, r, 409, map[string]string{"error": "此账号已绑定该登录方式"})
			return
		}
	}
	done("success", "binding_available", "identity_count", len(identities))
	s.beginAuth(w, r, provider, user.ID)
}
func (s *Server) beginAuth(w http.ResponseWriter, r *http.Request, provider string, bindUserID int64) {
	intent := "login"
	if bindUserID != 0 {
		intent = "bind"
		r = requestUser(r, bindUserID)
	}
	fail := func(code, stage string, err error) {
		logAuthFailure(r, provider, stage, code, err)
		if bindUserID != 0 {
			respond(w, r, 503, map[string]string{"error": "暂时无法发起绑定"})
		} else {
			http.Redirect(w, r, "/?auth_error="+code, http.StatusSeeOther)
		}
	}
	if !s.configured(provider) {
		fail("unconfigured", "provider_config", nil)
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
			fail("failed", "oidc_discovery", err)
			return
		}
		destination = config.AuthCodeURL(state, oauth2.SetAuthURLParam("nonce", nonce), oauth2.S256ChallengeOption(verifier))
	}
	sessionHash := ""
	if bindUserID != 0 {
		done := authFlowStage(r, provider, intent, "binding_session")
		cookie, err := r.Cookie("nailist_session")
		if err != nil {
			done("rejected", "session_missing")
			authFailure(r, provider, "binding_session", "session_missing", nil)
			respond(w, r, 401, map[string]string{"error": "请重新登录"})
			return
		}
		sessionHash = hashToken(cookie.Value)
		done("success", "initiating_session_recorded", "user_id", bindUserID)
	}
	if s.db != nil {
		done := authFlowStage(r, provider, intent, "auth_flow_cleanup")
		result, err := s.db.Exec(logging.WithOperation(r.Context(), "auth.flow.cleanup"), `DELETE FROM auth_flows WHERE expires_at<now()`)
		if err != nil {
			done("failed", "database_write_failed")
			fail("failed", "auth_flow_cleanup", err)
			return
		}
		done("success", "cleanup_completed", "deleted_flows", result.RowsAffected())
		done = authFlowStage(r, provider, intent, "auth_flow_create")
		if _, err := s.db.Exec(logging.WithOperation(r.Context(), "auth.flow.create"), `INSERT INTO auth_flows(state_hash,provider_id,bind_user_id,session_hash,nonce,verifier,expires_at) VALUES($1,$2,NULLIF($3,0),$4,$5,$6,now()+interval '10 minutes')`, hashToken(state), provider, bindUserID, sessionHash, nonce, verifier); err != nil {
			done("failed", "database_write_failed")
			fail("failed", "auth_flow_create", err)
			return
		}
		done("success", "flow_created", "ttl_seconds", 600)
	}
	s.cookie(w, stateCookie(provider), state, 600)
	requestResult(r, "success", "authorization_started")
	requestLogger(r).InfoContext(r.Context(), "已创建授权流程，等待登录服务回调", "component", "auth", "event", "auth.flow.started", "stage", "authorization_redirect", "outcome", "success", "reason", "authorization_started", "provider", provider, "intent", intent, "ttl_seconds", 600, "flow_persisted", s.db != nil)
	if bindUserID != 0 {
		respond(w, r, 200, map[string]string{"url": destination})
		return
	}
	http.Redirect(w, r, destination, http.StatusFound)
}
func (s *Server) completeAuth(w http.ResponseWriter, r *http.Request, provider string) {
	target := "/"
	fail := func(code, stage string, err error) {
		logAuthFailure(r, provider, stage, code, err)
		http.Redirect(w, r, target+"?auth_error="+code, http.StatusSeeOther)
	}
	reject := func(code, stage, reason string) {
		authFailure(r, provider, stage, reason, nil)
		http.Redirect(w, r, target+"?auth_error="+code, http.StatusSeeOther)
	}
	cookie, err := r.Cookie(stateCookie(provider))
	s.cookie(w, stateCookie(provider), "", -1)
	state := r.URL.Query().Get("state")
	done := authFlowStage(r, provider, "unknown", "state_validation")
	reason := ""
	switch {
	case err != nil:
		reason = "state_cookie_missing"
	case state == "":
		reason = "state_missing"
	case subtle.ConstantTimeCompare([]byte(cookie.Value), []byte(state)) != 1:
		reason = "state_mismatch"
	case s.db == nil:
		reason = "database_unavailable"
	case !s.configured(provider):
		reason = "provider_unconfigured"
	}
	if reason != "" {
		done("rejected", reason)
		reject("failed", "state_validation", reason)
		return
	}
	done("success", "state_valid")
	var bindUserID int64
	var sessionHash, nonce, verifier string
	done = authFlowStage(r, provider, "unknown", "auth_flow_consume")
	err = s.db.QueryRow(logging.WithOperation(r.Context(), "auth.flow.consume"), `DELETE FROM auth_flows WHERE state_hash=$1 AND provider_id=$2 AND expires_at>now() RETURNING COALESCE(bind_user_id,0),session_hash,nonce,verifier`, hashToken(state), provider).Scan(&bindUserID, &sessionHash, &nonce, &verifier)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			done("rejected", "flow_missing_expired_or_consumed")
			reject("failed", "auth_flow_consume", "flow_missing_expired_or_consumed")
		} else {
			done("failed", "database_read_failed")
			fail("failed", "auth_flow_consume", err)
		}
		return
	}
	intent := "login"
	if bindUserID != 0 {
		intent = "bind"
	}
	done("success", "flow_consumed", "intent", intent, "nonce_present", nonce != "", "verifier_present", verifier != "")
	if bindUserID != 0 {
		target = "/manage"
		done = authFlowStage(r, provider, intent, "binding_session")
		user, ok := r.Context().Value(userKey).(User)
		session, err := r.Cookie("nailist_session")
		if !ok || user.ID != bindUserID || err != nil || subtle.ConstantTimeCompare([]byte(hashToken(session.Value)), []byte(sessionHash)) != 1 {
			done("rejected", "session_changed")
			fail("session_changed", "binding_session", nil)
			return
		}
		done("success", "initiating_session_matches", "user_id", bindUserID)
	}
	code := r.URL.Query().Get("code")
	if code == "" || r.URL.Query().Get("error") != "" {
		reason := "authorization_code_missing"
		if r.URL.Query().Get("error") != "" {
			reason = "provider_authorization_rejected"
		}
		reject("failed", "authorization_response", reason)
		return
	}
	var identity accounts.Identity
	if provider == "qq" {
		identity, err = s.qqIdentity(r.Context(), code)
	} else {
		identity, err = s.oidcIdentity(r.Context(), code, nonce, verifier)
	}
	if err != nil {
		fail("failed", "identity_exchange", err)
		return
	}
	if bindUserID != 0 {
		// OAuth exchange is a network boundary: recheck that the initiating session is still live.
		var live bool
		done = authFlowStage(r, provider, intent, "binding_session_recheck")
		err = s.db.QueryRow(logging.WithOperation(r.Context(), "session.binding.recheck"), `SELECT EXISTS(SELECT 1 FROM sessions WHERE token_hash=$1 AND user_id=$2 AND expires_at>now())`, sessionHash, bindUserID).Scan(&live)
		if err != nil || !live {
			reason := "session_no_longer_live"
			outcome := "rejected"
			if err != nil {
				reason = "database_read_failed"
				outcome = "failed"
			}
			done(outcome, reason)
			fail("session_changed", "binding_session_recheck", err)
			return
		}
		done("success", "session_still_live", "user_id", bindUserID)
	}
	done = authFlowStage(r, provider, intent, "account_resolve")
	userID, err := (accounts.Store{DB: s.db}).Resolve(r.Context(), identity, bindUserID, s.cfg.QQUseUnionID)
	if err != nil {
		if errors.Is(err, accounts.ErrConflict) {
			done("rejected", "identity_conflict")
			fail("identity_conflict", "account_resolve", nil)
		} else if errors.Is(err, accounts.ErrAlreadyBound) {
			done("rejected", "already_bound")
			fail("already_bound", "account_resolve", nil)
		} else {
			done("failed", "account_resolution_failed")
			fail("failed", "account_resolve", err)
		}
		return
	}
	r = requestUser(r, userID)
	done("success", "account_resolved", "user_id", userID)
	if bindUserID != 0 {
		requestResult(r, "success", "identity_bound")
		requestLogger(r).InfoContext(r.Context(), "绑定登录方式成功", "component", "auth", "event", "auth.binding.completed", "stage", "complete", "outcome", "success", "reason", "identity_bound", "provider", provider, "user_id", userID, "intent", intent, "created_or_existing", "existing")
		http.Redirect(w, r, "/manage?bound=1", http.StatusSeeOther)
		return
	}
	session := randomToken()
	done = authFlowStage(r, provider, intent, "session_create")
	if _, err = s.db.Exec(logging.WithOperation(r.Context(), "session.create"), `INSERT INTO sessions(token_hash,user_id,csrf_token,expires_at) VALUES($1,$2,$3,now()+interval '7 days')`, hashToken(session), userID, randomToken()); err != nil {
		done("failed", "database_write_failed")
		fail("failed", "session_create", err)
		return
	}
	done("success", "session_created", "user_id", userID, "ttl_seconds", 7*24*3600)
	done = authFlowStage(r, provider, intent, "session_cleanup")
	result, err := s.db.Exec(logging.WithOperation(r.Context(), "session.cleanup"), `DELETE FROM sessions WHERE expires_at<now()`)
	if err != nil {
		done("failed", "database_write_failed")
		args := append([]any{"component", "auth", "event", "auth.session.cleanup.failed", "stage", "session_cleanup", "outcome", "degraded", "reason", "database_write_failed", "provider", provider}, logging.ErrorAttrs(fmt.Errorf("cleanup expired sessions: %w", err))...)
		requestLogger(r).WarnContext(r.Context(), "清理过期会话失败，本次登录会话已创建", args...)
	} else {
		done("success", "cleanup_completed", "deleted_sessions", result.RowsAffected())
	}
	s.cookie(w, "nailist_session", session, 7*24*3600)
	requestResult(r, "success", "logged_in")
	requestLogger(r).InfoContext(r.Context(), "登录成功，已签发登录会话", "component", "auth", "event", "auth.login.completed", "stage", "complete", "outcome", "success", "reason", "logged_in", "provider", provider, "user_id", userID, "intent", intent, "ttl_seconds", 7*24*3600)
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func authFlowStage(r *http.Request, provider, intent, stage string) func(string, string, ...any) {
	started := time.Now()
	logger := requestLogger(r).With("component", "auth", "provider", provider, "intent", intent, "stage", stage)
	logger.DebugContext(r.Context(), "开始处理授权流程阶段", "event", "auth.flow.stage.started", "outcome", "started")
	return func(outcome, reason string, fields ...any) {
		attrs := append([]any{"event", "auth.flow.stage.completed", "outcome", outcome, "reason", reason, "duration_ms", logging.DurationMS(started)}, fields...)
		logger.DebugContext(r.Context(), "授权流程阶段处理结束", attrs...)
	}
}

func logAuthFailure(r *http.Request, provider, stage, reason string, err error) {
	var upstream *authProviderError
	if errors.As(err, &upstream) {
		kind := "oidc"
		if provider == "qq" {
			kind = "qq"
		}
		stage, reason = kind+"_"+upstream.stage, upstream.reason
		if upstream.rejected {
			err = nil
		}
	} else if err != nil {
		switch stage {
		case "auth_flow_consume", "binding_session_recheck", "binding_lookup":
			reason = "database_read_failed"
		case "auth_flow_cleanup", "auth_flow_create", "session_create", "session_cleanup":
			reason = "database_write_failed"
		case "account_resolve":
			reason = "account_resolution_failed"
		default:
			if reason == "failed" {
				reason = stage + "_failed"
			}
		}
	}
	authFailure(r, provider, stage, reason, err)
}
