package config

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAppConfigJSONExcludesSecrets(t *testing.T) {
	cfg := &AppConfig{
		AdminToken: "private-admin-token",
		OIDC:       OIDCConfig{ClientID: "test-client", ClientSecret: "private-oidc-client-secret"},
		Gitea: GiteaConfig{
			URL: "https://gitea.example.com", Username: "git-user", Token: "private-gitea-token",
		},
		Servers: map[string]ServerConfig{
			"dev": {Host: "192.0.2.1", Password: "private-ssh-password"},
		},
		Environments: map[string]EnvConfig{
			"dev": {Password: "private-deploy-password"},
		},
	}
	data, err := json.Marshal(cfg)
	require.NoError(t, err)
	for _, secret := range []string{cfg.AdminToken, cfg.Gitea.Token, cfg.OIDC.ClientSecret, cfg.Servers["dev"].Password, cfg.Environments["dev"].Password} {
		assert.NotContains(t, string(data), secret)
	}
	assert.NotContains(t, string(data), `"token":`)
	assert.NotContains(t, string(data), `"password":`)
	assert.NotContains(t, string(data), `"admin_token":`)
	assert.NotContains(t, string(data), `"oidc":`)
	assert.Contains(t, string(data), cfg.Gitea.URL)
	assert.Contains(t, string(data), cfg.Gitea.Username)
	data, err = json.Marshal(cfg.OIDC)
	require.NoError(t, err)
	assert.NotContains(t, string(data), cfg.OIDC.ClientSecret)
	assert.NotContains(t, string(data), `"client_secret":`)
}
