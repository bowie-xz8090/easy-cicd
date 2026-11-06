package api

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"auto-deploy-platform/internal/auth"
	"auto-deploy-platform/internal/config"
	"auto-deploy-platform/internal/db"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWhitelistSavePersistsOnlyAdmissionRules(t *testing.T) {
	router, manager := setupConfigSecurityRouter(t)
	original := manager.Get()
	for _, token := range []string{"", "wrong", original.AdminToken} {
		payload := `{"whitelist":[" 192.0.2.1 ","18223452780","18223452780"],"trusted_proxies":["0.0.0.0/0"],"client_secret":"forged"}`
		request := httptest.NewRequest("PUT", "/api/config/whitelist", strings.NewReader(payload))
		request.Header.Set("Content-Type", "application/json")
		request.Header.Set("Authorization", "Bearer "+token)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		if token != original.AdminToken {
			assert.Equal(t, 401, response.Code)
			continue
		}
		require.Equal(t, 200, response.Code, response.Body.String())
		assertNoConfigSecrets(t, response.Body.String(), original)
	}
	persisted, err := config.Load(manager.GetFilePath())
	require.NoError(t, err)
	assert.Equal(t, []string{"192.0.2.1", "18223452780"}, persisted.Access.Whitelist)
	expected := *original
	expected.Access.Whitelist = persisted.Access.Whitelist
	assert.Equal(t, &expected, persisted)
	assert.Equal(t, persisted, manager.Get())
	data, err := os.ReadFile(manager.GetFilePath())
	require.NoError(t, err)
	assert.NotContains(t, string(data), "ip_whitelist:")
}

func TestWhitelistInvalidOrMissingListDoesNotChangeConfig(t *testing.T) {
	router, manager := setupConfigSecurityRouter(t)
	before, err := os.ReadFile(manager.GetFilePath())
	require.NoError(t, err)
	for _, payload := range []string{`{}`, `{"whitelist":null}`, `{"whitelist":"192.0.2.1"}`, `{"whitelist":["+8618223452780"]}`, `{"whitelist":["192.0.2.0/24"]}`} {
		request := httptest.NewRequest("PUT", "/api/config/whitelist", strings.NewReader(payload))
		request.Header.Set("Authorization", "Bearer "+manager.Get().AdminToken)
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		assert.Equal(t, 400, response.Code)
		after, err := os.ReadFile(manager.GetFilePath())
		require.NoError(t, err)
		assert.Equal(t, before, after)
	}
}

func TestEveryBusinessRouteRejectsLoggedInUnlistedPhone(t *testing.T) {
	cfg := testConfig()
	cfg.OIDC = config.OIDCConfig{Issuer: "https://sso.example", ClientID: "cicd"}
	database, err := db.InitDB(filepath.Join(t.TempDir(), "sessions.db"))
	require.NoError(t, err)
	defer database.Close()
	raw := "opaque-test-session"
	hash := sha256.Sum256([]byte(raw))
	require.NoError(t, db.SaveAuthSession(database, db.AuthSession{TokenHash: hex.EncodeToString(hash[:]), Issuer: cfg.OIDC.Issuer, ClientID: cfg.OIDC.ClientID, Subject: "test-subject", Phone: "18223452780", CreatedAt: time.Now(), ExpiresAt: time.Now().Add(time.Hour)}))
	gin.SetMode(gin.TestMode)
	router := gin.New()
	handler := NewHandler(&mockGiteaClient{}, cfg, &mockTaskManager{})
	handler.SetAccessService(auth.NewService(database, func() *config.AppConfig { return cfg }, ""))
	handler.RegisterRoutes(router)
	for _, route := range router.Routes() {
		if strings.HasPrefix(route.Path, "/api/auth/") || route.Path == "/api/health" || route.Path == "/api/site-info" {
			continue
		}
		t.Run(route.Method+route.Path, func(t *testing.T) {
			path := strings.NewReplacer(":owner", "org", ":repo", "repo", ":id", "task", ":key", "dev").Replace(route.Path)
			request := httptest.NewRequest(route.Method, path, bytes.NewBufferString(`{}`))
			request.RemoteAddr = "198.51.100.1:1234"
			request.AddCookie(&http.Cookie{Name: "cicd_session", Value: raw})
			request.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
			request.Header.Set("X-CICD-Request", "1")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, 403, response.Code)
			var body map[string]interface{}
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
			assert.Equal(t, float64(auth.ForbiddenCode), body["code"])
			assert.NotContains(t, response.Body.String(), "18223452780")
			assert.Empty(t, response.Result().Cookies())
		})
	}
}
