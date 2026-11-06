package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"auto-deploy-platform/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func accessRouter(service *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	api := router.Group("/api")
	service.RegisterRoutes(api)
	api.Use(service.Middleware())
	api.GET("/private", func(c *gin.Context) { c.JSON(200, gin.H{"display": Principal(c).Display()}) })
	api.POST("/private", func(c *gin.Context) { c.Status(204) })
	return router
}

func TestWhitelistBoundariesAndForwardedHeaders(t *testing.T) {
	cfg := &config.AppConfig{Access: config.AccessConfig{Whitelist: []string{"192.0.2.1", "192.0.2.2", "192.0.2.3", "192.0.2.4", "192.0.2.5", "192.0.2.6", "192.0.2.7", "192.0.2.8", "192.0.2.9", "192.0.2.10"}}}
	service := NewService(nil, func() *config.AppConfig { return cfg }, "")
	router := accessRouter(service)
	for _, test := range []struct {
		ip, forwarded string
		status        int
	}{
		{"192.0.2.1:1234", "", 200}, {"192.0.2.10:1234", "", 200},
		{"[::ffff:192.0.2.1]:1234", "", 200}, {"192.0.2.0:1234", "", 401}, {"192.0.2.11:1234", "", 401},
		{"198.51.100.1:1234", "192.0.2.1", 401}, {"127.0.0.1:1234", "192.0.2.1", 401},
	} {
		t.Run(test.ip+test.forwarded, func(t *testing.T) {
			request := httptest.NewRequest("GET", "/api/private", nil)
			request.RemoteAddr = test.ip
			request.Header.Set("X-Forwarded-For", test.forwarded)
			request.Header.Set("X-Real-IP", test.forwarded)
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, test.status, response.Code)
			if test.status == 401 {
				assert.Contains(t, response.Body.String(), `"code":40101`)
			}
		})
	}
	// The service must read the current whitelist for every request.
	cfg.Access.Whitelist = nil
	request := httptest.NewRequest("GET", "/api/private", nil)
	request.RemoteAddr = "192.0.2.1:1234"
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, 401, response.Code)
}

func TestClientIPExplicitTrustedProxy(t *testing.T) {
	for _, test := range []struct {
		peer, forwarded string
		trusted         []string
		expected        string
	}{
		{"10.0.0.2:1234", "192.0.2.1", nil, "10.0.0.2"},
		{"10.0.0.2:1234", "192.0.2.1", []string{"10.0.0.0/24"}, "192.0.2.1"},
		{"10.0.0.2:1234", "192.0.2.1, 198.51.100.2", []string{"10.0.0.2"}, "198.51.100.2"},
		{"10.0.0.2:1234", "invalid", []string{"10.0.0.2"}, "10.0.0.2"},
		{"invalid", "192.0.2.1", []string{"10.0.0.2"}, ""},
	} {
		request := httptest.NewRequest("GET", "/", nil)
		request.RemoteAddr = test.peer
		request.Header.Set("X-Forwarded-For", test.forwarded)
		assert.Equal(t, test.expected, ClientIP(request, test.trusted))
	}
}

func TestWriteRequestGuard(t *testing.T) {
	cfg := &config.AppConfig{Access: config.AccessConfig{Whitelist: []string{"192.0.2.1"}}, OIDC: config.OIDCConfig{RedirectURI: "http://cicd.example/api/auth/oidc/callback"}}
	router := accessRouter(NewService(nil, func() *config.AppConfig { return cfg }, ""))
	for _, test := range []struct {
		header, origin, fetch string
		status                int
	}{
		{"", "", "", 403}, {"1", "https://evil.example", "", 403}, {"1", "null", "", 403},
		{"1", "http://cicd.example", "cross-site", 403}, {"1", "http://cicd.example", "same-origin", 204}, {"1", "", "", 204},
	} {
		request := httptest.NewRequest("POST", "http://cicd.example/api/private", nil)
		request.Header.Set("X-CICD-Request", test.header)
		request.Header.Set("Origin", test.origin)
		request.Header.Set("Sec-Fetch-Site", test.fetch)
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		assert.Equal(t, test.status, response.Code)
	}
}

func TestSafeReturnAndUnavailableOIDC(t *testing.T) {
	service := NewService(nil, func() *config.AppConfig { return &config.AppConfig{} }, "/cicd")
	for _, raw := range []string{"https://evil.example", "//evil.example", "/\\evil.example", "/outside", "/cicd/../outside", "/cicd/api/auth/oidc/login"} {
		assert.Equal(t, "/cicd/", service.safeReturn(raw))
	}
	assert.Equal(t, "/cicd/history?project=test#record", service.safeReturn("/cicd/history?project=test#record"))
	router := accessRouter(service)
	request := httptest.NewRequest(http.MethodGet, "/api/auth/oidc/login", nil)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusServiceUnavailable, response.Code)
	assert.Contains(t, response.Body.String(), "SSO")
}
