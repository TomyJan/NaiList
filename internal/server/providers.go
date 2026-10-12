package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
	"nailist/internal/accounts"
	"net/http"
	"net/url"
	"regexp"
	"strings"
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
	identities, err := (accounts.Store{DB: s.db}).List(r.Context(), user.ID)
	if err != nil {
		failure(w, r, err)
		return
	}
	respond(w, r, 200, struct {
		User
		Identities []accounts.Identity `json:"identities"`
	}{user, identities})
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
	ctx = oidc.ClientContext(ctx, s.client)
	provider, err := oidc.NewProvider(ctx, s.cfg.OIDC.Issuer)
	if err != nil {
		return nil, nil, err
	}
	var metadata struct {
		JWKS string `json:"jwks_uri"`
	}
	if err = provider.Claims(&metadata); err != nil {
		return nil, nil, err
	}
	for _, address := range []string{provider.Endpoint().AuthURL, provider.Endpoint().TokenURL, metadata.JWKS} {
		u, err := url.Parse(address)
		if err != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" {
			return nil, nil, errors.New("OIDC endpoints must use HTTPS")
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
	return provider, config, nil
}
func (s *Server) qqIdentity(ctx context.Context, code string) (accounts.Identity, error) {
	identity := accounts.Identity{ProviderID: "qq", Issuer: s.cfg.QQAppID}
	q := url.Values{"grant_type": {"authorization_code"}, "client_id": {s.cfg.QQAppID}, "client_secret": {s.cfg.QQAppKey}, "code": {code}, "redirect_uri": {s.cfg.PublicURL + "/auth/qq/callback"}, "fmt": {"json"}}
	data, err := s.qqGet(ctx, "https://graph.qq.com/oauth2.0/token?"+q.Encode())
	if err != nil {
		return identity, err
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
		return identity, fmt.Errorf("QQ access token missing (provider_error=%d)", token.Error)
	}
	q = url.Values{"access_token": {token.AccessToken}, "fmt": {"json"}}
	if s.cfg.QQUseUnionID {
		q.Set("unionid", "1")
	}
	data, err = s.qqGet(ctx, "https://graph.qq.com/oauth2.0/me?"+q.Encode())
	if err != nil {
		return identity, err
	}
	var info struct {
		Error    int    `json:"error"`
		OpenID   string `json:"openid"`
		ClientID string `json:"client_id"`
		UnionID  string `json:"unionid"`
	}
	if json.Unmarshal(data, &info) != nil || info.OpenID == "" || info.ClientID != s.cfg.QQAppID || s.cfg.QQUseUnionID && info.UnionID == "" {
		return identity, fmt.Errorf("QQ identity invalid (provider_error=%d)", info.Error)
	}
	identity.Subject = info.OpenID
	identity.UnionID = info.UnionID
	data, err = s.qqGet(ctx, "https://graph.qq.com/user/get_user_info?"+url.Values{"access_token": {token.AccessToken}, "oauth_consumer_key": {s.cfg.QQAppID}, "openid": {info.OpenID}}.Encode())
	if err != nil {
		return identity, err
	}
	var profile struct {
		Ret         int    `json:"ret"`
		Nickname    string `json:"nickname"`
		Avatar      string `json:"figureurl_qq_2"`
		SmallAvatar string `json:"figureurl_qq_1"`
	}
	if json.Unmarshal(data, &profile) != nil || profile.Ret != 0 || profile.Nickname == "" {
		return identity, fmt.Errorf("QQ profile invalid (provider_error=%d)", profile.Ret)
	}
	identity.Nickname = profile.Nickname
	identity.AvatarURL = profile.Avatar
	if identity.AvatarURL == "" {
		identity.AvatarURL = profile.SmallAvatar
	}
	return identity, nil
}
func (s *Server) oidcIdentity(ctx context.Context, code, nonce, verifier string) (accounts.Identity, error) {
	identity := accounts.Identity{ProviderID: s.cfg.OIDC.ID, Issuer: s.cfg.OIDC.Issuer}
	provider, config, err := s.oidcClient(ctx)
	if err != nil {
		return identity, err
	}
	token, err := config.Exchange(oidc.ClientContext(ctx, s.client), code, oauth2.VerifierOption(verifier))
	if err != nil {
		return identity, err
	}
	raw, ok := token.Extra("id_token").(string)
	if !ok {
		return identity, errors.New("OIDC id_token missing")
	}
	verified, err := provider.Verifier(&oidc.Config{ClientID: s.cfg.OIDC.ClientID}).Verify(oidc.ClientContext(ctx, s.client), raw)
	if err != nil {
		return identity, err
	}
	if verified.Nonce != nonce || verified.Subject == "" {
		return identity, errors.New("OIDC nonce or subject invalid")
	}
	var claims struct {
		Name      string `json:"name"`
		Preferred string `json:"preferred_username"`
		Picture   string `json:"picture"`
	}
	if err = verified.Claims(&claims); err != nil {
		return identity, err
	}
	identity.Subject = verified.Subject
	identity.Nickname = claims.Name
	if identity.Nickname == "" {
		identity.Nickname = claims.Preferred
	}
	if identity.Nickname == "" {
		identity.Nickname = s.cfg.OIDC.Name + "用户"
	}
	if u, err := url.Parse(claims.Picture); err == nil && u.Scheme == "https" && u.Host != "" {
		identity.AvatarURL = claims.Picture
	}
	return identity, nil
}
