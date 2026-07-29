package config

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const validYAML = `
config_version: 2
server:
  port: 9090
  workspace: "/tmp/workspace"

gitea:
  url: "http://gitea.example.com"
  username: "git-user"
  token: "test-token"

servers:
  dev-server:
    host: "192.168.1.10"
    port: 22
    user: "deploy"
    deploy_path: "/opt/apps"
  sit-server:
    host: "192.168.1.20"
    port: 22
    user: "deploy"
    deploy_path: "/opt/apps"

environments:
  dev:
    label: "开发环境"
    servers: ["dev-server"]
  sit:
    label: "集成测试环境"
    servers: ["sit-server"]

projects:
  my-project:
    label: "我的项目"
    mode: single
    build_type: frontend
    environments:
      dev:
        build:
          command: "npm run build:dev"
          artifacts:
            - key: web
              output: "./dist"
              upload_as: "web.tar.gz"
        deployments:
          dev-server:
            label: "开发服务"
            script: "echo deploy"
      sit:
        build:
          command: "npm run build:sit"
          artifacts:
            - key: web
              output: "./dist"
              upload_as: "web.tar.gz"
        deployments:
          sit-server:
            label: "测试服务"
            script: "echo deploy"
`

func writeTestConfig(t *testing.T, content string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	err := os.WriteFile(path, []byte(content), 0644)
	require.NoError(t, err)
	return path
}

func TestLoad_ValidConfig(t *testing.T) {
	path := writeTestConfig(t, validYAML)
	cfg, err := Load(path)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	assert.Equal(t, 9090, cfg.Server.Port)
	assert.Equal(t, "/tmp/workspace", cfg.Server.Workspace)
	assert.Equal(t, "http://gitea.example.com", cfg.Gitea.URL)
	assert.Equal(t, "git-user", cfg.Gitea.Username)
	assert.Equal(t, "test-token", cfg.Gitea.Token)
	assert.Len(t, cfg.Environments, 2)
	assert.Len(t, cfg.Projects, 1)
}

func TestLoad_MissingFile(t *testing.T) {
	_, err := Load("/nonexistent/path/config.yaml")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "config file not found")
}

func TestLoad_MalformedYAML(t *testing.T) {
	path := writeTestConfig(t, `{{{invalid yaml:::`)
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid YAML format")
}

func TestLoad_MissingGiteaURL(t *testing.T) {
	yaml := `
gitea:
  url: ""
  token: "some-token"
environments:
  dev:
    label: "dev"
    server:
      host: "localhost"
`
	path := writeTestConfig(t, yaml)
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitea.url must not be empty")
}

func TestLoad_MissingGiteaToken(t *testing.T) {
	yaml := `
gitea:
  url: "http://gitea.example.com"
  token: ""
environments:
  dev:
    label: "dev"
    server:
      host: "localhost"
`
	path := writeTestConfig(t, yaml)
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "gitea.token must not be empty")
}

func TestLoad_NoEnvironments(t *testing.T) {
	yaml := `
gitea:
  url: "http://gitea.example.com"
  token: "some-token"
environments:
`
	path := writeTestConfig(t, yaml)
	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "at least one environment must be defined")
}

func TestLoad_DefaultServerPort(t *testing.T) {
	yaml := `
gitea:
  url: "http://gitea.example.com"
  token: "some-token"
environments:
  dev:
    label: "dev"
    server:
      host: "localhost"
`
	path := writeTestConfig(t, yaml)
	cfg, err := Load(path)
	require.NoError(t, err)
	assert.Equal(t, 8080, cfg.Server.Port)
	assert.Equal(t, "./workspace", cfg.Server.Workspace)
}

func TestGetGiteaConfig(t *testing.T) {
	path := writeTestConfig(t, validYAML)
	cfg, err := Load(path)
	require.NoError(t, err)

	gitea := cfg.GetGiteaConfig()
	assert.Equal(t, "http://gitea.example.com", gitea.URL)
	assert.Equal(t, "test-token", gitea.Token)
}

func TestResolveDeployment_Exists(t *testing.T) {
	path := writeTestConfig(t, validYAML)
	cfg, err := Load(path)
	require.NoError(t, err)

	resolved, err := cfg.ResolveDeployment("my-project", "", "dev", "dev-server")
	require.NoError(t, err)
	assert.Equal(t, "192.168.1.10", resolved.Server.Host)
	assert.Equal(t, "npm run build:dev", resolved.Build.BuildCmd)
	assert.Equal(t, "开发服务", resolved.TargetLabel)
}

func TestResolveDeployment_ServerBuildCommandOverridesOnlyCommand(t *testing.T) {
	path := writeTestConfig(t, validYAML)
	cfg, err := Load(path)
	require.NoError(t, err)

	project := cfg.Projects["my-project"]
	dev := project.Environments["dev"]
	deployment := dev.Deployments["dev-server"]
	deployment.BuildCommand = "API_BASE=/special npm run build:dev"
	dev.Deployments["dev-server"] = deployment
	project.Environments["dev"] = dev
	cfg.Projects["my-project"] = project

	resolved, err := cfg.ResolveDeployment("my-project", "", "dev", "dev-server")
	require.NoError(t, err)
	assert.Equal(t, "API_BASE=/special npm run build:dev", resolved.Build.BuildCmd)
	require.Len(t, resolved.Build.Artifacts, 1)
	assert.Equal(t, "./dist", resolved.Build.Artifacts[0].BuildOutput)
	assert.Equal(t, "web.tar.gz", resolved.Build.Artifacts[0].RenameTo)
}

func TestResolveDeployment_NotFound(t *testing.T) {
	path := writeTestConfig(t, validYAML)
	cfg, err := Load(path)
	require.NoError(t, err)

	_, err = cfg.ResolveDeployment("my-project", "", "staging", "dev-server")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `environment "staging" not configured`)
}

func TestGetProjectConfig_Exists(t *testing.T) {
	path := writeTestConfig(t, validYAML)
	cfg, err := Load(path)
	require.NoError(t, err)

	proj, err := cfg.GetProjectConfig("my-project")
	require.NoError(t, err)
	assert.Equal(t, "我的项目", proj.Label)
	assert.Equal(t, "single", proj.Mode)
	assert.Len(t, proj.Environments, 2)
}

func TestLoad_NormalizesLegacyAliasesByRealAppsPath(t *testing.T) {
	legacy := `
gitea:
  url: "http://gitea.example.com"
  token: "test-token"
servers:
  prod-main:
    host: "10.0.0.1"
  prod-alt:
    host: "10.0.0.2"
environments:
  prod:
    label: "生产环境"
projects:
  monorepo:
    label: "前端"
    sub_projects:
      cde-onlyone-ui:
        label: "公服用户端"
        env_overrides:
          prod:
            build_cmd: "cd apps/cde-onlyone-ui && pnpm build:prod"
            artifacts:
              - build_output: "./apps/cde-onlyone-ui/dist"
                rename_to: "onlyone.tar.gz"
                deploy_script: "echo main"
            server: "prod-main"
      mult-onlyone-ui:
        label: "数据要素X用户端"
        env_overrides:
          prod:
            build_cmd: "cd apps/cde-onlyone-ui && pnpm build:prod"
            artifacts:
              - build_output: "./apps/cde-onlyone-ui/dist"
                rename_to: "onlyone.tar.gz"
                deploy_script: "echo alt"
            server: "prod-alt"
`
	cfg, err := Load(writeTestConfig(t, legacy))
	require.NoError(t, err)

	project := cfg.Projects["monorepo"]
	assert.Equal(t, "monorepo", project.Mode)
	require.Len(t, project.Components, 1)
	component := project.Components["cde-onlyone-ui"]
	assert.Equal(t, "apps/cde-onlyone-ui", component.Path)
	assert.Len(t, component.Environments["prod"].Deployments, 2)
	assert.Nil(t, project.SubProjects)
	assert.ElementsMatch(t, []string{"prod-alt", "prod-main"}, cfg.Environments["prod"].Servers)
}

func TestNormalize_V2DoesNotSilentlyRestoreRemovedEnvironmentServer(t *testing.T) {
	cfg, err := Load(writeTestConfig(t, validYAML))
	require.NoError(t, err)

	dev := cfg.Environments["dev"]
	dev.Servers = nil
	cfg.Environments["dev"] = dev

	require.NoError(t, cfg.Normalize())
	err = cfg.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), `server "dev-server" is not enabled for environment "dev"`)
}

func TestGetProjectConfig_NotFound(t *testing.T) {
	path := writeTestConfig(t, validYAML)
	cfg, err := Load(path)
	require.NoError(t, err)

	_, err = cfg.GetProjectConfig("nonexistent")
	require.Error(t, err)
	assert.Contains(t, err.Error(), `project "nonexistent" not found`)
}
