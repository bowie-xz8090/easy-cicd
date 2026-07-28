package config

import (
	"fmt"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strings"
	"sync"

	"gopkg.in/yaml.v3"
)

const CurrentVersion = 2

// AppConfig 是应用的顶层配置。Version 2 将真实组件、环境构建规则和服务器部署规则分离。
type AppConfig struct {
	ConfigVersion int                      `yaml:"config_version" json:"config_version"`
	Server        ServerAppConfig          `yaml:"server" json:"server"`
	Gitea         GiteaConfig              `yaml:"gitea" json:"gitea"`
	Servers       map[string]ServerConfig  `yaml:"servers" json:"servers"`
	Environments  map[string]EnvConfig     `yaml:"environments" json:"environments"`
	Projects      map[string]ProjectConfig `yaml:"projects" json:"projects"`
	AdminToken    string                   `yaml:"admin_token" json:"-"`
}

type ServerAppConfig struct {
	Port      int    `yaml:"port" json:"port"`
	Workspace string `yaml:"workspace" json:"workspace"`
	Title     string `yaml:"title" json:"title"`
	BasePath  string `yaml:"base_path" json:"base_path"`
}

type GiteaConfig struct {
	URL      string `yaml:"url" json:"url"`
	Username string `yaml:"username" json:"username"`
	Token    string `yaml:"token" json:"token"`
}

// ServerConfig 只保存连接资源。项目专属的部署脚本位于 DeploymentConfig。
type ServerConfig struct {
	Label      string `yaml:"label,omitempty" json:"label"`
	Host       string `yaml:"host" json:"host"`
	Port       int    `yaml:"port" json:"port"`
	User       string `yaml:"user" json:"user"`
	Password   string `yaml:"password,omitempty" json:"-"`
	DeployPath string `yaml:"deploy_path" json:"deploy_path"`
}

// EnvConfig 维护环境展示、权限及该环境允许使用的服务器。
type EnvConfig struct {
	Label    string   `yaml:"label" json:"label"`
	Disabled bool     `yaml:"disabled" json:"disabled"`
	Servers  []string `yaml:"servers,omitempty" json:"servers"`
	Links    EnvLinks `yaml:"links" json:"links"`
	Password string   `yaml:"password,omitempty" json:"-"`
}

type EnvLinks struct {
	UserURL  string    `yaml:"user_url" json:"user_url"`
	AdminURL string    `yaml:"admin_url" json:"admin_url"`
	Extra    []EnvLink `yaml:"extra" json:"extra"`
}

type EnvLink struct {
	Label string `yaml:"label" json:"label"`
	URL   string `yaml:"url" json:"url"`
}

// ProjectConfig 的 single 模式直接使用 Environments；monorepo 模式通过 Components 配置真实组件。
type ProjectConfig struct {
	Label        string                      `yaml:"label" json:"label"`
	Mode         string                      `yaml:"mode" json:"mode"`
	BuildType    string                      `yaml:"build_type,omitempty" json:"build_type"`
	Environments map[string]UnitEnvConfig    `yaml:"environments,omitempty" json:"environments"`
	Components   map[string]ComponentConfig  `yaml:"components,omitempty" json:"components"`
	SubProjects  map[string]SubProjectConfig `yaml:"sub_projects,omitempty" json:"-"`
}

type ComponentConfig struct {
	Label        string                   `yaml:"label" json:"label"`
	Path         string                   `yaml:"path" json:"path"`
	BuildType    string                   `yaml:"build_type,omitempty" json:"build_type"`
	Environments map[string]UnitEnvConfig `yaml:"environments" json:"environments"`
}

// UnitEnvConfig 将公共构建配置与各服务器部署配置分开。
type UnitEnvConfig struct {
	Disabled    *bool                       `yaml:"disabled,omitempty" json:"disabled"`
	Build       BuildConfig                 `yaml:"build" json:"build"`
	Deployments map[string]DeploymentConfig `yaml:"deployments" json:"deployments"`
}

type BuildConfig struct {
	BuildCmd  string           `yaml:"command" json:"command"`
	Artifacts []ArtifactConfig `yaml:"artifacts" json:"artifacts"`
}

// ArtifactConfig 只描述构建产物；部署脚本在所有产物上传后统一执行。
type ArtifactConfig struct {
	Key          string `yaml:"key,omitempty" json:"key"`
	BuildOutput  string `yaml:"output" json:"output"`
	RenameTo     string `yaml:"upload_as,omitempty" json:"upload_as"`
	DeployScript string `yaml:"deploy_script,omitempty" json:"-"`
}

// DeploymentConfig 表示当前项目/组件在某一环境下的一个部署目标。
// Server 为空时使用 deployments 映射的 key 作为服务器 key。
type DeploymentConfig struct {
	Label         string       `yaml:"label,omitempty" json:"label"`
	Server        string       `yaml:"server,omitempty" json:"server"`
	DeployScript  string       `yaml:"script" json:"script"`
	Disabled      *bool        `yaml:"disabled,omitempty" json:"disabled"`
	BuildOverride *BuildConfig `yaml:"build_override,omitempty" json:"build_override,omitempty"`
}

// 以下结构仅用于读取旧版配置，归一化后不会再写回文件。
type SubProjectConfig struct {
	Label        string                           `yaml:"label" json:"label"`
	BuildType    string                           `yaml:"build_type" json:"build_type"`
	EnvOverrides map[string]SubProjectEnvOverride `yaml:"env_overrides" json:"env_overrides"`
}

type SubProjectEnvOverride struct {
	BuildCmd     string                 `yaml:"build_cmd" json:"build_cmd"`
	BuildOutput  string                 `yaml:"build_output" json:"build_output"`
	DeployScript string                 `yaml:"deploy_script" json:"deploy_script"`
	RenameTo     string                 `yaml:"rename_to" json:"rename_to"`
	Artifacts    []LegacyArtifactConfig `yaml:"artifacts" json:"artifacts"`
	Server       string                 `yaml:"server" json:"server"`
	Disabled     *bool                  `yaml:"disabled" json:"disabled"`
}

type LegacyArtifactConfig struct {
	BuildOutput  string `yaml:"build_output" json:"build_output"`
	DeployScript string `yaml:"deploy_script" json:"deploy_script"`
	RenameTo     string `yaml:"rename_to" json:"rename_to"`
}

type ResolvedDeployment struct {
	ProjectKey     string
	ProjectLabel   string
	ComponentKey   string
	ComponentLabel string
	Environment    string
	TargetKey      string
	TargetLabel    string
	ServerKey      string
	BuildType      string
	Build          BuildConfig
	Deployment     DeploymentConfig
	Server         ServerConfig
}

type Manager struct {
	mu       sync.RWMutex
	cfg      *AppConfig
	filePath string
}

func NewManager(path string) (*Manager, error) {
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}
	return &Manager{cfg: cfg, filePath: path}, nil
}

// Get 返回深拷贝，避免配置管理请求修改仍在被部署任务读取的嵌套 map。
func (m *Manager) Get() *AppConfig {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return cloneConfig(m.cfg)
}

func (m *Manager) GetFilePath() string {
	return m.filePath
}

func (m *Manager) Reload() (*AppConfig, error) {
	cfg, err := Load(m.filePath)
	if err != nil {
		return nil, err
	}
	m.mu.Lock()
	m.cfg = cfg
	m.mu.Unlock()
	return cloneConfig(cfg), nil
}

func (m *Manager) Update(newCfg *AppConfig) error {
	if newCfg == nil {
		return fmt.Errorf("config validation error: config must not be nil")
	}
	normalized := cloneConfig(newCfg)
	if err := normalized.Normalize(); err != nil {
		return err
	}
	if err := normalized.Validate(); err != nil {
		return err
	}

	data, err := yaml.Marshal(normalized)
	if err != nil {
		return fmt.Errorf("failed to marshal config: %w", err)
	}
	if err := os.WriteFile(m.filePath, data, 0600); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	m.mu.Lock()
	m.cfg = normalized
	m.mu.Unlock()
	return nil
}

func Load(path string) (*AppConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("config file not found: %s", path)
		}
		return nil, fmt.Errorf("failed to read config file %s: %w", path, err)
	}

	var cfg AppConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file %s: invalid YAML format: %w", path, err)
	}
	if err := cfg.Normalize(); err != nil {
		return nil, fmt.Errorf("failed to normalize config file %s: %w", path, err)
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

func (c *AppConfig) Validate() error {
	if c.Gitea.URL == "" {
		return fmt.Errorf("config validation error: gitea.url must not be empty")
	}
	if c.Gitea.Token == "" {
		return fmt.Errorf("config validation error: gitea.token must not be empty")
	}
	if len(c.Environments) == 0 {
		return fmt.Errorf("config validation error: at least one environment must be defined")
	}
	if c.Server.Port == 0 {
		c.Server.Port = 8080
	}
	if c.Server.Workspace == "" {
		c.Server.Workspace = "./workspace"
	}

	for projectKey, project := range c.Projects {
		if project.Mode != "single" && project.Mode != "monorepo" {
			return fmt.Errorf("config validation error: project %q mode must be single or monorepo", projectKey)
		}
		if project.Mode == "single" {
			if len(project.Environments) == 0 {
				return fmt.Errorf("config validation error: single project %q must define environments", projectKey)
			}
			if err := c.validateUnitEnvironments(projectKey, project.Environments); err != nil {
				return err
			}
			continue
		}
		if len(project.Components) == 0 {
			return fmt.Errorf("config validation error: monorepo project %q must define components", projectKey)
		}
		for componentKey, component := range project.Components {
			if strings.TrimSpace(component.Path) == "" {
				return fmt.Errorf("config validation error: component %q in project %q must define path", componentKey, projectKey)
			}
			if err := c.validateUnitEnvironments(projectKey+"/"+componentKey, component.Environments); err != nil {
				return err
			}
		}
	}
	return nil
}

func (c *AppConfig) validateUnitEnvironments(unit string, environments map[string]UnitEnvConfig) error {
	for envKey, env := range environments {
		if _, ok := c.Environments[envKey]; !ok {
			return fmt.Errorf("config validation error: environment %q used by %q is not defined", envKey, unit)
		}
		if strings.TrimSpace(env.Build.BuildCmd) == "" {
			return fmt.Errorf("config validation error: build command is empty for %q environment %q", unit, envKey)
		}
		if len(env.Build.Artifacts) == 0 {
			return fmt.Errorf("config validation error: no artifacts configured for %q environment %q", unit, envKey)
		}
		for index, artifact := range env.Build.Artifacts {
			if strings.TrimSpace(artifact.BuildOutput) == "" {
				return fmt.Errorf("config validation error: artifact %d output is empty for %q environment %q", index+1, unit, envKey)
			}
		}
		if len(env.Deployments) == 0 {
			return fmt.Errorf("config validation error: no deployments configured for %q environment %q", unit, envKey)
		}
		for targetKey, deployment := range env.Deployments {
			serverKey := deployment.Server
			if serverKey == "" {
				serverKey = targetKey
			}
			if _, ok := c.Servers[serverKey]; !ok {
				return fmt.Errorf("config validation error: server %q used by %q environment %q is not defined", serverKey, unit, envKey)
			}
			if !contains(c.Environments[envKey].Servers, serverKey) {
				return fmt.Errorf("config validation error: server %q is not enabled for environment %q", serverKey, envKey)
			}
			if strings.TrimSpace(deployment.DeployScript) == "" {
				return fmt.Errorf("config validation error: deploy script is empty for target %q of %q environment %q", targetKey, unit, envKey)
			}
		}
	}
	return nil
}

// Normalize 将旧 sub_projects 配置迁移到 v2 内存模型，并补齐环境服务器关联。
func (c *AppConfig) Normalize() error {
	needsLegacyEnvironmentServers := c.ConfigVersion < CurrentVersion
	if c.Servers == nil {
		c.Servers = map[string]ServerConfig{}
	}
	if c.Projects == nil {
		c.Projects = map[string]ProjectConfig{}
	}
	for key, server := range c.Servers {
		if server.Label == "" {
			server.Label = key
			c.Servers[key] = server
		}
	}

	for projectKey, project := range c.Projects {
		if len(project.SubProjects) > 0 && len(project.Environments) == 0 && len(project.Components) == 0 {
			migrated, err := migrateLegacyProject(projectKey, project)
			if err != nil {
				return err
			}
			project = migrated
			needsLegacyEnvironmentServers = true
		}
		normalizeProject(&project)
		c.Projects[projectKey] = project
	}
	if needsLegacyEnvironmentServers {
		c.populateEnvironmentServers()
	}
	c.ConfigVersion = CurrentVersion
	return nil
}

func migrateLegacyProject(projectKey string, legacy ProjectConfig) (ProjectConfig, error) {
	type group struct {
		key       string
		label     string
		path      string
		buildType string
		envs      map[string]UnitEnvConfig
	}
	groups := map[string]*group{}
	hasRealComponents := false

	subKeys := make([]string, 0, len(legacy.SubProjects))
	for subKey := range legacy.SubProjects {
		subKeys = append(subKeys, subKey)
	}
	sort.Strings(subKeys)
	for _, subKey := range subKeys {
		sub := legacy.SubProjects[subKey]
		componentKey := inferComponentKey(subKey, sub)
		if componentKey != "" {
			hasRealComponents = true
		}
		groupKey := componentKey
		if groupKey == "" {
			groupKey = "__project__"
		}
		current := groups[groupKey]
		if current == nil {
			label := sub.Label
			if componentKey != "" && (strings.HasPrefix(subKey, "mult-") || label == "") {
				label = componentKey
			}
			current = &group{
				key:       componentKey,
				label:     label,
				path:      componentPath(componentKey),
				buildType: sub.BuildType,
				envs:      map[string]UnitEnvConfig{},
			}
			groups[groupKey] = current
		}
		if componentKey != "" && subKey == componentKey && sub.Label != "" {
			current.label = sub.Label
		}
		for envKey, override := range sub.EnvOverrides {
			if err := mergeLegacyEnvironment(projectKey, subKey, sub.Label, envKey, override, current.envs); err != nil {
				return ProjectConfig{}, err
			}
		}
	}

	result := ProjectConfig{Label: legacy.Label}
	if !hasRealComponents {
		result.Mode = "single"
		result.BuildType = inferBuildTypeFromEnvironments(groups["__project__"].envs)
		result.Environments = groups["__project__"].envs
		return result, nil
	}

	result.Mode = "monorepo"
	result.BuildType = "frontend"
	result.Components = map[string]ComponentConfig{}
	for key, item := range groups {
		if key == "__project__" {
			return ProjectConfig{}, fmt.Errorf("legacy project %q mixes repository-level and apps/* deployment units", projectKey)
		}
		buildType := item.buildType
		if buildType == "" {
			buildType = inferBuildTypeFromEnvironments(item.envs)
		}
		result.Components[key] = ComponentConfig{
			Label:        item.label,
			Path:         item.path,
			BuildType:    buildType,
			Environments: item.envs,
		}
	}
	return result, nil
}

func mergeLegacyEnvironment(projectKey, subKey, subLabel, envKey string, legacy SubProjectEnvOverride, environments map[string]UnitEnvConfig) error {
	build := BuildConfig{BuildCmd: legacy.BuildCmd}
	if len(legacy.Artifacts) > 0 {
		build.Artifacts = make([]ArtifactConfig, 0, len(legacy.Artifacts))
		for index, artifact := range legacy.Artifacts {
			build.Artifacts = append(build.Artifacts, ArtifactConfig{
				Key:         artifactKey(artifact.RenameTo, index),
				BuildOutput: artifact.BuildOutput,
				RenameTo:    artifact.RenameTo,
			})
		}
	} else if legacy.BuildOutput != "" {
		build.Artifacts = []ArtifactConfig{{
			Key:         "artifact",
			BuildOutput: legacy.BuildOutput,
			RenameTo:    legacy.RenameTo,
		}}
	}

	existing, exists := environments[envKey]
	if exists && !reflect.DeepEqual(existing.Build, build) {
		return fmt.Errorf("legacy config conflict: %s/%s environment %s has a different build rule", projectKey, subKey, envKey)
	}
	if !exists {
		existing = UnitEnvConfig{
			Disabled:    legacy.Disabled,
			Build:       build,
			Deployments: map[string]DeploymentConfig{},
		}
	}

	targetKey := legacy.Server
	if targetKey == "" {
		return fmt.Errorf("legacy config error: %s/%s environment %s has no server", projectKey, subKey, envKey)
	}
	script := strings.TrimSpace(legacy.DeployScript)
	for _, artifact := range legacy.Artifacts {
		if strings.TrimSpace(artifact.DeployScript) == "" {
			continue
		}
		if script != "" {
			script += "\n"
		}
		script += strings.TrimSpace(artifact.DeployScript)
	}
	deployment := DeploymentConfig{
		Label:        subLabel,
		Server:       targetKey,
		DeployScript: script,
		Disabled:     legacy.Disabled,
	}
	if previous, ok := existing.Deployments[targetKey]; ok {
		if previous.DeployScript != deployment.DeployScript {
			return fmt.Errorf("legacy config conflict: %s environment %s has multiple scripts for server %s", projectKey, envKey, targetKey)
		}
	} else {
		existing.Deployments[targetKey] = deployment
	}
	environments[envKey] = existing
	return nil
}

func normalizeProject(project *ProjectConfig) {
	project.SubProjects = nil
	if project.Mode == "" {
		if len(project.Components) > 0 {
			project.Mode = "monorepo"
		} else {
			project.Mode = "single"
		}
	}
	if project.BuildType == "" {
		if project.Mode == "monorepo" {
			project.BuildType = "frontend"
		} else {
			project.BuildType = inferBuildTypeFromEnvironments(project.Environments)
		}
	}
	for componentKey, component := range project.Components {
		if component.Path == "" {
			component.Path = componentPath(componentKey)
		}
		if component.BuildType == "" {
			component.BuildType = project.BuildType
		}
		project.Components[componentKey] = component
	}
}

func (c *AppConfig) populateEnvironmentServers() {
	byEnvironment := map[string]map[string]bool{}
	add := func(envKey string, env UnitEnvConfig) {
		if byEnvironment[envKey] == nil {
			byEnvironment[envKey] = map[string]bool{}
		}
		for targetKey, deployment := range env.Deployments {
			serverKey := deployment.Server
			if serverKey == "" {
				serverKey = targetKey
			}
			byEnvironment[envKey][serverKey] = true
		}
	}
	for _, project := range c.Projects {
		for envKey, env := range project.Environments {
			add(envKey, env)
		}
		for _, component := range project.Components {
			for envKey, env := range component.Environments {
				add(envKey, env)
			}
		}
	}
	for envKey, env := range c.Environments {
		servers := append([]string(nil), env.Servers...)
		for serverKey := range byEnvironment[envKey] {
			if !contains(servers, serverKey) {
				servers = append(servers, serverKey)
			}
		}
		sort.Strings(servers)
		env.Servers = servers
		c.Environments[envKey] = env
	}
}

var appsPathPattern = regexp.MustCompile(`(?:^|[\s./])apps/([A-Za-z0-9_.-]+)(?:[/\s]|$)`)

func inferComponentKey(subKey string, sub SubProjectConfig) string {
	var candidates []string
	for _, env := range sub.EnvOverrides {
		candidates = append(candidates, env.BuildCmd, env.BuildOutput)
		for _, artifact := range env.Artifacts {
			candidates = append(candidates, artifact.BuildOutput)
		}
	}
	for _, candidate := range candidates {
		match := appsPathPattern.FindStringSubmatch(candidate)
		if len(match) == 2 {
			return match[1]
		}
	}
	if strings.HasPrefix(subKey, "cde-") && strings.HasSuffix(subKey, "-ui") {
		return subKey
	}
	return ""
}

func componentPath(componentKey string) string {
	if componentKey == "" {
		return ""
	}
	return "apps/" + componentKey
}

func artifactKey(renameTo string, index int) string {
	base := strings.TrimSuffix(renameTo, ".tar.gz")
	base = strings.TrimSuffix(base, ".tgz")
	base = strings.TrimSuffix(base, ".zip")
	base = strings.TrimSuffix(base, ".jar")
	base = regexp.MustCompile(`[^A-Za-z0-9_-]+`).ReplaceAllString(base, "-")
	base = strings.Trim(base, "-")
	if base == "" {
		return fmt.Sprintf("artifact-%d", index+1)
	}
	return base
}

func inferBuildTypeFromEnvironments(environments map[string]UnitEnvConfig) string {
	for _, env := range environments {
		command := strings.ToLower(env.Build.BuildCmd)
		if strings.Contains(command, "pnpm") || strings.Contains(command, "npm ") || strings.Contains(command, "yarn") {
			return "frontend"
		}
	}
	return "backend"
}

func contains(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func cloneConfig(cfg *AppConfig) *AppConfig {
	if cfg == nil {
		return nil
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		copy := *cfg
		return &copy
	}
	var cloned AppConfig
	if err := yaml.Unmarshal(data, &cloned); err != nil {
		copy := *cfg
		return &copy
	}
	return &cloned
}

func (c *AppConfig) GetGiteaConfig() GiteaConfig {
	return c.Gitea
}

func (c *AppConfig) ResolveServer(serverKey string) (ServerConfig, error) {
	if serverKey == "" {
		return ServerConfig{}, fmt.Errorf("server key is empty")
	}
	server, ok := c.Servers[serverKey]
	if !ok {
		return ServerConfig{}, fmt.Errorf("server %q not found in servers config", serverKey)
	}
	if server.Host == "" {
		return ServerConfig{}, fmt.Errorf("server %q has no host configured", serverKey)
	}
	return server, nil
}

func (c *AppConfig) GetProjectConfig(project string) (ProjectConfig, error) {
	projectConfig, ok := c.Projects[project]
	if !ok {
		return ProjectConfig{}, fmt.Errorf("project %q not found in config", project)
	}
	return projectConfig, nil
}

func (c *AppConfig) GetUnitEnvironments(projectKey, componentKey string) (map[string]UnitEnvConfig, string, string, error) {
	project, ok := c.Projects[projectKey]
	if !ok {
		return nil, "", "", fmt.Errorf("project %q not found in config", projectKey)
	}
	if project.Mode == "single" {
		if componentKey != "" {
			return nil, "", "", fmt.Errorf("single project %q does not accept component %q", projectKey, componentKey)
		}
		return project.Environments, project.BuildType, project.Label, nil
	}
	if componentKey == "" {
		return nil, "", "", fmt.Errorf("component must not be empty for monorepo project %q", projectKey)
	}
	component, ok := project.Components[componentKey]
	if !ok {
		return nil, "", "", fmt.Errorf("component %q not found in project %q", componentKey, projectKey)
	}
	buildType := component.BuildType
	if buildType == "" {
		buildType = project.BuildType
	}
	return component.Environments, buildType, component.Label, nil
}

func (c *AppConfig) ResolveDeployment(projectKey, componentKey, environment, targetKey string) (ResolvedDeployment, error) {
	environments, buildType, componentLabel, err := c.GetUnitEnvironments(projectKey, componentKey)
	if err != nil {
		return ResolvedDeployment{}, err
	}
	env, ok := environments[environment]
	if !ok {
		return ResolvedDeployment{}, fmt.Errorf("environment %q not configured for project %q", environment, projectKey)
	}
	deployment, ok := env.Deployments[targetKey]
	if !ok {
		return ResolvedDeployment{}, fmt.Errorf("target %q not configured for project %q environment %q", targetKey, projectKey, environment)
	}
	serverKey := deployment.Server
	if serverKey == "" {
		serverKey = targetKey
	}
	server, err := c.ResolveServer(serverKey)
	if err != nil {
		return ResolvedDeployment{}, err
	}
	build := env.Build
	if deployment.BuildOverride != nil {
		build = *deployment.BuildOverride
	}
	project := c.Projects[projectKey]
	targetLabel := deployment.Label
	if targetLabel == "" {
		targetLabel = server.Label
	}
	return ResolvedDeployment{
		ProjectKey:     projectKey,
		ProjectLabel:   project.Label,
		ComponentKey:   componentKey,
		ComponentLabel: componentLabel,
		Environment:    environment,
		TargetKey:      targetKey,
		TargetLabel:    targetLabel,
		ServerKey:      serverKey,
		BuildType:      buildType,
		Build:          build,
		Deployment:     deployment,
		Server:         server,
	}, nil
}
