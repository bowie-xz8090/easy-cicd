package api

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"auto-deploy-platform/internal/config"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func setupConfigSecurityRouter(t *testing.T) (*gin.Engine, *config.Manager) {
	t.Helper()
	cfg := testConfig()
	cfg.Gitea = config.GiteaConfig{
		URL: "https://gitea.example.com", Username: "git-user", Token: "private-gitea-token",
	}
	cfg.AdminToken = "private-admin-token"
	cfg.OIDC = config.OIDCConfig{
		Issuer: "https://sso.example.com", DiscoveryURL: "https://sso.example.com/.well-known/openid-configuration",
		ClientID: "test-client", ClientSecret: "private-oidc-client-secret",
		RedirectURI: "https://deploy.example.com/api/auth/oidc/callback",
	}
	server := cfg.Servers["dev-server"]
	server.Password = "private-ssh-password"
	cfg.Servers["dev-server"] = server
	environment := cfg.Environments["dev"]
	environment.Password = "private-deploy-password"
	cfg.Environments["dev"] = environment
	data, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	path := filepath.Join(t.TempDir(), "config.yaml")
	require.NoError(t, os.WriteFile(path, data, 0600))
	manager, err := config.NewManager(path)
	require.NoError(t, err)
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(legacyTestClient())
	NewHandlerWithManager(&mockGiteaClient{}, manager, nil).RegisterRoutes(router)
	return router, manager
}

func assertNoConfigSecrets(t *testing.T, body string, cfg *config.AppConfig) {
	t.Helper()
	for _, secret := range []string{
		cfg.Gitea.Token, cfg.AdminToken, cfg.OIDC.ClientSecret,
		cfg.Servers["dev-server"].Password, cfg.Environments["dev"].Password,
	} {
		require.NotEmpty(t, secret)
		assert.NotContains(t, body, secret)
	}
}

func TestConfigResponsesExcludeSecrets(t *testing.T) {
	for _, endpoint := range []struct{ method, path string }{
		{http.MethodGet, "/api/config/editor"},
		{http.MethodPost, "/api/config/reload"},
	} {
		t.Run(endpoint.path, func(t *testing.T) {
			router, manager := setupConfigSecurityRouter(t)
			cfg := manager.Get()
			for _, token := range []string{"", "wrong-token", cfg.AdminToken} {
				request := httptest.NewRequest(endpoint.method, endpoint.path, nil)
				if token != "" {
					request.Header.Set("Authorization", "Bearer "+token)
				}
				response := httptest.NewRecorder()
				router.ServeHTTP(response, request)
				assertNoConfigSecrets(t, response.Body.String(), cfg)
				if token != cfg.AdminToken {
					assert.Equal(t, http.StatusUnauthorized, response.Code)
					continue
				}
				require.Equal(t, http.StatusOK, response.Code)
				var body struct {
					Code int                        `json:"code"`
					Data map[string]json.RawMessage `json:"data"`
				}
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &body))
				assert.Zero(t, body.Code)
				assert.Len(t, body.Data, 4)
				assert.Contains(t, body.Data, "whitelist")
				assert.Contains(t, body.Data, "environments")
				assert.Contains(t, body.Data, "projects")
				assert.Contains(t, body.Data, "servers")
			}
		})
	}
}

func TestFullConfigUpdatePreservesServerSideSecrets(t *testing.T) {
	router, manager := setupConfigSecurityRouter(t)
	original := manager.Get()
	data, err := json.Marshal(original)
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &payload))
	payload["server"].(map[string]interface{})["title"] = "Updated title"
	// Credentials supplied by a browser must not replace the server-side values.
	payload["gitea"].(map[string]interface{})["token"] = "browser-supplied-token"
	payload["admin_token"] = "browser-supplied-admin-token"
	payload["oidc"] = map[string]interface{}{"client_secret": "browser-supplied-oidc-secret"}
	payload["access"] = map[string]interface{}{"ip_whitelist": []string{"0.0.0.0"}}
	payload["servers"].(map[string]interface{})["dev-server"].(map[string]interface{})["password"] = "browser-supplied-ssh-password"
	payload["environments"].(map[string]interface{})["dev"].(map[string]interface{})["password"] = "browser-supplied-deploy-password"
	data, err = json.Marshal(payload)
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPut, "/api/config", bytes.NewReader(data))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Authorization", "Bearer "+original.AdminToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assertNoConfigSecrets(t, response.Body.String(), original)
	assert.NotContains(t, response.Body.String(), "browser-supplied-")
	assert.Equal(t, "Updated title", manager.Get().Server.Title)
	persisted, err := config.Load(manager.GetFilePath())
	require.NoError(t, err)
	for _, cfg := range []*config.AppConfig{manager.Get(), persisted} {
		assert.Equal(t, original.Gitea.Token, cfg.Gitea.Token)
		assert.Equal(t, original.AdminToken, cfg.AdminToken)
		assert.Equal(t, original.OIDC, cfg.OIDC)
		assert.Equal(t, original.Access, cfg.Access)
		assert.Equal(t, original.Servers["dev-server"].Password, cfg.Servers["dev-server"].Password)
		assert.Equal(t, original.Environments["dev"].Password, cfg.Environments["dev"].Password)
	}
}

func TestReloadConfigLoadsNewTokenWithoutExposingIt(t *testing.T) {
	router, manager := setupConfigSecurityRouter(t)
	cfg := manager.Get()
	cfg.Gitea.Token = "rotated-private-gitea-token"
	cfg.OIDC.ClientSecret = "rotated-private-oidc-client-secret"
	cfg.Server.Title = "Reloaded title"
	data, err := yaml.Marshal(cfg)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(manager.GetFilePath(), data, 0600))
	request := httptest.NewRequest(http.MethodPost, "/api/config/reload", nil)
	request.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	assertNoConfigSecrets(t, response.Body.String(), cfg)
	assert.Equal(t, cfg.Gitea.Token, manager.Get().Gitea.Token)
	assert.Equal(t, cfg.OIDC, manager.Get().OIDC)
	assert.Equal(t, "Reloaded title", manager.Get().Server.Title)
}

func TestEditableConfigUpdatePreservesAllCredentials(t *testing.T) {
	router, manager := setupConfigSecurityRouter(t)
	original := manager.Get()
	payload, err := json.Marshal(editableConfig(original))
	require.NoError(t, err)
	request := httptest.NewRequest(http.MethodPut, "/api/config/editor", bytes.NewReader(payload))
	request.Header.Set("Authorization", "Bearer "+original.AdminToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assertNoConfigSecrets(t, response.Body.String(), original)
	assert.Equal(t, original.Environments["dev"].Password, manager.Get().Environments["dev"].Password)
	assert.Equal(t, original.Gitea.Token, manager.Get().Gitea.Token)
	assert.Equal(t, original.OIDC, manager.Get().OIDC)
	assert.Equal(t, original.Access, manager.Get().Access)
}

func TestConfigJSONCannotReplaceServerManagedWebProxy(t *testing.T) {
	router, manager := setupConfigSecurityRouter(t)
	cfg := manager.Get()
	cfg.WebProxy = config.WebProxyConfig{KnownHostsFile: "/private/known_hosts", Routes: map[string]map[string]config.WebProxyRoute{"dev": {"nacos": {Target: "http://192.0.2.10:8848/nacos/", SSHServer: "dev-server", ListenPort: 8083, PublicURL: "https://deploy.example.com:8083"}}}}
	require.NoError(t, manager.Update(cfg))
	data, err := json.Marshal(manager.Get())
	require.NoError(t, err)
	var payload map[string]interface{}
	require.NoError(t, json.Unmarshal(data, &payload))
	payload["web_proxy"] = map[string]interface{}{"known_hosts_file": "/attacker/keys"}
	data, err = json.Marshal(payload)
	require.NoError(t, err)
	request := httptest.NewRequest("PUT", "/api/config", bytes.NewReader(data))
	request.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
	assert.Equal(t, cfg.WebProxy, manager.Get().WebProxy)
	assert.NotContains(t, response.Body.String(), "known_hosts")
	assert.NotContains(t, response.Body.String(), "web_proxy")
}
