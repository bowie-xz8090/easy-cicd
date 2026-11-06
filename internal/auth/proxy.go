package auth

import (
	"context"
	"html"
	"net/http"
	"net/url"
	"strings"

	"github.com/gin-gonic/gin"
)

const proxySessionCookie = "cicd_proxy_session"

// BrowserMiddleware is used only by the main site's GET-only proxy launcher.
func (s *Service) BrowserMiddleware(returnTo func(*gin.Context) string) gin.HandlerFunc {
	return s.browserMiddleware("", returnTo)
}

// ProxyMiddleware permits native target-app forms without changing the CICD
// API's custom-header requirement. Writes must originate at this gateway.
func (s *Service) ProxyMiddleware(origin string, returnTo func(*gin.Context) string) gin.HandlerFunc {
	return s.browserMiddleware(origin, returnTo)
}

func (s *Service) browserMiddleware(origin string, returnTo func(*gin.Context) string) gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		principal, expires, err := s.resolve(c)
		if principal.Type == "" && origin != "" && err == nil {
			raw, _ := c.Cookie(proxySessionCookie)
			principal, expires, err = s.resolveToken(raw, ClientIP(c.Request, s.config().Access.TrustedProxies))
		}
		if err != nil {
			c.AbortWithStatusJSON(503, gin.H{"code": -1, "message": "暂时无法验证登录状态"})
			return
		}
		if principal.Type == "" || !s.authorized(principal) {
			code, status, title := ForbiddenCode, http.StatusForbidden, "没有访问权限"
			if principal.Type == "" {
				code, status, title = RequiredCode, http.StatusUnauthorized, "请先登录 CICD"
			}
			if !strings.Contains(c.GetHeader("Accept"), "text/html") {
				c.AbortWithStatusJSON(status, gin.H{"code": code, "message": title})
				return
			}
			mainURL, _ := url.Parse(s.config().OIDC.RedirectURI)
			mainOrigin := ""
			if mainURL != nil && mainURL.Host != "" {
				mainOrigin = mainURL.Scheme + "://" + mainURL.Host
			}
			back := s.safeReturn(returnTo(c))
			login := mainOrigin + back
			if origin == "" {
				login = mainOrigin + s.basePath + "/api/auth/oidc/login?return_to=" + url.QueryEscape(back)
			}
			action, label := c.Request.URL.RequestURI(), "刷新权限"
			message := "当前账号没有访问权限。加入白名单后刷新即可访问，无需重新登录。"
			if status == http.StatusUnauthorized {
				action, label = login, "登录"
				message = "请使用 CICD 登录后访问此服务。"
			}
			c.Header("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; base-uri 'none'; frame-ancestors 'none'")
			c.Header("Referrer-Policy", "no-referrer")
			c.Data(status, "text/html; charset=utf-8", []byte(`<!doctype html><html lang="zh-CN"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>`+title+`</title><style>body{margin:0;min-height:100vh;display:grid;place-items:center;background:#f5f7fb;font:16px/1.6 system-ui;color:#172033}main{max-width:420px;margin:20px;padding:32px;border-radius:12px;background:white;box-shadow:0 8px 32px #0f172a16}h1{font-size:24px;margin-top:0}p{color:#64748b}a{display:inline-block;padding:8px 20px;border-radius:6px;background:#2563eb;color:white;text-decoration:none}</style><main role="alertdialog"><h1>`+title+`</h1><p>`+message+`</p><a href="`+html.EscapeString(action)+`">`+label+`</a></main></html>`))
			c.Abort()
			return
		}
		if origin != "" && (c.GetHeader("Sec-Fetch-Site") == "cross-site" || ((c.Request.Method != "GET" && c.Request.Method != "HEAD" && c.Request.Method != "OPTIONS") || strings.EqualFold(c.GetHeader("Upgrade"), "websocket")) && !proxyWriteOrigin(c, origin)) {
			c.AbortWithStatusJSON(403, gin.H{"code": -1, "message": "请求来源验证失败"})
			return
		}
		c.Set(principalKey, principal)
		if origin != "" && expires != nil {
			ctx, cancel := context.WithDeadline(c.Request.Context(), *expires)
			defer cancel()
			c.Request = c.Request.WithContext(ctx)
		}
		c.Next()
	}
}

func proxyWriteOrigin(c *gin.Context, expected string) bool {
	if origin := c.GetHeader("Origin"); origin != "" {
		return origin == expected
	}
	return c.GetHeader("X-CICD-Request") == "1"
}

// ShareProxySession bridges an existing session to native upstream paths when
// CICD itself has a URL prefix. The random token and absolute expiry stay fixed.
func (s *Service) ShareProxySession(c *gin.Context) error {
	raw, _ := c.Cookie(sessionCookie)
	_, expires, err := s.resolveToken(raw, "")
	if err != nil || expires == nil {
		return err
	}
	public, _ := url.Parse(s.config().OIDC.RedirectURI)
	http.SetCookie(c.Writer, &http.Cookie{Name: proxySessionCookie, Value: raw, Path: "/", HttpOnly: true,
		Secure: public != nil && public.Scheme == "https", SameSite: http.SameSiteLaxMode,
		Expires: *expires, MaxAge: int(expires.Sub(s.now()).Seconds())})
	return nil
}
