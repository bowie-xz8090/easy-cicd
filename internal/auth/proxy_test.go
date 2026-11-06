package auth

import (
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProxySessionBridgeDoesNotExtendLoginOrAuthorizeCICDAPI(t *testing.T) {
	f := newOIDCFixture(t)
	f.cfg.Access.Whitelist = []string{"18223452780"}
	state, binding := f.begin(t)
	loggedIn := f.callback(state, binding)
	original := cookieNamed(t, loggedIn, sessionCookie)
	f.now = f.now.Add(24 * time.Hour)
	core := gin.New()
	core.GET("/bridge", f.service.BrowserMiddleware(func(*gin.Context) string { return "/proxy/sit/nacos/" }), func(c *gin.Context) { require.NoError(t, f.service.ShareProxySession(c)); c.Status(204) })
	request := httptest.NewRequest("GET", "/bridge", nil)
	request.AddCookie(original)
	response := httptest.NewRecorder()
	core.ServeHTTP(response, request)
	require.Equal(t, 204, response.Code)
	bridged := cookieNamed(t, response, proxySessionCookie)
	assert.Equal(t, original.Value, bridged.Value)
	assert.Equal(t, original.Expires, bridged.Expires)
	assert.Equal(t, 29*24*60*60, bridged.MaxAge)
	assert.Equal(t, "/", bridged.Path)
	assert.True(t, bridged.HttpOnly)
	proxy := gin.New()
	proxy.Use(f.service.ProxyMiddleware("http://cicd.example:83", func(*gin.Context) string { return "/proxy/sit/nacos/" }))
	proxy.NoRoute(func(c *gin.Context) { c.JSON(200, gin.H{"display": Principal(c).Display()}) })
	for _, test := range []struct {
		method, origin string
		status         int
	}{
		{"GET", "", 200}, {"POST", "http://cicd.example:83", 200}, {"POST", "http://cicd.example", 403}, {"POST", "http://evil.example", 403},
	} {
		request := httptest.NewRequest(test.method, "/nacos/", nil)
		request.AddCookie(bridged)
		request.Header.Set("Origin", test.origin)
		response := httptest.NewRecorder()
		proxy.ServeHTTP(response, request)
		assert.Equal(t, test.status, response.Code)
		assert.NotContains(t, response.Body.String(), original.Value)
		assert.NotContains(t, response.Body.String(), "18223452780")
	}
	request = httptest.NewRequest("GET", "/api/private", nil)
	request.AddCookie(bridged)
	response = httptest.NewRecorder()
	f.router.ServeHTTP(response, request)
	assert.Equal(t, 401, response.Code)
	f.cfg.Access.Whitelist = nil
	request = httptest.NewRequest("GET", "/nacos/", nil)
	request.AddCookie(bridged)
	request.Header.Set("Accept", "text/html")
	response = httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	assert.Equal(t, 403, response.Code)
	assert.Contains(t, response.Body.String(), "没有访问权限")
	assert.Empty(t, response.Result().Cookies())
	f.cfg.Access.Whitelist = []string{"18223452780"}
	response = httptest.NewRecorder()
	proxy.ServeHTTP(response, request)
	assert.Equal(t, 200, response.Code)
}
