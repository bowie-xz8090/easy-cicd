package api

import (
	"errors"
	"net/http"
	"sort"
	"strings"

	"auto-deploy-platform/internal/auth"
	"auto-deploy-platform/internal/config"
	"auto-deploy-platform/internal/gitea"
	"auto-deploy-platform/internal/task"

	"github.com/gin-gonic/gin"
)

// Handler holds the dependencies for all API endpoint handlers.
type Handler struct {
	giteaClient gitea.GiteaClient
	cfg         *config.AppConfig
	cfgManager  *config.Manager
	taskManager task.TaskManager
	access      *auth.Service
	basePath    string
}

func (h *Handler) SetAccessService(service *auth.Service) { h.access = service }

// NewHandler creates a new Handler with the given dependencies.
func NewHandler(giteaClient gitea.GiteaClient, cfg *config.AppConfig, taskMgr task.TaskManager) *Handler {
	return &Handler{
		giteaClient: giteaClient,
		cfg:         cfg,
		taskManager: taskMgr,
	}
}

// NewHandlerWithManager creates a new Handler with a config Manager for dynamic config support.
func NewHandlerWithManager(giteaClient gitea.GiteaClient, cfgMgr *config.Manager, taskMgr task.TaskManager) *Handler {
	return &Handler{
		giteaClient: giteaClient,
		cfg:         cfgMgr.Get(),
		cfgManager:  cfgMgr,
		taskManager: taskMgr,
	}
}

// getLatestConfig returns the most up-to-date config, preferring the Manager if available.
func (h *Handler) getLatestConfig() *config.AppConfig {
	if h.cfgManager != nil {
		return h.cfgManager.Get()
	}
	return h.cfg
}

// RegisterRoutes registers all API routes on the given Gin engine.
func (h *Handler) RegisterRoutes(r *gin.Engine) {
	h.RegisterRoutesWithBasePath(r, "")
}

// RegisterRoutesWithBasePath registers API routes under an optional application base path.
func (h *Handler) RegisterRoutesWithBasePath(r *gin.Engine, basePath string) {
	h.basePath = basePath
	// Forwarding headers are interpreted only by the access service's explicit proxy policy.
	_ = r.SetTrustedProxies(nil)
	if h.access == nil {
		h.access = auth.NewService(nil, h.getLatestConfig, basePath)
	}
	api := r.Group(joinRoutePath(basePath, "/api"))
	{
		api.GET("/health", h.handleHealth)
		api.GET("/site-info", h.handleSiteInfo)
		h.access.RegisterRoutes(api)
		api.Use(h.access.Middleware())
		api.GET("/projects", h.handleListProjects)
		api.GET("/projects/:owner/:repo/branches", h.handleListBranches)
		api.GET("/projects/:owner/:repo/tags", h.handleListTags)
		api.GET("/environments", h.handleListEnvironments)

		// Deploy routes: register /deploy/records BEFORE /:id routes to avoid conflicts.
		api.GET("/deploy/records", h.handleDeployRecords)
		api.POST("/deploy", h.handleDeploy)
		api.POST("/deploy/:id/cancel", h.handleDeployCancel)
		api.GET("/deploy/:id/status", h.handleDeployStatus)
		api.GET("/deploy/:id/logs", h.handleDeployLogs)

		// Config management routes (require admin_token authentication)
		configGroup := api.Group("/config")
		configGroup.Use(h.adminAuthMiddleware())
		{
			configGroup.GET("/editor", h.handleGetEditableConfig)
			configGroup.PUT("/editor", h.handleUpdateEditableConfig)
			configGroup.PUT("/whitelist", h.handleUpdateWhitelist)
			configGroup.DELETE("/history", h.handleClearDeployHistory)
			configGroup.PUT("/projects", h.handleUpdateProjects)
			configGroup.PUT("/environments/:key", h.handleUpdateEnvironmentAccess)
			configGroup.PUT("", h.handleUpdateConfig)
			configGroup.POST("/reload", h.handleReloadConfig)
		}
	}
}

func joinRoutePath(basePath, routePath string) string {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" || basePath == "/" {
		return routePath
	}
	basePath = "/" + strings.Trim(basePath, "/")
	return basePath + routePath
}

// handleHealth returns a simple health check response.
func (h *Handler) handleHealth(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": "ok"})
}

// handleSiteInfo returns site configuration (title, etc.) for the frontend.
func (h *Handler) handleSiteInfo(c *gin.Context) {
	cfg := h.getLatestConfig()
	title := cfg.Server.Title
	if title == "" {
		title = "自动部署平台"
	}
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"title": title,
		},
	})
}

type ComponentItem struct {
	Key   string `json:"key"`
	Label string `json:"label"`
	Path  string `json:"path"`
}

// ProjectItem 表示一个代码仓库。single 项目自身可选，monorepo 通过 Components 选择真实应用。
type ProjectItem struct {
	Owner      string          `json:"owner"`
	Name       string          `json:"name"`
	FullName   string          `json:"full_name"`
	Label      string          `json:"label"`
	Mode       string          `json:"mode"`
	Selectable bool            `json:"selectable"`
	Components []ComponentItem `json:"components"`
	CloneURL   string          `json:"clone_url"`
}

// handleListProjects 返回树形项目结构，避免把服务器部署变体伪装成子项目。
func (h *Handler) handleListProjects(c *gin.Context) {
	repos, err := h.giteaClient.ListRepos()
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"code":    -1,
			"message": "获取项目列表失败: " + err.Error(),
		})
		return
	}

	cfg := h.getLatestConfig()
	items := make([]ProjectItem, 0, len(repos))

	for _, repo := range repos {
		projCfg, ok := cfg.Projects[repo.Name]
		if !ok {
			continue
		}

		projLabel := projCfg.Label
		if projLabel == "" {
			projLabel = repo.Name
		}

		components := make([]ComponentItem, 0, len(projCfg.Components))
		for componentKey, component := range projCfg.Components {
			label := component.Label
			if label == "" {
				label = componentKey
			}
			components = append(components, ComponentItem{Key: componentKey, Label: label, Path: component.Path})
		}
		sort.SliceStable(components, func(i, j int) bool {
			if components[i].Label != components[j].Label {
				return components[i].Label < components[j].Label
			}
			return components[i].Key < components[j].Key
		})
		items = append(items, ProjectItem{
			Owner:      repo.Owner,
			Name:       repo.Name,
			FullName:   repo.FullName,
			Label:      projLabel,
			Mode:       projCfg.Mode,
			Selectable: projCfg.Mode == "single",
			Components: components,
			CloneURL:   repo.CloneURL,
		})
	}
	sort.SliceStable(items, func(i, j int) bool {
		if items[i].Label != items[j].Label {
			return items[i].Label < items[j].Label
		}
		if items[i].Owner != items[j].Owner {
			return items[i].Owner < items[j].Owner
		}
		if items[i].Name != items[j].Name {
			return items[i].Name < items[j].Name
		}
		return items[i].FullName < items[j].FullName
	})

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": items,
	})
}

// handleListBranches fetches all branches for the specified repository from Gitea.
func (h *Handler) handleListBranches(c *gin.Context) {
	owner := c.Param("owner")
	repo := c.Param("repo")

	branches, err := h.giteaClient.ListBranches(owner, repo)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"code":    -1,
			"message": "获取分支列表失败: " + err.Error(),
		})
		return
	}
	sort.SliceStable(branches, func(i, j int) bool {
		return branches[i].Name < branches[j].Name
	})

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": branches,
	})
}

// handleListTags fetches all tags for the specified repository from Gitea.
func (h *Handler) handleListTags(c *gin.Context) {
	owner := c.Param("owner")
	repo := c.Param("repo")

	tags, err := h.giteaClient.ListTags(owner, repo)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{
			"code":    -1,
			"message": "获取标签列表失败: " + err.Error(),
		})
		return
	}
	sort.SliceStable(tags, func(i, j int) bool {
		return tags[i].Name < tags[j].Name
	})

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": tags,
	})
}

// EnvironmentItem represents a single environment entry in the API response.
type EnvironmentItem struct {
	Key          string       `json:"key"`
	Label        string       `json:"label"`
	Disabled     bool         `json:"disabled"`
	NeedPassword bool         `json:"need_password"`
	Targets      []TargetItem `json:"targets"`
	UserURL      string       `json:"user_url"`
	AdminURL     string       `json:"admin_url"`
	Extra        []LinkItem   `json:"extra"`
}

type TargetItem struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Server      string `json:"server"`
	ServerLabel string `json:"server_label"`
	ServerHost  string `json:"server_host"`
	ServerPort  int    `json:"server_port"`
}

// LinkItem represents a custom link in the API response.
type LinkItem struct {
	Label    string `json:"label"`
	URL      string `json:"url"`
	ProxyURL string `json:"proxy_url,omitempty"`
}

// toLinkItems converts config EnvLink slice to API LinkItem slice.
func toLinkItems(links []config.EnvLink) []LinkItem {
	if len(links) == 0 {
		return nil
	}
	items := make([]LinkItem, len(links))
	for i, l := range links {
		items[i] = LinkItem{Label: l.Label, URL: l.URL}
	}
	return items
}

// handleListEnvironments 返回环境及当前项目/组件的部署目标。
func (h *Handler) handleListEnvironments(c *gin.Context) {
	cfg := h.getLatestConfig()
	projectName := c.Query("project")
	componentName := c.Query("component")
	if componentName == "" {
		componentName = c.Query("sub_project")
	}

	order := []string{"dev", "sit", "prod"}
	for key := range cfg.Environments {
		if key != "dev" && key != "sit" && key != "prod" {
			order = append(order, key)
		}
	}
	envs := make([]EnvironmentItem, 0, len(order))
	var unitEnvironments map[string]config.UnitEnvConfig
	if projectName != "" {
		if resolved, _, _, err := cfg.GetUnitEnvironments(projectName, componentName); err == nil {
			unitEnvironments = resolved
		}
	}

	for _, key := range order {
		envCfg, ok := cfg.Environments[key]
		if !ok {
			continue
		}
		item := EnvironmentItem{
			Key:          key,
			Label:        envCfg.Label,
			Disabled:     envCfg.Disabled,
			NeedPassword: envCfg.Password != "",
			Targets:      []TargetItem{},
			UserURL:      envCfg.Links.UserURL,
			AdminURL:     envCfg.Links.AdminURL,
			Extra:        toLinkItems(envCfg.Links.Extra),
		}
		for index := range item.Extra {
			item.Extra[index].ProxyURL = cfg.ProxyLink(key, item.Extra[index].URL, h.basePath)
		}
		if projectName != "" {
			unitEnv, configured := unitEnvironments[key]
			if !configured {
				item.Disabled = true
			} else {
				if unitEnv.Disabled != nil {
					item.Disabled = item.Disabled || *unitEnv.Disabled
				}
				for targetKey, deployment := range unitEnv.Deployments {
					if deployment.Disabled != nil && *deployment.Disabled {
						continue
					}
					serverKey := deployment.Server
					if serverKey == "" {
						serverKey = targetKey
					}
					server := cfg.Servers[serverKey]
					label := deployment.Label
					if label == "" {
						label = server.Label
					}
					item.Targets = append(item.Targets, TargetItem{
						Key:         targetKey,
						Label:       label,
						Server:      serverKey,
						ServerLabel: server.Label,
						ServerHost:  server.Host,
						ServerPort:  server.Port,
					})
				}
				sort.SliceStable(item.Targets, func(i, j int) bool {
					if item.Targets[i].Label != item.Targets[j].Label {
						return item.Targets[i].Label < item.Targets[j].Label
					}
					return item.Targets[i].Key < item.Targets[j].Key
				})
				if len(item.Targets) == 0 {
					item.Disabled = true
				}
			}
		}
		envs = append(envs, item)
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": envs,
	})
}

// handleDeploy creates a new deployment task from the request body.
func (h *Handler) handleDeploy(c *gin.Context) {
	var req task.DeployRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	cfg := h.getLatestConfig()
	component := req.Component
	if component == "" {
		component = req.SubProject
	}
	if isDeploymentDisabled(cfg, req.ProjectName, component, req.Environment, req.Target) {
		c.JSON(http.StatusForbidden, gin.H{
			"code":    -1,
			"message": "当前项目、环境或部署目标未开放部署",
		})
		return
	}

	// Validate deploy password if the environment has one configured.
	if envCfg, ok := cfg.Environments[req.Environment]; ok {
		if envCfg.Password != "" && req.DeployPassword != envCfg.Password {
			c.JSON(http.StatusForbidden, gin.H{
				"code":    -1,
				"message": "部署密码错误",
			})
			return
		}
	}

	req.Principal = auth.Principal(c)
	deployTask, err := h.taskManager.CreateTask(req)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"task_id":    deployTask.ID,
			"status":     deployTask.Status,
			"created_at": deployTask.CreatedAt,
		},
	})
}

// isDeploymentDisabled 统一检查环境、项目/组件环境及服务器部署目标三层权限。
func isDeploymentDisabled(cfg *config.AppConfig, projectName, componentName, environment, target string) bool {
	if cfg == nil {
		return true
	}

	environmentCfg, ok := cfg.Environments[environment]
	if !ok {
		return true
	}
	disabled := environmentCfg.Disabled

	unitEnvironments, _, _, err := cfg.GetUnitEnvironments(projectName, componentName)
	if err != nil {
		return true
	}
	unitEnvironment, ok := unitEnvironments[environment]
	if !ok {
		return true
	}
	if unitEnvironment.Disabled != nil {
		disabled = disabled || *unitEnvironment.Disabled
	}
	if target == "" {
		enabledTargets := 0
		for _, deployment := range unitEnvironment.Deployments {
			if deployment.Disabled == nil || !*deployment.Disabled {
				enabledTargets++
			}
		}
		return disabled || enabledTargets != 1
	}
	deployment, ok := unitEnvironment.Deployments[target]
	if !ok {
		return true
	}
	if deployment.Disabled != nil {
		disabled = disabled || *deployment.Disabled
	}

	return disabled
}

// handleDeployStatus returns the current status of a deployment task.
func (h *Handler) handleDeployStatus(c *gin.Context) {
	id := c.Param("id")

	status, err := h.taskManager.GetTaskStatus(id)
	if err != nil {
		if strings.Contains(err.Error(), "task not found") {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    -1,
				"message": err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    -1,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": status,
	})
}

// handleDeployCancel attempts to cancel a running deployment task.
func (h *Handler) handleDeployCancel(c *gin.Context) {
	id := c.Param("id")

	if err := h.taskManager.CancelTask(id); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "已发送取消信号，任务将在当前阶段完成后终止",
	})
}

// handleDeployLogs returns the execution logs of a deployment task.
func (h *Handler) handleDeployLogs(c *gin.Context) {
	id := c.Param("id")

	logs, err := h.taskManager.GetTaskLogs(id)
	if err != nil {
		if strings.Contains(err.Error(), "task not found") {
			c.JSON(http.StatusNotFound, gin.H{
				"code":    -1,
				"message": err.Error(),
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    -1,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"task_id": id,
			"logs":    logs,
		},
	})
}

// handleDeployRecords returns a paginated list of deployment records with optional filtering.
func (h *Handler) handleDeployRecords(c *gin.Context) {
	var filter task.RecordFilter
	if err := c.ShouldBindQuery(&filter); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	records, total, err := h.taskManager.ListRecords(filter)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    -1,
			"message": err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{
			"total":   total,
			"records": records,
		},
	})
}

// handleClearDeployHistory removes deployment records and their stored logs,
// then compacts SQLite to reclaim disk space.
func (h *Handler) handleClearDeployHistory(c *gin.Context) {
	deleted, err := h.taskManager.ClearDeployHistory()
	if err != nil {
		if errors.Is(err, task.ErrDeploymentsInProgress) {
			c.JSON(http.StatusConflict, gin.H{
				"code":    -1,
				"message": "存在进行中的部署任务，暂时无法清空部署历史",
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    -1,
			"message": "清空部署历史失败: " + err.Error(),
		})
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": gin.H{"deleted": deleted},
	})
}

// EditableConfig contains the configuration sections that can be safely edited
// from the web UI. Connection credentials are intentionally excluded.
type EditableConfig struct {
	Whitelist    []string                        `json:"whitelist"`
	Environments map[string]config.EnvConfig     `json:"environments"`
	Projects     map[string]config.ProjectConfig `json:"projects"`
	Servers      []ServerSummary                 `json:"servers"`
}

type ServerSummary struct {
	Key        string `json:"key"`
	Label      string `json:"label"`
	Host       string `json:"host"`
	Port       int    `json:"port"`
	DeployPath string `json:"deploy_path"`
}

func editableConfig(cfg *config.AppConfig) EditableConfig {
	servers := make([]ServerSummary, 0, len(cfg.Servers))
	for key, server := range cfg.Servers {
		servers = append(servers, ServerSummary{
			Key:        key,
			Label:      server.Label,
			Host:       server.Host,
			Port:       server.Port,
			DeployPath: server.DeployPath,
		})
	}
	sort.SliceStable(servers, func(i, j int) bool { return servers[i].Key < servers[j].Key })
	return EditableConfig{
		Whitelist:    append([]string{}, cfg.Access.Whitelist...),
		Environments: cfg.Environments,
		Projects:     cfg.Projects,
		Servers:      servers,
	}
}

// EnvironmentAccessConfig contains the environment fields managed by the UI.
// The label and deploy password remain unchanged when access settings are saved.
type EnvironmentAccessConfig struct {
	Disabled bool            `json:"disabled"`
	Servers  []string        `json:"servers"`
	Links    config.EnvLinks `json:"links"`
}

// handleGetEditableConfig returns only the editable configuration sections.
func (h *Handler) handleGetEditableConfig(c *gin.Context) {
	cfg := h.getLatestConfig()
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": editableConfig(cfg)})
}

// Whitelist editing requires both platform admission and the existing admin
// credential. It never accepts OIDC secrets or proxy-trust configuration.
func (h *Handler) handleUpdateWhitelist(c *gin.Context) {
	if h.cfgManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "配置管理器未初始化，不支持动态修改"})
		return
	}
	var payload struct {
		Whitelist *[]string `json:"whitelist"`
	}
	if err := c.ShouldBindJSON(&payload); err != nil || payload.Whitelist == nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "请提供白名单列表，每项为 IP 或 11 位手机号"})
		return
	}
	if err := h.cfgManager.UpdateWhitelist(*payload.Whitelist); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"code": -1, "message": "白名单保存失败：" + err.Error()})
		return
	}
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": gin.H{"whitelist": h.cfgManager.Get().Access.Whitelist}})
}

// handleUpdateEditableConfig merges edited environment and project settings
// into the current configuration, then persists and hot-reloads it.
func (h *Handler) handleUpdateEditableConfig(c *gin.Context) {
	if h.cfgManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    -1,
			"message": "配置管理器未初始化，不支持动态修改",
		})
		return
	}

	var editable EditableConfig
	if err := c.ShouldBindJSON(&editable); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}
	if len(editable.Environments) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "至少需要保留一个环境配置",
		})
		return
	}

	updated := *h.cfgManager.Get()
	for key, environment := range editable.Environments {
		if existing, ok := updated.Environments[key]; ok {
			environment.Password = existing.Password
		}
		editable.Environments[key] = environment
	}
	updated.Environments = editable.Environments
	updated.Projects = editable.Projects
	if err := h.cfgManager.Update(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "配置更新失败: " + err.Error(),
		})
		return
	}

	h.cfg = h.cfgManager.Get()
	c.JSON(http.StatusOK, gin.H{"code": 0, "data": editableConfig(h.cfg)})
}

// handleUpdateProjects replaces only project build configuration, leaving
// environment access settings and all credentials untouched.
func (h *Handler) handleUpdateProjects(c *gin.Context) {
	if h.cfgManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    -1,
			"message": "配置管理器未初始化，不支持动态修改",
		})
		return
	}

	var projects map[string]config.ProjectConfig
	if err := c.ShouldBindJSON(&projects); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	updated := *h.cfgManager.Get()
	updated.Projects = projects
	if err := h.cfgManager.Update(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "配置更新失败: " + err.Error(),
		})
		return
	}

	h.cfg = h.cfgManager.Get()
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": h.cfg.Projects,
	})
}

// handleUpdateEnvironmentAccess updates only one standard environment's access
// settings so editing it cannot overwrite other environment or project config.
func (h *Handler) handleUpdateEnvironmentAccess(c *gin.Context) {
	if h.cfgManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    -1,
			"message": "配置管理器未初始化，不支持动态修改",
		})
		return
	}

	key := c.Param("key")
	if key != "dev" && key != "sit" && key != "prod" {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "仅支持修改开发、测试和生产环境访问配置",
		})
		return
	}

	var access EnvironmentAccessConfig
	if err := c.ShouldBindJSON(&access); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	updated := *h.cfgManager.Get()
	environment, ok := updated.Environments[key]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "环境配置不存在: " + key,
		})
		return
	}
	environment.Disabled = access.Disabled
	environment.Servers = access.Servers
	environment.Links = access.Links
	updated.Environments[key] = environment

	if err := h.cfgManager.Update(&updated); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "配置更新失败: " + err.Error(),
		})
		return
	}

	h.cfg = h.cfgManager.Get()
	c.JSON(http.StatusOK, gin.H{
		"code": 0,
		"data": EnvironmentAccessConfig{
			Disabled: h.cfg.Environments[key].Disabled,
			Servers:  h.cfg.Environments[key].Servers,
			Links:    h.cfg.Environments[key].Links,
		},
	})
}

// adminAuthMiddleware returns a Gin middleware that validates the admin token.
// The token can be provided via:
//   - Header: Authorization: Bearer <token>
//   - Query parameter: ?token=<token>
func (h *Handler) adminAuthMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		// Get the admin token from config
		var adminToken string
		if h.cfgManager != nil {
			adminToken = h.cfgManager.Get().AdminToken
		} else if h.cfg != nil {
			adminToken = h.cfg.AdminToken
		}

		// If no admin_token is configured, reject all requests
		if adminToken == "" {
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{
				"code":    -1,
				"message": "配置管理接口未启用：请在 config.yaml 中设置 admin_token",
			})
			return
		}

		// Extract token from request
		token := ""
		authHeader := c.GetHeader("Authorization")
		if strings.HasPrefix(authHeader, "Bearer ") {
			token = strings.TrimPrefix(authHeader, "Bearer ")
		}
		if token == "" {
			token = c.Query("token")
		}

		// Validate
		if token == "" || token != adminToken {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"code":    -1,
				"message": "未授权：admin_token 无效",
			})
			return
		}

		c.Next()
	}
}

// handleUpdateConfig updates the configuration with the provided JSON body and persists to disk.
func (h *Handler) handleUpdateConfig(c *gin.Context) {
	if h.cfgManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    -1,
			"message": "配置管理器未初始化，不支持动态修改",
		})
		return
	}

	var newCfg config.AppConfig
	if err := c.ShouldBindJSON(&newCfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "请求参数错误: " + err.Error(),
		})
		return
	}

	// Secret fields are deliberately excluded from JSON. Preserve their current
	// values so a full configuration update cannot silently erase credentials.
	current := h.cfgManager.Get()
	newCfg.AdminToken = current.AdminToken
	newCfg.Gitea.Token = current.Gitea.Token
	newCfg.OIDC = current.OIDC
	newCfg.Access = current.Access
	newCfg.WebProxy = current.WebProxy
	for key, server := range newCfg.Servers {
		if existing, ok := current.Servers[key]; ok {
			server.Password = existing.Password
			newCfg.Servers[key] = server
		}
	}
	for key, environment := range newCfg.Environments {
		if existing, ok := current.Environments[key]; ok {
			environment.Password = existing.Password
			newCfg.Environments[key] = environment
		}
	}

	if err := h.cfgManager.Update(&newCfg); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{
			"code":    -1,
			"message": "配置更新失败: " + err.Error(),
		})
		return
	}

	// Update the local reference
	h.cfg = h.cfgManager.Get()

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "配置已更新并保存",
		"data":    editableConfig(h.cfgManager.Get()),
	})
}

// handleReloadConfig reloads the configuration from disk without restarting the service.
func (h *Handler) handleReloadConfig(c *gin.Context) {
	if h.cfgManager == nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    -1,
			"message": "配置管理器未初始化，不支持动态重载",
		})
		return
	}

	cfg, err := h.cfgManager.Reload()
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{
			"code":    -1,
			"message": "配置重载失败: " + err.Error(),
		})
		return
	}

	// Update the local reference
	h.cfg = cfg

	c.JSON(http.StatusOK, gin.H{
		"code":    0,
		"message": "配置已从磁盘重新加载",
		"data":    editableConfig(cfg),
	})
}
