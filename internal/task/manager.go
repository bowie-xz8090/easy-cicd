package task

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"auto-deploy-platform/internal/builder"
	"auto-deploy-platform/internal/config"
	"auto-deploy-platform/internal/db"
	"auto-deploy-platform/internal/deployer"
	"auto-deploy-platform/internal/identity"

	"github.com/google/uuid"
)

const (
	taskTimeout   = 30 * time.Minute
	deployTimeout = 10 * time.Minute
	frontendBuild = "frontend"
	backendBuild  = "backend"
)

var ErrDeploymentsInProgress = errors.New("deployments are still in progress")

// DeployRequest represents a request to create a new deployment task.
type DeployRequest struct {
	Principal      identity.Principal `json:"-"`
	ProjectOwner   string             `json:"project_owner"`
	ProjectName    string             `json:"project_name"`
	Branch         string             `json:"branch"`
	RefType        string             `json:"ref_type"`
	CommitID       string             `json:"commit_id"`
	CommitMessage  string             `json:"commit_message"`
	CommitTime     *time.Time         `json:"commit_time"`
	Environment    string             `json:"environment"`
	Component      string             `json:"component"`
	SubProject     string             `json:"sub_project,omitempty"`
	Target         string             `json:"target"`
	DeployPassword string             `json:"deploy_password"`
}

// TaskStatus represents the current status of a deployment task.
type TaskStatus struct {
	TaskID      string     `json:"task_id"`
	Status      string     `json:"status"`
	ProjectName string     `json:"project_name"`
	Component   string     `json:"component"`
	SubProject  string     `json:"sub_project,omitempty"`
	Target      string     `json:"target"`
	Server      string     `json:"server"`
	Branch      string     `json:"branch"`
	Environment string     `json:"environment"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	FinishedAt  *time.Time `json:"finished_at"`
}

// DeployRecord represents a deployment record for list results.
type DeployRecord struct {
	Operator        string     `json:"operator"`
	OperatorType    string     `json:"operator_type"`
	ID              string     `json:"id"`
	ProjectOwner    string     `json:"project_owner"`
	ProjectName     string     `json:"project_name"`
	ProjectLabel    string     `json:"project_label"`
	Component       string     `json:"component"`
	ComponentLabel  string     `json:"component_label"`
	SubProject      string     `json:"sub_project"`
	SubProjectLabel string     `json:"sub_project_label"`
	Target          string     `json:"target"`
	TargetLabel     string     `json:"target_label"`
	Server          string     `json:"server"`
	Branch          string     `json:"branch"`
	CommitMessage   string     `json:"commit_message"`
	CommitTime      *time.Time `json:"commit_time,omitempty"`
	Environment     string     `json:"environment"`
	Status          string     `json:"status"`
	CreatedAt       time.Time  `json:"created_at"`
	FinishedAt      *time.Time `json:"finished_at"`
}

// RecordFilter holds filtering and pagination parameters for listing deploy records.
type RecordFilter struct {
	Project           string `form:"project"`
	ProjectKeyword    string `form:"project_keyword"`
	SubProjectKeyword string `form:"sub_project_keyword"`
	Environment       string `form:"environment"`
	Status            string `form:"status"`
	Page              int    `form:"page,default=1"`
	PageSize          int    `form:"page_size,default=20"`
}

// TaskManager defines the interface for managing deployment tasks.
type TaskManager interface {
	CreateTask(req DeployRequest) (*db.DeployTask, error)
	GetTaskStatus(taskID string) (*TaskStatus, error)
	GetTaskLogs(taskID string) (string, error)
	ListRecords(filter RecordFilter) ([]DeployRecord, int, error)
	ClearDeployHistory() (int, error)
	CancelTask(taskID string) error
}

// validTransitions defines the allowed state transitions for deployment tasks.
var validTransitions = map[string]map[string]bool{
	"pending": {
		"cloning": true,
		"failed":  true,
	},
	"cloning": {
		"building": true,
		"failed":   true,
	},
	"building": {
		"deploying": true,
		"failed":    true,
	},
	"deploying": {
		"success": true,
		"failed":  true,
	},
}

// IsValidTransition checks whether a state transition from one status to another is allowed.
func IsValidTransition(from, to string) bool {
	targets, ok := validTransitions[from]
	if !ok {
		return false
	}
	return targets[to]
}

// taskManager is the concrete implementation of TaskManager.
type taskManager struct {
	database   *sql.DB
	builder    builder.Builder
	deployer   deployer.Deployer
	cfg        *config.AppConfig
	cfgManager *config.Manager
	cancelMap  sync.Map // taskID -> context.CancelFunc
	historyMu  sync.Mutex
	frontendMu chan struct{}
	backendMu  chan struct{}
}

// NewTaskManager creates a new TaskManager instance.
// The builder, deployer, and cfg parameters may be nil if async deployment is not needed
// (e.g., in tests that only test task creation and querying).
func NewTaskManager(database *sql.DB, bldr builder.Builder, dplyr deployer.Deployer, cfg *config.AppConfig) TaskManager {
	return &taskManager{
		database:   database,
		builder:    bldr,
		deployer:   dplyr,
		cfg:        cfg,
		frontendMu: make(chan struct{}, 1),
		backendMu:  make(chan struct{}, 1),
	}
}

// NewTaskManagerWithConfigManager creates a TaskManager that reads config dynamically from the Manager.
func NewTaskManagerWithConfigManager(database *sql.DB, bldr builder.Builder, dplyr deployer.Deployer, cfgMgr *config.Manager) TaskManager {
	return &taskManager{
		database:   database,
		builder:    bldr,
		deployer:   dplyr,
		cfg:        cfgMgr.Get(),
		cfgManager: cfgMgr,
		frontendMu: make(chan struct{}, 1),
		backendMu:  make(chan struct{}, 1),
	}
}

// ctxErrMsg returns an appropriate error message based on context error type.
func ctxErrMsg(ctx context.Context) string {
	if ctx.Err() == context.DeadlineExceeded {
		return "部署超时（超过30分钟），已自动中断"
	}
	return "任务已被取消"
}

func deployCtxErrMsg(ctx context.Context, err error) string {
	if ctx.Err() == context.DeadlineExceeded || errors.Is(err, context.DeadlineExceeded) {
		return "部署超时（超过10分钟），已自动中断"
	}
	if ctx.Err() != nil {
		return ctxErrMsg(ctx)
	}
	return ""
}

func safeWorkDirSegment(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "_"
	}
	return strings.NewReplacer(
		"/", "_",
		"\\", "_",
		":", "_",
		"*", "_",
		"?", "_",
		"\"", "_",
		"<", "_",
		">", "_",
		"|", "_",
	).Replace(value)
}

func resolveBuildType(configuredType, buildCmd string) (string, error) {
	buildType := strings.ToLower(strings.TrimSpace(configuredType))
	if buildType == "" {
		command := strings.ToLower(buildCmd)
		if strings.Contains(command, "pnpm") || strings.Contains(command, "npm ") || strings.Contains(command, "yarn") {
			return frontendBuild, nil
		}
		return backendBuild, nil
	}
	if buildType != frontendBuild && buildType != backendBuild {
		return "", fmt.Errorf("invalid build_type %q: must be frontend or backend", configuredType)
	}
	return buildType, nil
}

func (m *taskManager) buildLane(buildType string) chan struct{} {
	if buildType == frontendBuild {
		return m.frontendMu
	}
	return m.backendMu
}

func (m *taskManager) acquireBuildLane(ctx context.Context, taskID, buildType string) error {
	lane := m.buildLane(buildType)
	m.appendLog(taskID, "queued", fmt.Sprintf("waiting for %s build lane", buildType))
	select {
	case lane <- struct{}{}:
		m.appendLog(taskID, "queued", fmt.Sprintf("acquired %s build lane", buildType))
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// getConfig returns the latest config, preferring the Manager if available.
func (m *taskManager) getConfig() *config.AppConfig {
	if m.cfgManager != nil {
		return m.cfgManager.Get()
	}
	return m.cfg
}

// CreateTask validates the deploy request, creates a new task record with status "pending",
// and launches the async deployment pipeline.
func (m *taskManager) CreateTask(req DeployRequest) (*db.DeployTask, error) {
	// Validate required fields.
	if req.ProjectOwner == "" {
		return nil, fmt.Errorf("project_owner must not be empty")
	}
	if req.ProjectName == "" {
		return nil, fmt.Errorf("project_name must not be empty")
	}
	if req.Branch == "" {
		return nil, fmt.Errorf("branch must not be empty")
	}
	refType := strings.TrimSpace(req.RefType)
	if refType == "" {
		refType = "branch"
	}
	if refType != "branch" && refType != "tag" {
		return nil, fmt.Errorf("ref_type must be branch or tag")
	}
	if req.Environment == "" {
		return nil, fmt.Errorf("environment must not be empty")
	}

	cfg := m.getConfig()
	component := strings.TrimSpace(req.Component)
	if component == "" {
		component = strings.TrimSpace(req.SubProject)
	}
	target := strings.TrimSpace(req.Target)
	serverKey := ""
	if cfg != nil {
		if _, ok := cfg.Environments[req.Environment]; !ok {
			return nil, fmt.Errorf("invalid environment %q", req.Environment)
		}
		environments, _, _, err := cfg.GetUnitEnvironments(req.ProjectName, component)
		if err != nil {
			return nil, err
		}
		unitEnvironment, ok := environments[req.Environment]
		if !ok {
			return nil, fmt.Errorf("environment %q is not configured for the selected project/component", req.Environment)
		}
		if target == "" {
			enabledTargets := make([]string, 0, len(unitEnvironment.Deployments))
			for key, deployment := range unitEnvironment.Deployments {
				if deployment.Disabled == nil || !*deployment.Disabled {
					enabledTargets = append(enabledTargets, key)
				}
			}
			if len(enabledTargets) != 1 {
				return nil, fmt.Errorf("target must be selected when environment %q has multiple deployment servers", req.Environment)
			}
			target = enabledTargets[0]
		}
		resolved, err := cfg.ResolveDeployment(req.ProjectName, component, req.Environment, target)
		if err != nil {
			return nil, err
		}
		if resolved.Deployment.Disabled != nil && *resolved.Deployment.Disabled {
			return nil, fmt.Errorf("target %q is disabled", target)
		}
		serverKey = resolved.ServerKey
	} else {
		switch req.Environment {
		case "dev", "sit", "prod":
		default:
			return nil, fmt.Errorf("invalid environment %q", req.Environment)
		}
	}

	now := time.Now().UTC()
	task := &db.DeployTask{
		OperatorType:  req.Principal.Type,
		OperatorValue: req.Principal.Value,
		OperatorIP:    req.Principal.IP,
		ID:            uuid.New().String(),
		ProjectOwner:  req.ProjectOwner,
		ProjectName:   req.ProjectName,
		SubProject:    component,
		TargetKey:     target,
		ServerKey:     serverKey,
		Branch:        req.Branch,
		RefType:       refType,
		CommitID:      strings.TrimSpace(req.CommitID),
		CommitMessage: req.CommitMessage,
		CommitTime:    req.CommitTime,
		Environment:   req.Environment,
		Status:        "pending",
		Logs:          "",
		ErrorMessage:  "",
		CreatedAt:     now,
		UpdatedAt:     now,
		FinishedAt:    nil,
	}

	m.historyMu.Lock()
	err := db.CreateTask(m.database, task)
	m.historyMu.Unlock()
	if err != nil {
		return nil, fmt.Errorf("failed to create task: %w", err)
	}

	// Launch async deployment if builder and deployer are available.
	if m.builder != nil && m.deployer != nil && m.getConfig() != nil {
		ctx, cancel := context.WithCancel(context.Background())
		m.cancelMap.Store(task.ID, cancel)
		go m.executeDeployment(ctx, task)
	}

	return task, nil
}

// executeDeployment runs the full async deployment pipeline:
// pending -> cloning -> building -> deploying -> success
// Any stage failure or cancellation sets the task to "failed" with the error recorded.
func (m *taskManager) executeDeployment(ctx context.Context, task *db.DeployTask) {
	defer m.cancelMap.Delete(task.ID)

	// Get the latest config at the start of deployment.
	cfg := m.getConfig()

	// --- Stage 1: Cloning ---
	db.UpdateTaskStatus(m.database, task.ID, "cloning")
	m.appendLog(task.ID, "cloning", "开始拉取代码...")

	if ctx.Err() != nil {
		m.failTask(task.ID, ctxErrMsg(ctx))
		return
	}

	// 解析不可歧义的部署目标。单体项目允许 component 为空，monorepo 必须选择真实组件。
	deploymentPlan, err := cfg.ResolveDeployment(task.ProjectName, task.SubProject, task.Environment, task.TargetKey)
	if err != nil {
		m.failTask(task.ID, fmt.Sprintf("获取部署配置失败: %v", err))
		return
	}
	m.appendLog(task.ID, "cloning", fmt.Sprintf("部署目标: %s (%s)", deploymentPlan.TargetLabel, deploymentPlan.ServerKey))

	buildType, err := resolveBuildType(deploymentPlan.BuildType, deploymentPlan.Build.BuildCmd)
	if err != nil {
		m.failTask(task.ID, err.Error())
		return
	}
	if err := m.acquireBuildLane(ctx, task.ID, buildType); err != nil {
		m.failTask(task.ID, ctxErrMsg(ctx))
		return
	}
	defer func() { <-m.buildLane(buildType) }()
	// Queue wait time is not part of the deployment execution timeout.
	ctx, cancelExecution := context.WithTimeout(ctx, taskTimeout)
	defer cancelExecution()

	// Build repo URL from Gitea config.
	giteaCfg := cfg.GetGiteaConfig()
	repoURL := buildRepoURL(giteaCfg, task.ProjectOwner, task.ProjectName)

	// Each build type has an independent serial lane. The lane lock protects this
	// shared repository checkout from concurrent branch switching and cleanup.
	workDir := filepath.Join(
		cfg.Server.Workspace,
		buildType,
		safeWorkDirSegment(task.ProjectOwner),
		safeWorkDirSegment(task.ProjectName),
	)

	refLabel := "分支"
	if task.RefType == "tag" {
		refLabel = "标签"
	}
	m.appendLog(task.ID, "cloning", fmt.Sprintf("代码引用: %s %s", refLabel, task.Branch))

	// Fetch and check out the exact ref selected by the user. The selected
	// commit is verified as well so the UI and build cannot silently diverge.
	workspaceOptions := builder.WorkspaceOptions{PreserveDependencies: buildType == frontendBuild}
	actualCommit, err := m.builder.CloneOrPull(repoURL, task.Branch, task.RefType, task.CommitID, workDir, workspaceOptions)
	if err != nil {
		m.failTask(task.ID, fmt.Sprintf("代码拉取失败: %v", err))
		return
	}
	m.appendLog(task.ID, "cloning", fmt.Sprintf("代码拉取完成，实际构建提交: %s", actualCommit))

	defer func() {
		if err := m.builder.CleanWorkDir(workDir, workspaceOptions); err != nil {
			m.appendLog(task.ID, "cleanup", fmt.Sprintf("cleaning build cache failed: %v", err))
			return
		}
		m.appendLog(task.ID, "cleanup", "已清理构建产物及临时文件；仅保留验证成功的前端依赖缓存")
	}()

	// --- Stage 2: Building ---
	if ctx.Err() != nil {
		m.failTask(task.ID, ctxErrMsg(ctx))
		return
	}
	db.UpdateTaskStatus(m.database, task.ID, "building")
	m.appendLog(task.ID, "building", "开始构建...")

	if _, err := m.builder.Build(ctx, workDir, deploymentPlan.Build.BuildCmd, task.Environment, func(line string) {
		m.appendLog(task.ID, "building", line)
	}); err != nil {
		if ctx.Err() != nil {
			m.failTask(task.ID, ctxErrMsg(ctx))
			return
		}
		m.failTask(task.ID, fmt.Sprintf("构建失败: %v", err))
		return
	}
	m.appendLog(task.ID, "building", "构建完成")

	// --- Stage 3: Deploying ---
	if ctx.Err() != nil {
		m.failTask(task.ID, ctxErrMsg(ctx))
		return
	}
	db.UpdateTaskStatus(m.database, task.ID, "deploying")
	m.appendLog(task.ID, "deploying", "开始部署...")
	deployCtx, cancelDeploy := context.WithTimeout(ctx, deployTimeout)
	defer cancelDeploy()

	// 所有产物先准备并上传，最后只执行一次目标服务器脚本。
	// 这避免多产物场景依赖“把脚本挂在最后一个产物上”的隐式顺序。
	for index, artifact := range deploymentPlan.Build.Artifacts {
		m.appendLog(task.ID, "deploying", fmt.Sprintf("准备产物 [%d/%d]: %s", index+1, len(deploymentPlan.Build.Artifacts), artifact.BuildOutput))
		artifactPath := filepath.Join(workDir, artifact.BuildOutput)
		artifactPath, err = m.prepareArtifact(artifactPath, artifact.RenameTo)
		if err != nil {
			m.failTask(task.ID, fmt.Sprintf("产物准备失败 [%s]: %v", artifact.BuildOutput, err))
			return
		}
		remoteArtifactPath := path.Join(deploymentPlan.Server.DeployPath, filepath.Base(artifactPath))
		m.appendLog(task.ID, "deploying", fmt.Sprintf(
			"上传产物 [%s] 到 [%s:%s]...",
			filepath.Base(artifactPath),
			deploymentPlan.Server.Host,
			remoteArtifactPath,
		))
		if err := m.deployer.Upload(deployCtx, artifactPath, deploymentPlan.Server); err != nil {
			if msg := deployCtxErrMsg(deployCtx, err); msg != "" {
				m.failTask(task.ID, msg)
				return
			}
			m.failTask(task.ID, fmt.Sprintf("产物上传失败 [%s]: %v", artifact.BuildOutput, err))
			return
		}
		m.appendLog(task.ID, "deploying", fmt.Sprintf("产物上传并校验完成 [%s]", remoteArtifactPath))
	}

	m.appendLog(task.ID, "deploying", fmt.Sprintf("执行部署脚本 [%s]...", deploymentPlan.TargetLabel))
	if _, err := m.deployer.Execute(deployCtx, deploymentPlan.Server, deploymentPlan.Deployment.DeployScript); err != nil {
		if msg := deployCtxErrMsg(deployCtx, err); msg != "" {
			m.failTask(task.ID, msg)
			return
		}
		m.failTask(task.ID, fmt.Sprintf("部署脚本执行失败: %v", err))
		return
	}

	m.appendLog(task.ID, "deploying", "部署完成")

	// --- Success ---
	db.UpdateTaskStatus(m.database, task.ID, "success")
}

// appendLog appends a formatted log line to the task's logs in the database.
// Log format: [2006-01-02T15:04:05Z] [stage] message
func (m *taskManager) appendLog(taskID, stage, message string) {
	timestamp := time.Now().UTC().Format("2006-01-02T15:04:05Z")
	logLine := fmt.Sprintf("[%s] [%s] %s", timestamp, stage, message)

	// Get current logs to append.
	task, err := db.GetTaskByID(m.database, taskID)
	if err != nil {
		return
	}

	newLogs := task.Logs
	if newLogs != "" {
		newLogs += "\n"
	}
	newLogs += logLine

	db.UpdateTaskLogs(m.database, taskID, newLogs)
}

func buildRepoURL(giteaCfg config.GiteaConfig, owner, repo string) string {
	baseURL := strings.TrimRight(giteaCfg.URL, "/")
	repoURL := baseURL + "/" + owner + "/" + repo + ".git"
	if strings.TrimSpace(giteaCfg.Token) == "" {
		return repoURL
	}

	parsed, err := url.Parse(repoURL)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") {
		return repoURL
	}

	username := strings.TrimSpace(giteaCfg.Username)
	if username == "" {
		username = owner
	}
	parsed.User = url.UserPassword(username, giteaCfg.Token)
	return parsed.String()
}

// failTask sets the task status to "failed" and records the error message.
func (m *taskManager) failTask(taskID, errorMsg string) {
	m.appendLog(taskID, "error", errorMsg)
	db.UpdateTaskStatus(m.database, taskID, "failed")
	db.UpdateTaskErrorMessage(m.database, taskID, errorMsg)
}

// GetTaskStatus retrieves the current status of a deployment task by its ID.
func (m *taskManager) GetTaskStatus(taskID string) (*TaskStatus, error) {
	task, err := db.GetTaskByID(m.database, taskID)
	if err != nil {
		return nil, err
	}

	return &TaskStatus{
		TaskID:      task.ID,
		Status:      task.Status,
		ProjectName: task.ProjectName,
		Component:   task.SubProject,
		SubProject:  task.SubProject,
		Target:      task.TargetKey,
		Server:      task.ServerKey,
		Branch:      task.Branch,
		Environment: task.Environment,
		CreatedAt:   task.CreatedAt,
		UpdatedAt:   task.UpdatedAt,
		FinishedAt:  task.FinishedAt,
	}, nil
}

// CancelTask cancels a running deployment task.
// Only tasks in cloning/building/deploying status can be cancelled.
// Regardless of whether the running context is found, the task will be marked as failed.
func (m *taskManager) CancelTask(taskID string) error {
	task, err := db.GetTaskByID(m.database, taskID)
	if err != nil {
		return fmt.Errorf("任务不存在: %w", err)
	}

	// Only allow cancelling tasks that are in progress
	if task.Status != "cloning" && task.Status != "building" && task.Status != "deploying" {
		return fmt.Errorf("任务当前状态为 %q，无法中断（仅允许中断执行中的任务）", task.Status)
	}

	// Try to cancel the running goroutine if context exists
	if cancel, ok := m.cancelMap.Load(taskID); ok {
		cancel.(context.CancelFunc)()
	}

	// Always mark the task as failed, regardless of whether context was found
	m.failTask(taskID, "任务已被用户中断")
	return nil
}

// GetTaskLogs retrieves the execution logs of a deployment task by its ID.
func (m *taskManager) GetTaskLogs(taskID string) (string, error) {
	task, err := db.GetTaskByID(m.database, taskID)
	if err != nil {
		return "", err
	}

	return task.Logs, nil
}

// ListRecords returns a paginated list of deployment records matching the given filter,
// along with the total count of matching records.
func (m *taskManager) ListRecords(filter RecordFilter) ([]DeployRecord, int, error) {
	dbFilter := db.RecordFilter{
		Project:     filter.Project,
		Environment: filter.Environment,
		Status:      filter.Status,
		Page:        filter.Page,
		PageSize:    filter.PageSize,
	}

	needsLabelFilter := strings.TrimSpace(filter.ProjectKeyword) != "" || strings.TrimSpace(filter.SubProjectKeyword) != ""
	var tasks []db.DeployTask
	var total int
	var err error
	if needsLabelFilter {
		tasks, _, err = db.ListAllRecords(m.database, dbFilter)
	} else {
		tasks, total, err = db.ListRecords(m.database, dbFilter)
	}
	if err != nil {
		return nil, 0, err
	}

	records := make([]DeployRecord, 0, len(tasks))
	cfg := m.getConfig()
	for _, t := range tasks {
		projectLabel := t.ProjectName
		componentLabel := t.SubProject
		targetLabel := t.TargetKey
		if cfg != nil {
			if projCfg, ok := cfg.Projects[t.ProjectName]; ok {
				if projCfg.Label != "" {
					projectLabel = projCfg.Label
				}
				if component, ok := projCfg.Components[t.SubProject]; ok && component.Label != "" {
					componentLabel = component.Label
				}
				if t.TargetKey != "" {
					if resolved, resolveErr := cfg.ResolveDeployment(t.ProjectName, t.SubProject, t.Environment, t.TargetKey); resolveErr == nil {
						targetLabel = resolved.TargetLabel
					}
				}
			}
		}
		record := DeployRecord{
			Operator:        (identity.Principal{Type: t.OperatorType, Value: t.OperatorValue}).Display(),
			OperatorType:    t.OperatorType,
			ID:              t.ID,
			ProjectOwner:    t.ProjectOwner,
			ProjectName:     t.ProjectName,
			ProjectLabel:    projectLabel,
			Component:       t.SubProject,
			ComponentLabel:  componentLabel,
			SubProject:      t.SubProject,
			SubProjectLabel: componentLabel,
			Target:          t.TargetKey,
			TargetLabel:     targetLabel,
			Server:          t.ServerKey,
			Branch:          t.Branch,
			CommitMessage:   t.CommitMessage,
			CommitTime:      t.CommitTime,
			Environment:     t.Environment,
			Status:          t.Status,
			CreatedAt:       t.CreatedAt,
			FinishedAt:      t.FinishedAt,
		}
		if !recordMatchesKeywords(record, filter.ProjectKeyword, filter.SubProjectKeyword) {
			continue
		}
		records = append(records, record)
	}

	if needsLabelFilter {
		total = len(records)
		records = paginateRecords(records, filter.Page, filter.PageSize)
	}

	return records, total, nil
}

func recordMatchesKeywords(record DeployRecord, projectKeyword, subProjectKeyword string) bool {
	projectKeyword = strings.ToLower(strings.TrimSpace(projectKeyword))
	subProjectKeyword = strings.ToLower(strings.TrimSpace(subProjectKeyword))

	if projectKeyword != "" && !containsFold(record.ProjectName, projectKeyword) && !containsFold(record.ProjectLabel, projectKeyword) {
		return false
	}
	if subProjectKeyword != "" && !containsFold(record.SubProject, subProjectKeyword) && !containsFold(record.SubProjectLabel, subProjectKeyword) {
		return false
	}
	return true
}

func containsFold(value, keyword string) bool {
	return strings.Contains(strings.ToLower(value), keyword)
}

func paginateRecords(records []DeployRecord, page, pageSize int) []DeployRecord {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(records) {
		return []DeployRecord{}
	}
	end := start + pageSize
	if end > len(records) {
		end = len(records)
	}
	return records[start:end]
}

// ClearDeployHistory removes deployment records and their stored logs, then
// compacts SQLite. Active tasks are kept intact to avoid orphaning them.
func (m *taskManager) ClearDeployHistory() (int, error) {
	m.historyMu.Lock()
	defer m.historyMu.Unlock()

	hasActive, err := db.HasActiveTasks(m.database)
	if err != nil {
		return 0, err
	}
	if hasActive {
		return 0, ErrDeploymentsInProgress
	}

	return db.ClearDeployHistory(m.database)
}

// prepareArtifact checks if the artifact path is a directory or file.
// If it's a directory, it creates a tar.gz archive and returns the archive path.
// If renameTo is set and the artifact is a file, it renames the file.
// Returns the final path to upload.
func (m *taskManager) prepareArtifact(artifactPath string, renameTo string) (string, error) {
	info, err := os.Stat(artifactPath)
	if err != nil {
		return "", fmt.Errorf("产物不存在: %s: %w", artifactPath, err)
	}

	if info.IsDir() {
		// Directory: create a tar.gz archive
		tarPath := artifactPath + ".tar.gz"
		if err := createTarGz(tarPath, artifactPath); err != nil {
			return "", fmt.Errorf("打包目录失败: %w", err)
		}
		// If renameTo is set, rename the tar.gz
		if renameTo != "" {
			dir := filepath.Dir(tarPath)
			newPath := filepath.Join(dir, renameTo)
			if err := os.Rename(tarPath, newPath); err != nil {
				return "", fmt.Errorf("重命名失败: %w", err)
			}
			return newPath, nil
		}
		return tarPath, nil
	}

	// Single file: rename if needed
	if renameTo != "" {
		dir := filepath.Dir(artifactPath)
		newPath := filepath.Join(dir, renameTo)
		if err := os.Rename(artifactPath, newPath); err != nil {
			return "", fmt.Errorf("重命名失败: %w", err)
		}
		return newPath, nil
	}

	return artifactPath, nil
}

// createTarGz creates a tar.gz archive of the given source directory.
func createTarGz(tarPath string, sourceDir string) error {
	tarFile, err := os.Create(tarPath)
	if err != nil {
		return err
	}
	defer tarFile.Close()

	gzWriter := gzip.NewWriter(tarFile)
	defer gzWriter.Close()

	tarWriter := tar.NewWriter(gzWriter)
	defer tarWriter.Close()

	return filepath.Walk(sourceDir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// Use relative path inside the archive
		relPath, err := filepath.Rel(filepath.Dir(sourceDir), path)
		if err != nil {
			return err
		}
		header.Name = relPath

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}

		// Write file content (skip directories)
		if info.IsDir() {
			return nil
		}

		file, err := os.Open(path)
		if err != nil {
			return err
		}
		defer file.Close()

		_, err = io.Copy(tarWriter, file)
		return err
	})
}
