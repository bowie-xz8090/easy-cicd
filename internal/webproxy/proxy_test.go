package webproxy

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"auto-deploy-platform/internal/auth"
	"auto-deploy-platform/internal/config"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type failingTransport struct{}

func (failingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("private-connection-secret")
}

func TestGatewayFailureDoesNotLeakConnectionDetails(t *testing.T) {
	s, _, gateway := fixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("must not reach upstream") }))
	s.newTransport = func(config.ServerConfig, string, *url.URL) http.RoundTripper { return failingTransport{} }
	response := httptest.NewRecorder()
	gateway.ServeHTTP(response, httptest.NewRequest("GET", "http://cicd.example:83/nacos/", nil))
	assert.Equal(t, 502, response.Code)
	assert.NotContains(t, response.Body.String(), "private-")
	assert.Contains(t, response.Body.String(), "跳板机")
}

func TestGatewayRejectsUnregisteredHostHeader(t *testing.T) {
	_, _, gateway := fixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Error("must not forward requests from an unregistered browser origin")
	}))
	response := httptest.NewRecorder()
	gateway.ServeHTTP(response, httptest.NewRequest("GET", "http://untrusted.example:83/nacos/", nil))
	assert.Equal(t, 403, response.Code)
}

func fixture(t *testing.T, upstream http.Handler) (*Service, *config.AppConfig, http.Handler) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	server := httptest.NewServer(upstream)
	t.Cleanup(server.Close)
	cfg := &config.AppConfig{AdminToken: "private-admin", Access: config.AccessConfig{Whitelist: []string{"192.0.2.1", "127.0.0.1"}},
		Servers:  map[string]config.ServerConfig{"jump": {Host: "jump.example", Password: "private-ssh"}},
		WebProxy: config.WebProxyConfig{Routes: map[string]map[string]config.WebProxyRoute{"sit": {"nacos": {Target: server.URL + "/nacos/", PublicURL: "http://cicd.example:83", SSHServer: "jump", ListenPort: 83}}}}}
	s := New(func() *config.AppConfig { return cfg }, "")
	s.newTransport = func(config.ServerConfig, string, *url.URL) http.RoundTripper { return http.DefaultTransport }
	t.Cleanup(s.Close)
	return s, cfg, s.Gateway("sit", "nacos", auth.NewService(nil, func() *config.AppConfig { return cfg }, ""))
}

func TestGatewayPinsUpstreamAndIsolatesCookies(t *testing.T) {
	seen := make(chan *http.Request, 1)
	_, cfg, gateway := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen <- r.Clone(r.Context())
		w.Header().Add("Set-Cookie", "JSESSIONID=target-session; Domain=upstream.example; Path=/nacos; HttpOnly")
		w.Header().Add("Set-Cookie", "cicd_session=forged; Path=/")
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, `{"ok":true}`)
	}))
	request := httptest.NewRequest("POST", "http://cicd.example:83/nacos/v1/login?url=http://evil.example/", strings.NewReader("username=test"))
	request.Header.Set("Origin", "http://cicd.example:83")
	request.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
	request.Header.Set("X-CICD-Request", "1")
	request.Header.Set("X-Forwarded-For", "203.0.113.1")
	request.Header.Set("Cookie", "cicd_session=private-cicd; cicd_proxy_session=private-proxy; cicd_upstream_sit.nacos_JSESSIONID=target-session; cicd_upstream_prod.nacos_JSESSIONID=other-session; unscoped=untrusted")
	response := httptest.NewRecorder()
	gateway.ServeHTTP(response, request)
	require.Equal(t, 200, response.Code, response.Body.String())
	outbound := <-seen
	assert.NotEqual(t, "cicd.example:83", outbound.Host)
	assert.Equal(t, "/nacos/v1/login", outbound.URL.Path)
	assert.Equal(t, "url=http://evil.example/", outbound.URL.RawQuery)
	assert.Equal(t, "JSESSIONID=target-session", outbound.Header.Get("Cookie"))
	assert.Empty(t, outbound.Header.Get("Authorization"))
	assert.Empty(t, outbound.Header.Get("X-CICD-Request"))
	assert.Empty(t, outbound.Header.Get("X-Forwarded-For"))
	assert.Empty(t, response.Header().Get("Access-Control-Allow-Origin"))
	assert.Equal(t, "DENY", response.Header().Get("X-Frame-Options"))
	cookies := response.Result().Cookies()
	require.Len(t, cookies, 1)
	assert.Equal(t, "cicd_upstream_sit.nacos_JSESSIONID", cookies[0].Name)
	assert.Empty(t, cookies[0].Domain)
	assert.Equal(t, "/", cookies[0].Path)
	assert.True(t, cookies[0].HttpOnly)
}

func TestGatewayPreservesTargetAuthenticationAndRejectsCrossOriginWrites(t *testing.T) {
	calls := 0
	_, _, gateway := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		if r.Header.Get("Authorization") != "Basic dXNlcjpwYXNz" {
			w.Header().Set("WWW-Authenticate", `Basic realm="monitor"`)
			w.WriteHeader(401)
			return
		}
		w.WriteHeader(204)
	}))
	for _, test := range []struct {
		method, origin, basic string
		status                int
	}{
		{"GET", "", "", 401}, {"GET", "", "Basic dXNlcjpwYXNz", 204},
		{"POST", "http://evil.example", "Basic dXNlcjpwYXNz", 403},
		{"POST", "http://cicd.example", "Basic dXNlcjpwYXNz", 403},
		{"POST", "http://cicd.example:83", "Basic dXNlcjpwYXNz", 204},
	} {
		request := httptest.NewRequest(test.method, "http://cicd.example:83/nacos/", nil)
		request.Header.Set("Origin", test.origin)
		request.Header.Set("Authorization", test.basic)
		response := httptest.NewRecorder()
		gateway.ServeHTTP(response, request)
		assert.Equal(t, test.status, response.Code)
		if response.Code == 401 {
			assert.Equal(t, `Basic realm="monitor"`, response.Header().Get("WWW-Authenticate"))
		}
	}
	assert.Equal(t, 3, calls)
}

func TestResponseURLsUseGatewayWhileNativePathsStayUnchanged(t *testing.T) {
	var origin string
	_, cfg, gateway := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Header().Set("Location", origin+"/nacos/login")
		_, _ = io.WriteString(w, `<script src="/nacos/js/main.js"></script><a href="`+origin+`/nacos/">console</a>`)
	}))
	target, _ := url.Parse(cfg.WebProxy.Routes["sit"]["nacos"].Target)
	origin = target.Scheme + "://" + target.Host
	response := httptest.NewRecorder()
	gateway.ServeHTTP(response, httptest.NewRequest("GET", "http://cicd.example:83/nacos/", nil))
	require.Equal(t, 200, response.Code)
	assert.Contains(t, response.Body.String(), `src="/nacos/js/main.js"`)
	assert.Contains(t, response.Body.String(), `href="http://cicd.example:83/nacos/"`)
	assert.NotContains(t, response.Body.String(), origin)
	assert.Equal(t, "http://cicd.example:83/nacos/login", response.Header().Get("Location"))
	assert.Equal(t, len(response.Body.Bytes()), int(response.Result().ContentLength))
	delete(cfg.WebProxy.Routes["sit"], "nacos")
	response = httptest.NewRecorder()
	gateway.ServeHTTP(response, httptest.NewRequest("GET", "http://cicd.example:83/nacos/", nil))
	assert.Equal(t, 404, response.Code)
}

func TestLauncherOnlyRedirectsAndCannotBecomeOpenRedirect(t *testing.T) {
	s, cfg, _ := fixture(t, http.HandlerFunc(func(http.ResponseWriter, *http.Request) { t.Error("launcher must not access upstream") }))
	access := auth.NewService(nil, func() *config.AppConfig { return cfg }, "")
	router := gin.New()
	s.RegisterLauncher(router, access)
	for _, test := range []struct {
		path     string
		status   int
		location string
	}{
		{"/proxy/sit/nacos/", 302, "http://cicd.example:83/nacos/"},
		{"/proxy/sit/nacos/?next=%2Fnacos%2Flogin%3Fx%3D1", 302, "http://cicd.example:83/nacos/login?x=1"},
		{"/proxy/sit/nacos/?next=https%3A%2F%2Fevil.example", 400, ""},
		{"/proxy/sit/nacos/?next=%2F%2Fevil.example", 400, ""},
		{"/proxy/prod/unknown/", 404, ""},
	} {
		response := httptest.NewRecorder()
		router.ServeHTTP(response, httptest.NewRequest("GET", test.path, nil))
		assert.Equal(t, test.status, response.Code, response.Body.String())
		assert.Equal(t, test.location, response.Header().Get("Location"))
	}
}

func TestGatewayWebSocketUpgradeAndStream(t *testing.T) {
	_, _, gateway := fixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		connection, buffer, err := w.(http.Hijacker).Hijack()
		if err != nil {
			return
		}
		defer connection.Close()
		_, _ = buffer.WriteString("HTTP/1.1 101 Switching Protocols\r\nConnection: Upgrade\r\nUpgrade: websocket\r\n\r\n")
		_ = buffer.Flush()
		line, _ := buffer.ReadString('\n')
		_, _ = buffer.WriteString(line)
		_ = buffer.Flush()
	}))
	server := httptest.NewServer(gateway)
	defer server.Close()
	endpoint, _ := url.Parse(server.URL)
	connection, err := net.DialTimeout("tcp", endpoint.Host, time.Second)
	require.NoError(t, err)
	defer connection.Close()
	_ = connection.SetDeadline(time.Now().Add(5 * time.Second))
	_, err = fmt.Fprintf(connection, "GET /stream HTTP/1.1\r\nHost: cicd.example:83\r\nConnection: Upgrade\r\nUpgrade: websocket\r\nOrigin: http://cicd.example:83\r\n\r\n")
	require.NoError(t, err)
	reader := bufio.NewReader(connection)
	line, err := reader.ReadString('\n')
	require.NoError(t, err)
	require.Contains(t, line, "101")
	for {
		line, err = reader.ReadString('\n')
		require.NoError(t, err)
		if line == "\r\n" {
			break
		}
	}
	_, err = io.WriteString(connection, "hello\n")
	require.NoError(t, err)
	line, err = reader.ReadString('\n')
	require.NoError(t, err)
	assert.Equal(t, "hello\n", line)
}
