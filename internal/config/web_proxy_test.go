package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func proxyConfig(t *testing.T) *AppConfig {
	t.Helper()
	cfg, err := Load(writeTestConfig(t, validYAML))
	require.NoError(t, err)
	cfg.OIDC = OIDCConfig{Issuer: "http://sso.example", DiscoveryURL: "http://sso.example/.well-known/openid-configuration", ClientID: "client", ClientSecret: "private-secret", RedirectURI: "http://cicd.example:9090/api/auth/oidc/callback"}
	cfg.WebProxy = WebProxyConfig{KnownHostsFile: "private-known-hosts-path", Routes: map[string]map[string]WebProxyRoute{"sit": {"nacos": {Target: "http://192.0.2.10:8848/nacos/", SSHServer: "sit-server", ListenPort: 9091, PublicURL: "http://cicd.example:9091"}}}}
	return cfg
}

func TestWebProxyValidationAndJSONProtection(t *testing.T) {
	cfg := proxyConfig(t)
	require.NoError(t, cfg.Validate())
	assert.Equal(t, "/cicd/proxy/sit/nacos/", cfg.ProxyLink("sit", "http://192.0.2.10:8848/nacos", "/cicd"))
	assert.Empty(t, cfg.ProxyLink("dev", "http://192.0.2.10:8848/nacos/", ""))
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	assert.NotContains(t, string(data), "web_proxy")
	assert.NotContains(t, string(data), "private-known-hosts-path")
	for _, name := range []string{"main-port", "duplicate-port", "main-origin", "duplicate-origin", "other-host", "unknown-ssh", "unknown-environment", "unsafe-target", "invalid-port", "missing-host-key"} {
		t.Run(name, func(t *testing.T) {
			cfg := proxyConfig(t)
			route := cfg.WebProxy.Routes["sit"]["nacos"]
			switch name {
			case "main-port":
				route.ListenPort = 9090
			case "duplicate-port":
				cfg.WebProxy.Routes["sit"]["other"] = route
			case "main-origin":
				route.PublicURL = "http://cicd.example:9090"
			case "duplicate-origin":
				other := route
				other.ListenPort = 9092
				cfg.WebProxy.Routes["sit"]["other"] = other
			case "other-host":
				route.PublicURL = "http://elsewhere.example:9091"
			case "unknown-ssh":
				route.SSHServer = "missing"
			case "unknown-environment":
				cfg.WebProxy.Routes["unknown"] = cfg.WebProxy.Routes["sit"]
			case "unsafe-target":
				route.Target = "http://user:password@192.0.2.10:8848/nacos/"
			case "invalid-port":
				route.Target = "http://192.0.2.10:65536/"
			case "missing-host-key":
				cfg.WebProxy.KnownHostsFile = ""
			}
			cfg.WebProxy.Routes["sit"]["nacos"] = route
			require.Error(t, cfg.Validate())
		})
	}
}
