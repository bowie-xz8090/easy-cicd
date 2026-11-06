package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
	"time"

	"auto-deploy-platform/internal/config"
	"auto-deploy-platform/internal/db"

	"github.com/gin-gonic/gin"
	jose "github.com/go-jose/go-jose/v4"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/oauth2"
)

type oidcFixture struct {
	service                                         *Service
	router                                          *gin.Engine
	cfg                                             *config.AppConfig
	now                                             time.Time
	path                                            string
	nonce, challenge                                string
	phone, userPhone, userSubject                   string
	issuerOverride, audienceOverride, nonceOverride string
	badSignature                                    bool
	tokenCalls                                      int
}

func newOIDCFixture(t *testing.T) *oidcFixture {
	t.Helper()
	f := &oidcFixture{now: time.Now().UTC().Truncate(time.Second), phone: "+8618223452780", userSubject: "subject-1"}
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	wrongKey, err := rsa.GenerateKey(rand.Reader, 2048)
	require.NoError(t, err)
	mux := http.NewServeMux()
	server := httptest.NewServer(mux)
	t.Cleanup(server.Close)
	f.cfg = &config.AppConfig{OIDC: config.OIDCConfig{Issuer: server.URL, DiscoveryURL: server.URL + "/.well-known/openid-configuration", ClientID: "test-client", ClientSecret: "server-only-secret", RedirectURI: "http://cicd.example/api/auth/oidc/callback"}}
	respond := func(w http.ResponseWriter, value interface{}) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(value)
	}
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		respond(w, map[string]interface{}{"issuer": server.URL, "authorization_endpoint": server.URL + "/authorize", "token_endpoint": server.URL + "/token", "userinfo_endpoint": server.URL + "/userinfo", "jwks_uri": server.URL + "/jwks", "id_token_signing_alg_values_supported": []string{"RS256"}})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		respond(w, jose.JSONWebKeySet{Keys: []jose.JSONWebKey{{Key: &key.PublicKey, KeyID: "test-key", Algorithm: "RS256", Use: "sig"}}})
	})
	mux.HandleFunc("/userinfo", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer private-access-token" {
			w.WriteHeader(401)
			return
		}
		respond(w, map[string]string{"sub": f.userSubject, "phone_number": f.userPhone})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		f.tokenCalls++
		user, password, ok := r.BasicAuth()
		if !ok || user != f.cfg.OIDC.ClientID || password != f.cfg.OIDC.ClientSecret {
			w.WriteHeader(401)
			return
		}
		_ = r.ParseForm()
		if r.Form.Get("code") != "valid-code" || oauth2.S256ChallengeFromVerifier(r.Form.Get("code_verifier")) != f.challenge || r.Form.Get("redirect_uri") != f.cfg.OIDC.RedirectURI {
			w.WriteHeader(400)
			return
		}
		issuer, audience, nonce := server.URL, f.cfg.OIDC.ClientID, f.nonce
		if f.issuerOverride != "" {
			issuer = f.issuerOverride
		}
		if f.audienceOverride != "" {
			audience = f.audienceOverride
		}
		if f.nonceOverride != "" {
			nonce = f.nonceOverride
		}
		claims := map[string]interface{}{"iss": issuer, "aud": audience, "sub": "subject-1", "nonce": nonce, "iat": f.now.Unix(), "exp": f.now.Add(time.Hour).Unix()}
		if f.phone != "" {
			claims["phone_number"] = f.phone
		}
		signingKey := key
		if f.badSignature {
			signingKey = wrongKey
		}
		signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: signingKey}, (&jose.SignerOptions{}).WithHeader("kid", "test-key"))
		if err != nil {
			w.WriteHeader(500)
			return
		}
		payload, _ := json.Marshal(claims)
		signed, err := signer.Sign(payload)
		if err != nil {
			w.WriteHeader(500)
			return
		}
		serialized, err := signed.CompactSerialize()
		if err != nil {
			w.WriteHeader(500)
			return
		}
		respond(w, map[string]interface{}{"access_token": "private-access-token", "token_type": "Bearer", "expires_in": 3600, "id_token": serialized})
	})
	f.path = filepath.Join(t.TempDir(), "sessions.db")
	database, err := db.InitDB(f.path)
	require.NoError(t, err)
	t.Cleanup(func() { database.Close() })
	f.service = NewService(database, func() *config.AppConfig { return f.cfg }, "")
	f.service.now = func() time.Time { return f.now }
	f.router = accessRouter(f.service)
	return f
}

func (f *oidcFixture) begin(t *testing.T) (string, *http.Cookie) {
	t.Helper()
	request := httptest.NewRequest("GET", "http://cicd.example/api/auth/oidc/login?return_to=%2Fhistory", nil)
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	require.Equal(t, 302, response.Code, response.Body.String())
	location, err := url.Parse(response.Header().Get("Location"))
	require.NoError(t, err)
	assert.Equal(t, "openid phone", location.Query().Get("scope"))
	assert.Equal(t, "S256", location.Query().Get("code_challenge_method"))
	f.nonce = location.Query().Get("nonce")
	f.challenge = location.Query().Get("code_challenge")
	require.NotEmpty(t, f.nonce)
	require.NotEmpty(t, f.challenge)
	assert.NotContains(t, location.String(), f.cfg.OIDC.ClientSecret)
	cookies := response.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.True(t, cookies[0].HttpOnly)
	assert.Equal(t, http.SameSiteLaxMode, cookies[0].SameSite)
	return location.Query().Get("state"), cookies[0]
}

func (f *oidcFixture) callback(state string, cookie *http.Cookie) *httptest.ResponseRecorder {
	request := httptest.NewRequest("GET", "http://cicd.example/api/auth/oidc/callback?state="+url.QueryEscape(state)+"&code=valid-code", nil)
	if cookie != nil {
		request.AddCookie(cookie)
	}
	response := httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	return response
}

func cookieNamed(t *testing.T, response *httptest.ResponseRecorder, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range response.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("cookie %s missing", name)
	return nil
}

func TestOIDCLoginPersistsThirtyDaySession(t *testing.T) {
	f := newOIDCFixture(t)
	state, binding := f.begin(t)
	response := f.callback(state, binding)
	require.Equal(t, "/history", response.Header().Get("Location"))
	cookie := cookieNamed(t, response, sessionCookie)
	assert.Equal(t, 30*24*60*60, cookie.MaxAge)
	assert.True(t, cookie.HttpOnly)
	assert.False(t, cookie.Secure)
	assert.WithinDuration(t, f.now.Add(sessionTTL), cookie.Expires, time.Second)
	assert.NotContains(t, cookie.Value, "18223452780")
	session, err := db.FindAuthSession(f.service.database, tokenHash(cookie.Value))
	require.NoError(t, err)
	assert.Equal(t, "18223452780", session.Phone)
	assert.NotEqual(t, cookie.Value, session.TokenHash)
	assert.Equal(t, 64, len(session.TokenHash))
	// Close and reopen SQLite, then create a new service to simulate a process restart.
	require.NoError(t, f.service.database.Close())
	database, err := db.InitDB(f.path)
	require.NoError(t, err)
	defer database.Close()
	service := NewService(database, func() *config.AppConfig { return f.cfg }, "")
	service.now = func() time.Time { return f.now }
	router := accessRouter(service)
	check := func() *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", "/api/auth/session", nil)
		request.AddCookie(cookie)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		return response
	}
	for _, advance := range []time.Duration{0, sessionTTL - time.Second} {
		original := f.now
		f.now = original.Add(advance)
		body := check().Body.String()
		assert.Contains(t, body, `"authenticated":true`)
		assert.Contains(t, body, "182****2780")
		assert.NotContains(t, body, "18223452780")
		persisted, err := db.FindAuthSession(database, tokenHash(cookie.Value))
		require.NoError(t, err)
		assert.Equal(t, session.ExpiresAt, persisted.ExpiresAt)
		f.now = original
	}
	// A whitelisted IP wins even when an SSO cookie is present.
	f.cfg.Access.Whitelist = []string{"192.0.2.1"}
	body := check().Body.String()
	assert.Contains(t, body, `"mode":"ip"`)
	assert.Contains(t, body, "192.0.2.1")
	assert.NotContains(t, body, "182****2780")
	f.cfg.Access.Whitelist = nil
	f.now = f.now.Add(sessionTTL)
	assert.Contains(t, check().Body.String(), `"authenticated":false`)
}

func TestOIDCCallbackRejectsInvalidIdentityAndReplay(t *testing.T) {
	for _, name := range []string{"signature", "issuer", "audience", "nonce", "state", "binding", "expired_transaction", "missing_phone", "invalid_phone", "userinfo_subject"} {
		t.Run(name, func(t *testing.T) {
			f := newOIDCFixture(t)
			state, binding := f.begin(t)
			switch name {
			case "signature":
				f.badSignature = true
			case "issuer":
				f.issuerOverride = "https://untrusted.example"
			case "audience":
				f.audienceOverride = "different-client"
			case "nonce":
				f.nonceOverride = "wrong-nonce"
			case "state":
				state = "wrong-state"
			case "binding":
				binding = &http.Cookie{Name: transactionCookie, Value: "wrong-browser"}
			case "expired_transaction":
				f.now = f.now.Add(11 * time.Minute)
			case "missing_phone":
				f.phone = ""
				f.userPhone = ""
			case "invalid_phone":
				f.phone = "not-a-phone"
			case "userinfo_subject":
				f.phone = ""
				f.userPhone = "+8618223452780"
				f.userSubject = "someone-else"
			}
			response := f.callback(state, binding)
			assert.Equal(t, "/?auth_error=login_failed", response.Header().Get("Location"))
			for _, cookie := range response.Result().Cookies() {
				assert.NotEqual(t, sessionCookie, cookie.Name)
			}
			assert.NotContains(t, response.Body.String(), f.cfg.OIDC.ClientSecret)
			assert.NotContains(t, response.Body.String(), "private-access-token")
		})
	}
	f := newOIDCFixture(t)
	f.phone = ""
	f.userPhone = "+8618223452780"
	state, binding := f.begin(t)
	response := f.callback(state, binding)
	assert.Equal(t, "/history", response.Header().Get("Location"))
	assert.Equal(t, 1, f.tokenCalls)
	response = f.callback(state, binding)
	assert.Equal(t, "/?auth_error=login_failed", response.Header().Get("Location"))
	assert.Equal(t, 1, f.tokenCalls, "a consumed transaction must not exchange a code again")
}

func TestCookieFlagsAndSubpath(t *testing.T) {
	f := newOIDCFixture(t)
	f.cfg.OIDC.RedirectURI = "https://cicd.example/cicd/api/auth/oidc/callback"
	f.service.basePath = "/cicd"
	state, binding := f.begin(t)
	assert.True(t, binding.Secure)
	assert.Equal(t, "/cicd/", binding.Path)
	response := f.callback(state, binding)
	cookie := cookieNamed(t, response, sessionCookie)
	assert.True(t, cookie.Secure)
	assert.Equal(t, "/cicd/", cookie.Path)
	assert.Equal(t, "/cicd/", response.Header().Get("Location"))
	assert.Equal(t, "", fmt.Sprint(response.Header().Get("Authorization")))
}

func TestOIDCWhitelistAuthorizationPreservesSession(t *testing.T) {
	f := newOIDCFixture(t)
	state, binding := f.begin(t)
	response := f.callback(state, binding)
	require.Equal(t, "/history", response.Header().Get("Location"))
	cookie := cookieNamed(t, response, sessionCookie)
	original, err := db.FindAuthSession(f.service.database, tokenHash(cookie.Value))
	require.NoError(t, err)
	check := func(path string) *httptest.ResponseRecorder {
		request := httptest.NewRequest("GET", path, nil)
		request.RemoteAddr = "198.51.100.1:1234"
		request.AddCookie(cookie)
		result := httptest.NewRecorder()
		f.router.ServeHTTP(result, request)
		return result
	}
	for attempt := 0; attempt < 2; attempt++ {
		body := check("/api/auth/session")
		assert.Contains(t, body.Body.String(), `"authenticated":true`)
		assert.Contains(t, body.Body.String(), `"authorized":false`)
		assert.Contains(t, body.Body.String(), "182****2780")
		assert.NotContains(t, body.Body.String(), original.Phone)
		denied := check("/api/private")
		assert.Equal(t, 403, denied.Code)
		assert.Contains(t, denied.Body.String(), `"code":40301`)
		assert.Empty(t, denied.Result().Cookies(), "denial must not clear or replace the session cookie")
	}
	// A new service reuses the same session without any new OIDC exchange.
	f.service = NewService(f.service.database, func() *config.AppConfig { return f.cfg }, "")
	f.service.now = func() time.Time { return f.now }
	f.router = accessRouter(f.service)
	f.cfg.Access.Whitelist = []string{"18223452780"}
	assert.Equal(t, 200, check("/api/private").Code)
	assert.Contains(t, check("/api/auth/session").Body.String(), `"authorized":true`)
	f.cfg.Access.Whitelist = nil
	assert.Equal(t, 403, check("/api/private").Code)
	persisted, err := db.FindAuthSession(f.service.database, tokenHash(cookie.Value))
	require.NoError(t, err)
	assert.Equal(t, original, persisted)
	assert.Equal(t, 1, f.tokenCalls)
	f.now = original.ExpiresAt
	assert.Equal(t, 401, check("/api/private").Code)
	assert.Contains(t, check("/api/auth/session").Body.String(), `"authenticated":false`)
}
