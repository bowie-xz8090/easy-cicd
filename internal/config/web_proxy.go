package config

import (
	"fmt"
	"net"
	"net/url"
	"regexp"
	"strconv"
	"strings"
)

// WebProxyConfig is deliberately server-managed. Each upstream gets its own
// browser origin so its scripts cannot access the CICD API or another service's
// browser storage. SSH credentials reuse the configured connection resources.
type WebProxyConfig struct {
	KnownHostsFile string                              `yaml:"known_hosts_file"`
	Routes         map[string]map[string]WebProxyRoute `yaml:"routes"`
}

type WebProxyRoute struct {
	Target     string `yaml:"target"`
	SSHServer  string `yaml:"ssh_server"`
	ListenPort int    `yaml:"listen_port"`
	PublicURL  string `yaml:"public_url"`
}

var proxyID = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]*$`)

func (c *AppConfig) validateWebProxy() error {
	ports := map[int]bool{}
	origins := map[string]bool{}
	mainURL, _ := url.Parse(c.OIDC.RedirectURI)
	mainPort := c.Server.Port
	if mainPort == 0 {
		mainPort = 8080
	}
	for environment, routes := range c.WebProxy.Routes {
		if !proxyID.MatchString(environment) {
			return fmt.Errorf("web_proxy: invalid environment key")
		}
		if _, ok := c.Environments[environment]; !ok {
			return fmt.Errorf("web_proxy: environment %q does not exist", environment)
		}
		for key, route := range routes {
			if !proxyID.MatchString(key) || route.ListenPort < 1 || route.ListenPort > 65535 || route.ListenPort == mainPort || ports[route.ListenPort] {
				return fmt.Errorf("web_proxy: each service needs a unique valid port different from CICD")
			}
			ports[route.ListenPort] = true
			if _, ok := c.Servers[route.SSHServer]; !ok {
				return fmt.Errorf("web_proxy: unknown SSH connection for %s/%s", environment, key)
			}
			target, err := url.Parse(route.Target)
			if err != nil || !proxyHTTPURL(target) || target.RawQuery != "" || target.Fragment != "" || target.RawPath != "" {
				return fmt.Errorf("web_proxy: invalid target URL for %s/%s", environment, key)
			}
			public, err := url.Parse(route.PublicURL)
			if err != nil || !proxyHTTPURL(public) || (public.Path != "" && public.Path != "/") || public.RawQuery != "" || public.Fragment != "" {
				return fmt.Errorf("web_proxy: public_url must be an HTTP origin")
			}
			origin := ProxyOrigin(route.PublicURL)
			if origins[origin] || (mainURL != nil && mainURL.Host != "" && (origin == ProxyOrigin(c.OIDC.RedirectURI) || !strings.EqualFold(public.Hostname(), mainURL.Hostname()) || public.Scheme != mainURL.Scheme)) {
				return fmt.Errorf("web_proxy: service origins must be distinct, share the CICD hostname and use its public protocol")
			}
			origins[origin] = true
			if strings.TrimSpace(c.WebProxy.KnownHostsFile) == "" {
				return fmt.Errorf("web_proxy: known_hosts_file is required")
			}
		}
	}
	return nil
}

func proxyHTTPURL(u *url.URL) bool {
	if u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || u.User != nil || strings.ContainsAny(u.Host, "\\\r\n") {
		return false
	}
	if raw := u.Port(); raw != "" {
		port, err := strconv.Atoi(raw)
		return err == nil && port > 0 && port <= 65535
	}
	return true
}

func ProxyOrigin(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || !proxyHTTPURL(u) {
		return ""
	}
	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if port == "80" && u.Scheme == "http" || port == "443" && u.Scheme == "https" {
		port = ""
	}
	if port != "" {
		host = net.JoinHostPort(host, port)
	} else if strings.Contains(host, ":") {
		host = "[" + host + "]"
	}
	return u.Scheme + "://" + host
}

// ProxyLink returns a local launcher, never an arbitrary URL supplied by a
// browser. The registered upstream target must match the configured link.
func (c *AppConfig) ProxyLink(environment, target, basePath string) string {
	for key, route := range c.WebProxy.Routes[environment] {
		if strings.TrimRight(route.Target, "/") == strings.TrimRight(target, "/") {
			return strings.TrimRight(basePath, "/") + "/proxy/" + environment + "/" + key + "/"
		}
	}
	return ""
}
