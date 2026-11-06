package api

import (
	"net/http/httptest"
	"testing"

	"auto-deploy-platform/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnvironmentResponseUsesRegisteredProxyLaunchers(t *testing.T) {
	cfg := testConfig()
	environment := cfg.Environments["dev"]
	environment.Links.Extra = []config.EnvLink{{Label: "Nacos", URL: "http://192.0.2.10:8848/nacos/"}, {Label: "Unregistered", URL: "http://192.0.2.20/"}}
	cfg.Environments["dev"] = environment
	cfg.WebProxy = config.WebProxyConfig{Routes: map[string]map[string]config.WebProxyRoute{"dev": {"nacos": {Target: environment.Links.Extra[0].URL}}}}
	router := gin.New()
	router.Use(legacyTestClient())
	NewHandler(&mockGiteaClient{}, cfg, nil).RegisterRoutesWithBasePath(router, "/cicd")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest("GET", "/cicd/api/environments", nil))
	require.Equal(t, 200, response.Code)
	assert.Contains(t, response.Body.String(), `"proxy_url":"/cicd/proxy/dev/nacos/"`)
	assert.Contains(t, response.Body.String(), `"url":"http://192.0.2.10:8848/nacos/"`)
	assert.NotContains(t, response.Body.String(), "ssh_server")
}
