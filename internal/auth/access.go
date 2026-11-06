package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"sync"
	"time"

	"auto-deploy-platform/internal/config"
	"auto-deploy-platform/internal/db"
	"auto-deploy-platform/internal/identity"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/gin-gonic/gin"
)

const (
	RequiredCode      = 40101
	ForbiddenCode     = 40301
	principalKey      = "cicd.principal"
	sessionCookie     = "cicd_session"
	transactionCookie = "cicd_oidc_tx"
	sessionTTL        = 30 * 24 * time.Hour
	transactionTTL    = 10 * time.Minute
)

// Service owns local sessions and OIDC login transactions. Configuration is read
// on each request, so whitelist edits take effect without restarting the server.
type Service struct {
	database       *sql.DB
	config         func() *config.AppConfig
	basePath       string
	client         *http.Client
	now            func() time.Time
	mu             sync.Mutex
	transactions   map[string]loginTransaction
	providerMu     sync.Mutex
	provider       *oidc.Provider
	providerConfig config.OIDCConfig
}

func NewService(database *sql.DB, source func() *config.AppConfig, basePath string) *Service {
	return &Service{database: database, config: source, basePath: strings.TrimRight(basePath, "/"),
		client: &http.Client{Timeout: 10 * time.Second, CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }},
		now:    time.Now, transactions: map[string]loginTransaction{}}
}

func (s *Service) RegisterRoutes(api *gin.RouterGroup) {
	api.GET("/auth/session", s.handleSession)
	api.GET("/auth/oidc/login", s.handleLogin)
	api.GET("/auth/oidc/callback", s.handleCallback)
}

func Principal(c *gin.Context) identity.Principal {
	value, _ := c.Get(principalKey)
	principal, _ := value.(identity.Principal)
	return principal
}

// ClientIP trusts forwarding headers only when the socket peer is explicitly
// configured as a trusted proxy. IPv4-mapped IPv6 addresses are normalized.
func ClientIP(request *http.Request, trusted []string) string {
	host, _, err := net.SplitHostPort(request.RemoteAddr)
	if err != nil {
		host = request.RemoteAddr
	}
	peer, err := netip.ParseAddr(host)
	if err != nil {
		return ""
	}
	peer = peer.Unmap()
	trusts := func(address netip.Addr) bool {
		for _, raw := range trusted {
			if prefix, err := netip.ParsePrefix(raw); err == nil && prefix.Contains(address) {
				return true
			}
			if ip, err := netip.ParseAddr(raw); err == nil && ip.Unmap() == address {
				return true
			}
		}
		return false
	}
	if !trusts(peer) {
		return peer.String()
	}
	header := request.Header.Get("X-Forwarded-For")
	if header == "" {
		header = request.Header.Get("X-Real-IP")
	}
	if header == "" {
		return peer.String()
	}
	parts := strings.Split(header, ",")
	addresses := make([]netip.Addr, len(parts))
	for index, part := range parts {
		ip, err := netip.ParseAddr(strings.TrimSpace(part))
		if err != nil {
			return peer.String()
		}
		addresses[index] = ip.Unmap()
	}
	for index := len(addresses) - 1; index >= 0; index-- {
		if !trusts(addresses[index]) || index == 0 {
			return addresses[index].String()
		}
	}
	return peer.String()
}

func (s *Service) resolve(c *gin.Context) (identity.Principal, *time.Time, error) {
	cfg := s.config()
	ip := ClientIP(c.Request, cfg.Access.TrustedProxies)
	if cfg.Access.AllowsIP(ip) {
		return identity.Principal{Type: "ip", Value: ip, IP: ip}, nil, nil
	}
	raw, err := c.Cookie(sessionCookie)
	if err != nil {
		return identity.Principal{}, nil, nil
	}
	return s.resolveToken(raw, ip)
}

func (s *Service) resolveToken(raw, ip string) (identity.Principal, *time.Time, error) {
	if raw == "" || s.database == nil {
		return identity.Principal{}, nil, nil
	}
	cfg := s.config()
	session, err := db.FindAuthSession(s.database, tokenHash(raw))
	if errors.Is(err, sql.ErrNoRows) {
		return identity.Principal{}, nil, nil
	}
	if err != nil {
		return identity.Principal{}, nil, err
	}
	if !s.now().Before(session.ExpiresAt) || session.Issuer != cfg.OIDC.Issuer || session.ClientID != cfg.OIDC.ClientID {
		return identity.Principal{}, nil, nil
	}
	phone, err := identity.NormalizePhone(session.Phone)
	if err != nil {
		return identity.Principal{}, nil, err
	}
	return identity.Principal{Type: "oidc", Value: phone, IP: ip}, &session.ExpiresAt, nil
}

func (s *Service) authorized(principal identity.Principal) bool {
	access := s.config().Access
	if principal.Type == "ip" {
		return access.AllowsIP(principal.IP)
	}
	return principal.Type == "oidc" && access.AllowsPhone(principal.Value)
}

func (s *Service) handleSession(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	principal, expires, err := s.resolve(c)
	if err != nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"code": -1, "message": "暂时无法读取登录状态，请重试"})
		return
	}
	mode := principal.Type
	if mode == "" {
		mode = "anonymous"
	}
	display := ""
	if principal.Type != "" {
		display = principal.Display()
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"authenticated": principal.Type != "", "authorized": s.authorized(principal), "mode": mode, "display_name": display, "expires_at": expires}})
}

func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Cache-Control", "no-store")
		principal, _, err := s.resolve(c)
		if err != nil {
			c.AbortWithStatusJSON(http.StatusServiceUnavailable, gin.H{"code": -1, "message": "暂时无法验证登录状态"})
			return
		}
		if principal.Type == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"code": RequiredCode, "message": "请登录后再操作"})
			return
		}
		if !s.authorized(principal) {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": ForbiddenCode, "message": "没有访问权限"})
			return
		}
		c.Set(principalKey, principal)
		if c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead && c.Request.Method != http.MethodOptions {
			if !s.validWrite(c) {
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"code": -1, "message": "请求来源验证失败"})
				return
			}
		}
		c.Next()
	}
}

func (s *Service) validWrite(c *gin.Context) bool {
	if c.GetHeader("X-CICD-Request") != "1" {
		return false
	}
	if c.GetHeader("Sec-Fetch-Site") == "cross-site" {
		return false
	}
	origin := c.GetHeader("Origin")
	if origin == "" {
		return true
	} // Non-browser clients still need the custom header and access identity.
	expected, err := url.Parse(s.config().OIDC.RedirectURI)
	if err != nil || expected.Host == "" {
		scheme := "http"
		if c.Request.TLS != nil {
			scheme = "https"
		}
		return origin == scheme+"://"+c.Request.Host
	}
	return origin == expected.Scheme+"://"+expected.Host
}

func randomToken() (string, error) {
	var value [32]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(value[:]), nil
}

func tokenHash(value string) string {
	hash := sha256.Sum256([]byte(value))
	return hex.EncodeToString(hash[:])
}

func (s *Service) setCookie(c *gin.Context, name, value string, expires time.Time, maxAge int) {
	u, _ := url.Parse(s.config().OIDC.RedirectURI)
	secure := u != nil && u.Scheme == "https"
	http.SetCookie(c.Writer, &http.Cookie{Name: name, Value: value, Path: s.basePath + "/", Expires: expires, MaxAge: maxAge, HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}
