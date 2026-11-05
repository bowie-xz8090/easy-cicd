package auth

import (
	"context"
	"crypto/subtle"
	"errors"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
	"unicode"

	"auto-deploy-platform/internal/config"
	"auto-deploy-platform/internal/db"
	"auto-deploy-platform/internal/identity"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
	"golang.org/x/oauth2"
)

type loginTransaction struct {
	Binding  string
	Nonce    string
	Verifier string
	ReturnTo string
	Expires  time.Time
	Config   config.OIDCConfig
	Provider *oidc.Provider
}

func (s *Service) oidcContext(ctx context.Context) context.Context {
	return oidc.ClientContext(ctx, s.client)
}

func (s *Service) discover(ctx context.Context, cfg config.OIDCConfig) (*oidc.Provider, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" || cfg.ClientSecret == "" || cfg.RedirectURI == "" || cfg.DiscoveryURL != strings.TrimRight(cfg.Issuer, "/")+"/.well-known/openid-configuration" {
		return nil, errors.New("OIDC configuration is incomplete")
	}
	s.providerMu.Lock()
	defer s.providerMu.Unlock()
	if s.provider != nil && s.providerConfig == cfg {
		return s.provider, nil
	}
	provider, err := oidc.NewProvider(s.oidcContext(ctx), cfg.Issuer)
	if err != nil {
		return nil, err
	}
	s.provider = provider
	s.providerConfig = cfg
	return provider, nil
}

func oauthConfig(tx loginTransaction) oauth2.Config {
	endpoint := tx.Provider.Endpoint()
	endpoint.AuthStyle = oauth2.AuthStyleInHeader
	return oauth2.Config{ClientID: tx.Config.ClientID, ClientSecret: tx.Config.ClientSecret, RedirectURL: tx.Config.RedirectURI, Endpoint: endpoint, Scopes: []string{oidc.ScopeOpenID, "phone"}}
}

func (s *Service) safeReturn(raw string) string {
	fallback := s.basePath + "/"
	u, err := url.Parse(raw)
	if err != nil || u.IsAbs() || u.Host != "" || !strings.HasPrefix(u.Path, "/") || strings.HasPrefix(u.Path, "//") || strings.Contains(u.Path, "\\") || strings.IndexFunc(raw, unicode.IsControl) >= 0 || strings.IndexFunc(u.Path, unicode.IsControl) >= 0 {
		return fallback
	}
	clean := path.Clean(u.Path)
	if s.basePath != "" && clean != s.basePath && !strings.HasPrefix(clean, s.basePath+"/") {
		return fallback
	}
	if strings.HasPrefix(clean, s.basePath+"/api/") {
		return fallback
	}
	u.Path = clean
	u.RawPath = ""
	return u.String()
}

func (s *Service) handleLogin(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	cfg := s.config().OIDC
	provider, err := s.discover(c.Request.Context(), cfg)
	if err != nil || s.database == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": -1, "message": "SSO 暂时不可用，请稍后重试"})
		return
	}
	state, err := randomToken()
	if err != nil {
		s.loginFailure(c)
		return
	}
	binding, err := randomToken()
	if err != nil {
		s.loginFailure(c)
		return
	}
	nonce, err := randomToken()
	if err != nil {
		s.loginFailure(c)
		return
	}
	tx := loginTransaction{Binding: tokenHash(binding), Nonce: nonce, Verifier: oauth2.GenerateVerifier(), ReturnTo: s.safeReturn(c.Query("return_to")), Expires: s.now().Add(transactionTTL), Config: cfg, Provider: provider}
	s.mu.Lock()
	for key, previous := range s.transactions {
		if !s.now().Before(previous.Expires) {
			delete(s.transactions, key)
		}
	}
	if len(s.transactions) >= 1024 {
		s.mu.Unlock()
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": -1, "message": "登录请求繁忙，请稍后重试"})
		return
	}
	s.transactions[state] = tx
	s.mu.Unlock()
	s.setCookie(c, transactionCookie, binding, tx.Expires, int(transactionTTL.Seconds()))
	settings := oauthConfig(tx)
	c.Redirect(http.StatusFound, settings.AuthCodeURL(state, oidc.Nonce(nonce), oauth2.S256ChallengeOption(tx.Verifier)))
}

func (s *Service) loginFailure(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	c.Redirect(http.StatusFound, s.basePath+"/?auth_error=login_failed")
}

func (s *Service) handleCallback(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Header("Referrer-Policy", "no-referrer")
	query := c.Request.URL.Query()
	state := query.Get("state")
	s.mu.Lock()
	tx, ok := s.transactions[state]
	delete(s.transactions, state)
	s.mu.Unlock()
	binding, _ := c.Cookie(transactionCookie)
	s.setCookie(c, transactionCookie, "", time.Unix(1, 0), -1)
	if !ok || len(query["state"]) != 1 || len(query["code"]) != 1 || query.Get("code") == "" || query.Has("error") || !s.now().Before(tx.Expires) || tx.Config != s.config().OIDC || subtle.ConstantTimeCompare([]byte(tx.Binding), []byte(tokenHash(binding))) != 1 {
		s.loginFailure(c)
		return
	}
	ctx := s.oidcContext(c.Request.Context())
	settings := oauthConfig(tx)
	token, err := settings.Exchange(ctx, query.Get("code"), oauth2.VerifierOption(tx.Verifier))
	if err != nil {
		s.loginFailure(c)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		s.loginFailure(c)
		return
	}
	verified, err := tx.Provider.VerifierContext(ctx, &oidc.Config{ClientID: tx.Config.ClientID, SupportedSigningAlgs: []string{oidc.RS256}, Now: s.now}).Verify(ctx, rawIDToken)
	if err != nil || verified.Nonce != tx.Nonce || verified.Subject == "" || verified.IssuedAt.After(s.now().Add(time.Minute)) {
		s.loginFailure(c)
		return
	}
	if verified.AccessTokenHash != "" && verified.VerifyAccessToken(token.AccessToken) != nil {
		s.loginFailure(c)
		return
	}
	var claims struct {
		Phone           string `json:"phone_number"`
		AuthorizedParty string `json:"azp"`
	}
	if verified.Claims(&claims) != nil || (len(verified.Audience) > 1 && claims.AuthorizedParty != tx.Config.ClientID) || (claims.AuthorizedParty != "" && claims.AuthorizedParty != tx.Config.ClientID) {
		s.loginFailure(c)
		return
	}
	if claims.Phone == "" {
		info, err := tx.Provider.UserInfo(ctx, oauth2.StaticTokenSource(token))
		if err != nil || info.Subject != verified.Subject || info.Claims(&claims) != nil {
			s.loginFailure(c)
			return
		}
	}
	phone, err := identity.NormalizePhone(claims.Phone)
	if err != nil {
		s.loginFailure(c)
		return
	}
	rawSession, err := randomToken()
	if err != nil {
		s.loginFailure(c)
		return
	}
	now := s.now().UTC()
	session := db.AuthSession{TokenHash: tokenHash(rawSession), Issuer: tx.Config.Issuer, ClientID: tx.Config.ClientID, Subject: verified.Subject, Phone: phone, CreatedAt: now, ExpiresAt: now.Add(sessionTTL)}
	if s.database == nil || db.SaveAuthSession(s.database, session) != nil {
		s.loginFailure(c)
		return
	}
	_ = db.DeleteExpiredAuthSessions(s.database, now)
	s.setCookie(c, sessionCookie, rawSession, session.ExpiresAt, int(sessionTTL.Seconds()))
	c.Redirect(http.StatusFound, tx.ReturnTo)
}
