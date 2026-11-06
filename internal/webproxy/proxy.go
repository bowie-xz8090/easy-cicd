package webproxy

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io"
	"mime"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"

	"auto-deploy-platform/internal/auth"
	"auto-deploy-platform/internal/config"
	"github.com/gin-gonic/gin"
)

type transportEntry struct {
	signature string
	transport *sshTransport
}

type Service struct {
	source     func() *config.AppConfig
	basePath   string
	mu         sync.Mutex
	transports map[string]transportEntry
	// Tests can exercise response handling without creating an SSH connection.
	newTransport func(config.ServerConfig, string, *url.URL) http.RoundTripper
}

func New(source func() *config.AppConfig, basePath string) *Service {
	return &Service{source: source, basePath: strings.TrimRight(basePath, "/"), transports: map[string]transportEntry{}}
}

func (s *Service) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, entry := range s.transports {
		entry.transport.Close()
	}
	s.transports = map[string]transportEntry{}
}

func (s *Service) launcher(environment, key string) string {
	return s.basePath + "/proxy/" + environment + "/" + key + "/"
}

// RegisterLauncher serves redirects and first-party login messages only. No
// upstream HTML or script is ever served from the CICD origin.
func (s *Service) RegisterLauncher(router *gin.Engine, access *auth.Service) {
	if len(s.source().WebProxy.Routes) == 0 {
		return
	}
	returnTo := func(c *gin.Context) string { return c.Request.URL.RequestURI() }
	group := router.Group(s.basePath+"/proxy", access.BrowserMiddleware(returnTo))
	launch := func(c *gin.Context) {
		route, ok := s.source().WebProxy.Routes[c.Param("environment")][c.Param("service")]
		if !ok {
			c.Status(404)
			return
		}
		if access.ShareProxySession(c) != nil {
			c.Status(503)
			return
		}
		target, _ := url.Parse(route.Target)
		destination, _ := url.Parse(strings.TrimRight(route.PublicURL, "/"))
		destination.Path = target.Path
		if destination.Path == "" {
			destination.Path = "/"
		}
		if raw := c.Query("next"); raw != "" {
			next, err := url.Parse(raw)
			if err != nil || next.IsAbs() || next.Host != "" || !strings.HasPrefix(next.Path, "/") || strings.HasPrefix(next.Path, "//") || strings.ContainsAny(raw, "\\\r\n") {
				c.Status(400)
				return
			}
			destination.Path = next.Path
			destination.RawQuery = next.RawQuery
		}
		c.Header("Referrer-Policy", "no-referrer")
		c.Redirect(http.StatusFound, destination.String())
	}
	group.GET("/:environment/:service", launch)
	group.GET("/:environment/:service/*path", launch)
}

// Gateway is bound to a dedicated listener and one fixed configured service.
// It cannot route to another service through the request URL or Host header.
func (s *Service) Gateway(environment, key string, access *auth.Service) http.Handler {
	router := gin.New()
	_ = router.SetTrustedProxies(nil)
	configured := s.source().WebProxy.Routes[environment][key]
	expectedOrigin := config.ProxyOrigin(configured.PublicURL)
	publicURL, _ := url.Parse(configured.PublicURL)
	returnTo := func(c *gin.Context) string {
		return s.launcher(environment, key) + "?next=" + url.QueryEscape(c.Request.URL.RequestURI())
	}
	router.Use(func(c *gin.Context) {
		if config.ProxyOrigin(publicURL.Scheme+"://"+c.Request.Host) != expectedOrigin {
			c.AbortWithStatus(403)
			return
		}
		c.Next()
	}, access.ProxyMiddleware(expectedOrigin, returnTo))
	router.NoRoute(func(c *gin.Context) { s.serve(c, environment, key) })
	return router
}

func (s *Service) serve(c *gin.Context, environment, key string) {
	if c.Request.Method == http.MethodConnect || c.Request.Method == http.MethodTrace {
		c.Status(http.StatusMethodNotAllowed)
		return
	}
	cfg := s.source()
	route, ok := cfg.WebProxy.Routes[environment][key]
	if !ok {
		c.Status(404)
		return
	}
	target, err := url.Parse(route.Target)
	if err != nil {
		proxyError(c.Writer)
		return
	}
	root := *target
	root.Path = ""
	root.RawPath = ""
	transport := s.transport(environment+"/"+key, cfg, route, target)
	proxy := &httputil.ReverseProxy{
		Transport: transport, FlushInterval: -1,
		Rewrite: func(request *httputil.ProxyRequest) {
			request.SetURL(&root)
			request.Out.Host = target.Host
			request.Out.Header.Set("Accept-Encoding", "identity")
			request.Out.Header.Del("X-CICD-Request")
			request.Out.Header.Del("Forwarded")
			for name := range request.Out.Header {
				if strings.HasPrefix(strings.ToLower(name), "x-forwarded-") {
					request.Out.Header.Del(name)
				}
			}
			if request.Out.Header.Get("Authorization") == "Bearer "+cfg.AdminToken && cfg.AdminToken != "" {
				request.Out.Header.Del("Authorization")
			}
			if request.Out.Header.Get("Origin") != "" {
				request.Out.Header.Set("Origin", root.Scheme+"://"+root.Host)
			}
			request.Out.Header.Del("Referer")
			rewriteRequestCookies(request.Out, environment, key)
		},
		ModifyResponse: func(response *http.Response) error { return adaptResponse(response, route, environment, key) },
		ErrorHandler:   func(writer http.ResponseWriter, _ *http.Request, _ error) { proxyError(writer) },
	}
	ctx, cancel := context.WithCancel(c.Request.Context())
	defer cancel()
	proxy.ServeHTTP(c.Writer, c.Request.WithContext(ctx))
}

func proxyError(writer http.ResponseWriter) {
	writer.Header().Set("Content-Type", "text/plain; charset=utf-8")
	writer.Header().Set("Cache-Control", "no-store")
	writer.WriteHeader(http.StatusBadGateway)
	_, _ = io.WriteString(writer, "服务暂时无法通过跳板机访问，请稍后重试或联系管理员。")
}

func (s *Service) transport(key string, cfg *config.AppConfig, route config.WebProxyRoute, target *url.URL) http.RoundTripper {
	server := cfg.Servers[route.SSHServer]
	if s.newTransport != nil {
		return s.newTransport(server, cfg.WebProxy.KnownHostsFile, target)
	}
	hash := sha256.Sum256([]byte(server.Host + "\x00" + strconv.Itoa(server.Port) + "\x00" + server.User + "\x00" + server.Password + "\x00" + cfg.WebProxy.KnownHostsFile + "\x00" + route.Target))
	signature := hex.EncodeToString(hash[:])
	s.mu.Lock()
	defer s.mu.Unlock()
	if entry, ok := s.transports[key]; ok {
		if entry.signature == signature {
			return entry.transport
		}
		entry.transport.Close()
	}
	transport := newSSHTransport(server, cfg.WebProxy.KnownHostsFile, target)
	s.transports[key] = transportEntry{signature, transport}
	return transport
}

func cookiePrefix(environment, key string) string {
	return "cicd_upstream_" + environment + "." + key + "_"
}

func rewriteRequestCookies(request *http.Request, environment, key string) {
	cookies := request.Cookies()
	request.Header.Del("Cookie")
	prefix := cookiePrefix(environment, key)
	for _, cookie := range cookies {
		if strings.HasPrefix(cookie.Name, prefix) {
			cookie.Name = strings.TrimPrefix(cookie.Name, prefix)
			if !strings.HasPrefix(strings.ToLower(cookie.Name), "cicd_") {
				request.AddCookie(cookie)
			}
		}
	}
}

func adaptResponse(response *http.Response, route config.WebProxyRoute, environment, key string) error {
	target, _ := url.Parse(route.Target)
	origin := target.Scheme + "://" + target.Host
	public := strings.TrimRight(route.PublicURL, "/")
	for _, name := range []string{"Location", "Refresh"} {
		if value := response.Header.Get(name); value != "" {
			value = strings.ReplaceAll(value, origin, public)
			if strings.HasPrefix(value, "//"+target.Host+"/") {
				value = public + strings.TrimPrefix(value, "//"+target.Host)
			}
			response.Header.Set(name, value)
		}
	}
	for name := range response.Header {
		if strings.HasPrefix(strings.ToLower(name), "access-control-") {
			response.Header.Del(name)
		}
	}
	cookies := response.Cookies()
	response.Header.Del("Set-Cookie")
	for _, cookie := range cookies {
		if strings.HasPrefix(strings.ToLower(cookie.Name), "cicd_") {
			continue
		}
		cookie.Name = cookiePrefix(environment, key) + cookie.Name
		cookie.Domain = ""
		cookie.Path = "/"
		response.Header.Add("Set-Cookie", cookie.String())
	}
	response.Header.Set("Cache-Control", "no-store")
	response.Header.Set("Referrer-Policy", "no-referrer")
	response.Header.Set("X-Frame-Options", "DENY")
	response.Header.Add("Content-Security-Policy", "frame-ancestors 'none'")
	contentType, _, _ := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if contentType != "text/html" && contentType != "text/css" && contentType != "application/javascript" && contentType != "text/javascript" && contentType != "application/x-javascript" {
		return nil
	}
	if encoding := response.Header.Get("Content-Encoding"); encoding != "" && encoding != "identity" {
		return errors.New("unsupported upstream text encoding")
	}
	const limit = 16 * 1024 * 1024
	data, err := io.ReadAll(io.LimitReader(response.Body, limit+1))
	_ = response.Body.Close()
	if err != nil || len(data) > limit {
		return errors.New("upstream text response unavailable")
	}
	data = bytes.ReplaceAll(data, []byte(origin), []byte(public))
	data = bytes.ReplaceAll(data, []byte(strings.ReplaceAll(origin, "/", "\\/")), []byte(strings.ReplaceAll(public, "/", "\\/")))
	response.Body = io.NopCloser(bytes.NewReader(data))
	response.ContentLength = int64(len(data))
	response.Header.Set("Content-Length", strconv.Itoa(len(data)))
	response.Header.Del("ETag")
	response.Header.Del("Content-MD5")
	return nil
}
