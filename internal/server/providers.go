package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"nailist/internal/accounts"
	"nailist/internal/logging"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type OIDCConfig struct{ ID, Name, Icon, Issuer, ClientID, ClientSecret, Scopes string }
type LoginProvider struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Icon     string `json:"icon"`
	LoginURL string `json:"loginUrl"`
}

func (s *Server) providers() []LoginProvider {
	result := []LoginProvider{}
	if s.cfg.QQAppID != "" && s.cfg.QQAppKey != "" {
		result = append(result, LoginProvider{ID: "qq", Name: "QQ", LoginURL: "/auth/qq"})
	}
	c := s.cfg.OIDC
	if c.ID != "" && c.Issuer != "" && c.ClientID != "" {
		result = append(result, LoginProvider{ID: c.ID, Name: c.Name, Icon: c.Icon, LoginURL: "/auth/" + c.ID})
	}
	return result
}
func ValidateAuthConfig(cfg Config) error {
	c := cfg.OIDC
	if c.ID == "" && c.Name == "" && c.Icon == "" && c.Issuer == "" && c.ClientID == "" && c.ClientSecret == "" && c.Scopes == "" {
		return nil
	}
	if !regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`).MatchString(c.ID) || c.ID == "qq" {
		return errors.New("OIDC_ID 必须为非 qq 的小写标识")
	}
	if c.Name == "" || c.ClientID == "" {
		return errors.New("OIDC_NAME 和 OIDC_CLIENT_ID 必填")
	}
	u, err := url.Parse(c.Issuer)
	if err != nil || u.Host == "" || u.Scheme != "https" || u.RawQuery != "" || u.Fragment != "" || u.User != nil {
		return errors.New("OIDC_ISSUER 必须为 HTTPS issuer 地址")
	}
	if c.Icon != "" {
		u, err = url.Parse(c.Icon)
		if err != nil || !(u.IsAbs() && u.Scheme == "https" && u.Host != "" && u.User == nil || strings.HasPrefix(c.Icon, "/") && !strings.HasPrefix(c.Icon, "//") && u.Host == "") {
			return errors.New("OIDC_ICON 必须为 HTTPS 或站内图标路径")
		}
	}
	return nil
}
func (s *Server) authProviders(w http.ResponseWriter, r *http.Request) {
	respond(w, r, 200, s.providers())
}
func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	user, ok := r.Context().Value(userKey).(User)
	if !ok {
		respond(w, r, 200, nil)
		return
	}
	identities, err := (accounts.Store{DB: s.db}).List(logging.WithOperation(r.Context(), "account.identities.list"), user.ID)
	if err != nil {
		failOperation(w, r, err, "account.identities.failed", "database_read_failed", "stage", "identities_list")
		return
	}
	respond(w, r, 200, struct {
		User
		IsAdmin    bool                `json:"isAdmin"`
		Identities []accounts.Identity `json:"identities"`
	}{user, s.cfg.AdminUserIDs[user.ID], identities})
}
func (s *Server) configured(id string) bool {
	for _, p := range s.providers() {
		if p.ID == id {
			return true
		}
	}
	return false
}
func (s *Server) oidcClient(ctx context.Context) (*oidc.Provider, *oauth2.Config, error) {
	ctx = logging.TrackUpstreamErrors(ctx)
	done := providerStage(ctx, s.cfg.OIDC.ID, "discovery")
	ctx = oidc.ClientContext(ctx, logging.TraceClient(s.client))
	provider, err := oidc.NewProvider(ctx, s.cfg.OIDC.Issuer)
	if err != nil {
		return nil, nil, done("discovery_failed", err, false)
	}
	var metadata struct {
		JWKS string `json:"jwks_uri"`
	}
	if err = provider.Claims(&metadata); err != nil {
		return nil, nil, done("metadata_decode_failed", err, false)
	}
	for _, address := range []string{provider.Endpoint().AuthURL, provider.Endpoint().TokenURL, metadata.JWKS} {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return nil, nil, done("endpoint_not_https", nil, true)
		}
	}
	scopes := []string{oidc.ScopeOpenID, "profile"}
	for _, scope := range strings.Fields(s.cfg.OIDC.Scopes) {
		found := false
		for _, existing := range scopes {
			if existing == scope {
				found = true
			}
		}
		if !found {
			scopes = append(scopes, scope)
		}
	}
	config := &oauth2.Config{ClientID: s.cfg.OIDC.ClientID, ClientSecret: s.cfg.OIDC.ClientSecret, Endpoint: provider.Endpoint(), RedirectURL: s.cfg.PublicURL + "/auth/" + s.cfg.OIDC.ID + "/callback", Scopes: scopes}
	done("", nil, false, "authorization_endpoint_present", config.Endpoint.AuthURL != "", "token_endpoint_present", config.Endpoint.TokenURL != "", "jwks_endpoint_present", metadata.JWKS != "", "scope_count", len(scopes))
	return provider, config, nil
}
func (s *Server) qqIdentity(ctx context.Context, code string) (accounts.Identity, error) {
	identity := accounts.Identity{ProviderID: "qq", Issuer: s.cfg.QQAppID}
	done := providerStage(ctx, "qq", "token")
	q := url.Values{"grant_type": {"authorization_code"}, "client_id": {s.cfg.QQAppID}, "client_secret": {s.cfg.QQAppKey}, "code": {code}, "redirect_uri": {s.cfg.PublicURL + "/auth/qq/callback"}, "fmt": {"json"}}
	data, err := s.qqGet(ctx, "https://graph.qq.com/oauth2.0/token?"+q.Encode())
	if err != nil {
		return identity, done("token_request_failed", err, false)
	}
	var token struct {
		AccessToken string `json:"access_token"`
		Error       int    `json:"error"`
	}
	if json.Unmarshal(data, &token) != nil {
		v, _ := url.ParseQuery(string(data))
		token.AccessToken = v.Get("access_token")
	}
	if token.AccessToken == "" {
		return identity, done("access_token_missing", nil, true, "access_token_present", false, "provider_error_code", token.Error)
	}
	done("", nil, false, "access_token_present", true, "provider_error_code", token.Error)
	done = providerStage(ctx, "qq", "identity")
	q = url.Values{"access_token": {token.AccessToken}, "fmt": {"json"}}
	if s.cfg.QQUseUnionID {
		q.Set("unionid", "1")
	}
	data, err = s.qqGet(ctx, "https://graph.qq.com/oauth2.0/me?"+q.Encode())
	if err != nil {
		return identity, done("identity_request_failed", err, false)
	}
	var info struct {
		Error    int    `json:"error"`
		OpenID   string `json:"openid"`
		ClientID string `json:"client_id"`
		UnionID  string `json:"unionid"`
	}
	decodeErr := json.Unmarshal(data, &info)
	infoAttrs := []any{"provider_error_code", info.Error, "subject_present", info.OpenID != "", "client_id_present", info.ClientID != "", "client_id_matches", info.ClientID == s.cfg.QQAppID, "union_id_present", info.UnionID != "", "union_id_required", s.cfg.QQUseUnionID}
	reason := ""
	switch {
	case decodeErr != nil:
		reason = "identity_decode_failed"
	case info.OpenID == "":
		reason = "subject_missing"
	case info.ClientID != s.cfg.QQAppID:
		reason = "client_id_mismatch"
	case s.cfg.QQUseUnionID && info.UnionID == "":
		reason = "union_id_missing"
	}
	if reason != "" {
		return identity, done(reason, decodeErr, true, infoAttrs...)
	}
	done("", nil, false, infoAttrs...)
	identity.Subject = info.OpenID
	identity.UnionID = info.UnionID
	done = providerStage(ctx, "qq", "profile")
	data, err = s.qqGet(ctx, "https://graph.qq.com/user/get_user_info?"+url.Values{"access_token": {token.AccessToken}, "oauth_consumer_key": {s.cfg.QQAppID}, "openid": {info.OpenID}}.Encode())
	if err != nil {
		return identity, done("profile_request_failed", err, false)
	}
	var profile struct {
		Ret         int    `json:"ret"`
		Nickname    string `json:"nickname"`
		Avatar      string `json:"figureurl_qq_2"`
		SmallAvatar string `json:"figureurl_qq_1"`
	}
	decodeErr = json.Unmarshal(data, &profile)
	profileAttrs := []any{"provider_error_code", profile.Ret, "nickname_present", profile.Nickname != "", "avatar_present", profile.Avatar != "" || profile.SmallAvatar != ""}
	reason = ""
	switch {
	case decodeErr != nil:
		reason = "profile_decode_failed"
	case profile.Ret != 0:
		reason = "provider_rejected"
	case profile.Nickname == "":
		reason = "nickname_missing"
	}
	if reason != "" {
		return identity, done(reason, decodeErr, true, profileAttrs...)
	}
	identity.Nickname = profile.Nickname
	identity.AvatarURL = profile.Avatar
	if identity.AvatarURL == "" {
		identity.AvatarURL = profile.SmallAvatar
	}
	done("", nil, false, profileAttrs...)
	return identity, nil
}
func (s *Server) oidcIdentity(ctx context.Context, code, nonce, verifier string) (accounts.Identity, error) {
	ctx = logging.TrackUpstreamErrors(ctx)
	identity := accounts.Identity{ProviderID: s.cfg.OIDC.ID, Issuer: s.cfg.OIDC.Issuer}
	provider, config, err := s.oidcClient(ctx)
	if err != nil {
		return identity, err
	}
	done := providerStage(ctx, s.cfg.OIDC.ID, "token")
	ctx = oidc.ClientContext(ctx, logging.TraceClient(s.client))
	token, err := config.Exchange(ctx, code, oauth2.VerifierOption(verifier))
	if err != nil {
		var response *oauth2.RetrieveError
		if errors.As(err, &response) {
			status := 0
			if response.Response != nil {
				status = response.Response.StatusCode
			}
			code := logging.OAuthErrorCode(response.ErrorCode)
			rejected := status >= 400 && status < 500 && (code == "invalid_grant" || code == "access_denied")
			reason := "token_exchange_failed"
			if rejected {
				reason = "token_" + code
			}
			return identity, done(reason, err, rejected, "provider_error_code", code, "upstream_status", status)
		}
		return identity, done("token_exchange_failed", err, false)
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return identity, done("id_token_missing", nil, true, "id_token_present", false)
	}
	done("", nil, false, "id_token_present", true, "access_token_present", token.AccessToken != "")
	done = providerStage(ctx, s.cfg.OIDC.ID, "verify")
	verified, err := provider.VerifierContext(ctx, &oidc.Config{ClientID: s.cfg.OIDC.ClientID}).Verify(ctx, raw)
	if err != nil {
		reason := oidcVerificationReason(err)
		return identity, done(reason, err, reason != "keys_fetch_failed")
	}
	if verified.Nonce != nonce {
		return identity, done("nonce_mismatch", nil, true, "nonce_present", verified.Nonce != "", "subject_present", verified.Subject != "")
	}
	if verified.Subject == "" {
		return identity, done("subject_missing", nil, true, "nonce_matches", true, "subject_present", false)
	}
	done("", nil, false, "nonce_matches", true, "subject_present", true)
	done = providerStage(ctx, s.cfg.OIDC.ID, "claims")
	var claims oidcProfile
	if err = verified.Claims(&claims); err != nil {
		return identity, done("claims_decode_failed", err, true)
	}
	if claims.Name == "" && claims.Preferred == "" || claims.avatarURL() == "" {
		profile, err := s.oidcUserInfo(ctx, provider, token, verified.Subject)
		if err != nil {
			return identity, err
		}
		if claims.Name == "" && claims.Preferred == "" {
			claims.Name, claims.Preferred = profile.Name, profile.Preferred
		}
		if claims.avatarURL() == "" {
			claims.Picture = profile.Picture
		}
	}
	identity.Subject = verified.Subject
	identity.Nickname = claims.Name
	if identity.Nickname == "" {
		identity.Nickname = claims.Preferred
	}
	if identity.Nickname == "" {
		identity.Nickname = s.cfg.OIDC.Name + "用户"
	}
	identity.AvatarURL = claims.avatarURL()
	done("", nil, false, "name_present", claims.Name != "", "preferred_username_present", claims.Preferred != "", "picture_present", claims.Picture != "", "avatar_accepted", identity.AvatarURL != "", "nickname_fallback", claims.Name == "" && claims.Preferred == "")
	return identity, nil
}

type oidcProfile struct {
	Name      string `json:"name"`
	Preferred string `json:"preferred_username"`
	Picture   string `json:"picture"`
}

func (p oidcProfile) avatarURL() string {
	if u, err := url.Parse(p.Picture); err == nil && u.Scheme == "https" && u.Host != "" && u.User == nil {
		return p.Picture
	}
	return ""
}

// UserInfo supplements the verified ID Token; an unavailable profile service must
// not prevent login. A different subject, however, must never supply account data.
func (s *Server) oidcUserInfo(ctx context.Context, provider *oidc.Provider, token *oauth2.Token, subject string) (oidcProfile, error) {
	done := providerStage(ctx, s.cfg.OIDC.ID, "userinfo")
	logger := logging.FromContext(ctx).With("component", "auth", "provider", s.cfg.OIDC.ID, "stage", "userinfo")
	skip := func(reason string, cause error) (oidcProfile, error) {
		done(reason, cause, false)
		logger.WarnContext(ctx, "未能补充 OIDC 用户资料，继续使用已验证的 ID Token", "event", "auth.profile.unavailable", "reason", reason, "error_kind", logging.ErrorKind(cause))
		return oidcProfile{}, nil
	}
	endpoint := provider.UserInfoEndpoint()
	if endpoint == "" {
		done("", nil, false, "outcome_detail", "endpoint_not_provided")
		return oidcProfile{}, nil
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
		return skip("userinfo_endpoint_not_https", nil)
	}
	if token.AccessToken == "" {
		return skip("userinfo_access_token_missing", nil)
	}
	// Keep optional profile lookup bounded and never forward a bearer token on redirects.
	client := *s.client
	client.CheckRedirect = func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }
	lookupCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	lookupCtx = oidc.ClientContext(lookupCtx, logging.TraceClient(&client))
	info, err := provider.UserInfo(lookupCtx, oauth2.StaticTokenSource(token))
	if err != nil {
		if ctx.Err() != nil {
			return oidcProfile{}, done("userinfo_request_failed", ctx.Err(), false)
		}
		// The SDK's error can contain the entire provider response. Log only a
		// fixed reason and error kind, never the error text or profile payload.
		return skip("userinfo_request_failed", err)
	}
	if info.Subject != subject {
		return oidcProfile{}, done("userinfo_subject_mismatch", nil, true, "subject_present", info.Subject != "", "subject_matches", false)
	}
	var profile oidcProfile
	if err := info.Claims(&profile); err != nil {
		return skip("userinfo_claims_decode_failed", err)
	}
	done("", nil, false, "subject_matches", true, "name_present", profile.Name != "", "preferred_username_present", profile.Preferred != "", "picture_present", profile.Picture != "", "avatar_accepted", profile.avatarURL() != "")
	return profile, nil
}

// Provider libraries may include response bodies or raw claims in errors. Validation
// errors expose a fixed reason; dependency errors retain their useful cause for the
// application's redacting logger. Unwrap preserves cancellation and error kinds.
type authProviderError struct {
	stage, reason string
	cause         error
	rejected      bool
}

func (e *authProviderError) Error() string {
	if e.cause != nil && !e.rejected {
		return fmt.Sprintf("auth %s: %s: %v", e.stage, e.reason, e.cause)
	}
	return "auth " + e.stage + ": " + e.reason
}
func (e *authProviderError) Unwrap() error { return e.cause }

func providerStage(ctx context.Context, provider, stage string) func(string, error, bool, ...any) error {
	started := time.Now()
	logger := logging.FromContext(ctx).With("component", "auth", "provider", provider, "stage", stage)
	logger.DebugContext(ctx, "开始处理登录服务阶段", "event", "auth.provider.started", "outcome", "started")
	return func(reason string, cause error, rejected bool, fields ...any) error {
		if reason != "" && cause != nil {
			// go-oidc flattens some dependency errors with %v. Restore the typed
			// cause captured by our transport, without parsing provider error text.
			if ctx.Err() != nil {
				cause, rejected = ctx.Err(), false
			} else if dependency := logging.UpstreamFailure(ctx); dependency != nil {
				cause, rejected = fmt.Errorf("OIDC %s dependency failed: %w", stage, dependency), false
			}
		}
		outcome := "success"
		if reason != "" {
			outcome = "failed"
			if rejected {
				outcome = "rejected"
			}
		}
		attrs := []any{"event", "auth.provider.completed", "outcome", outcome, "reason", reason, "duration_ms", logging.DurationMS(started)}
		attrs = append(attrs, fields...)
		if cause != nil {
			attrs = append(attrs, "error_kind", logging.ErrorKind(cause))
		}
		logger.DebugContext(ctx, "登录服务阶段处理结束", attrs...)
		if reason == "" {
			return nil
		}
		return &authProviderError{stage: stage, reason: reason, cause: cause, rejected: rejected}
	}
}

func oidcVerificationReason(err error) string {
	message := strings.ToLower(err.Error())
	switch {
	case strings.Contains(message, "get keys") || strings.Contains(message, "fetching keys") || strings.Contains(message, "decode keys"):
		return "keys_fetch_failed"
	case strings.Contains(message, "expired"):
		return "token_expired"
	case strings.Contains(message, "audience"):
		return "audience_mismatch"
	case strings.Contains(message, "issuer"):
		return "issuer_mismatch"
	case strings.Contains(message, "algorithm"):
		return "signing_algorithm_invalid"
	case strings.Contains(message, "signature"):
		return "signature_invalid"
	default:
		return "token_invalid"
	}
}
