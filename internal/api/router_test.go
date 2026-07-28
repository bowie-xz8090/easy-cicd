package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"auto-deploy-platform/internal/config"
	"auto-deploy-platform/internal/db"
	"auto-deploy-platform/internal/gitea"
	"auto-deploy-platform/internal/task"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// mockGiteaClient is a test double for gitea.GiteaClient.
type mockGiteaClient struct {
	repos     []gitea.Repository
	branches  []gitea.Branch
	tags      []gitea.Tag
	repoErr   error
	branchErr error
	tagErr    error
}

func (m *mockGiteaClient) ListRepos() ([]gitea.Repository, error) {
	if m.repoErr != nil {
		return nil, m.repoErr
	}
	return m.repos, nil
}

func (m *mockGiteaClient) ListBranches(owner, repo string) ([]gitea.Branch, error) {
	if m.branchErr != nil {
		return nil, m.branchErr
	}
	return m.branches, nil
}

func (m *mockGiteaClient) ListTags(owner, repo string) ([]gitea.Tag, error) {
	if m.tagErr != nil {
		return nil, m.tagErr
	}
	return m.tags, nil
}

// mockTaskManager is a test double for task.TaskManager.
type mockTaskManager struct {
	createTaskFn   func(req task.DeployRequest) (*db.DeployTask, error)
	getStatusFn    func(taskID string) (*task.TaskStatus, error)
	getLogsFn      func(taskID string) (string, error)
	listRecordsFn  func(filter task.RecordFilter) ([]task.DeployRecord, int, error)
	clearHistoryFn func() (int, error)
	cancelTaskFn   func(taskID string) error
}

func (m *mockTaskManager) CreateTask(req task.DeployRequest) (*db.DeployTask, error) {
	if m.createTaskFn != nil {
		return m.createTaskFn(req)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockTaskManager) GetTaskStatus(taskID string) (*task.TaskStatus, error) {
	if m.getStatusFn != nil {
		return m.getStatusFn(taskID)
	}
	return nil, fmt.Errorf("not implemented")
}

func (m *mockTaskManager) GetTaskLogs(taskID string) (string, error) {
	if m.getLogsFn != nil {
		return m.getLogsFn(taskID)
	}
	return "", fmt.Errorf("not implemented")
}

func (m *mockTaskManager) ListRecords(filter task.RecordFilter) ([]task.DeployRecord, int, error) {
	if m.listRecordsFn != nil {
		return m.listRecordsFn(filter)
	}
	return nil, 0, fmt.Errorf("not implemented")
}

func (m *mockTaskManager) ClearDeployHistory() (int, error) {
	if m.clearHistoryFn != nil {
		return m.clearHistoryFn()
	}
	return 0, fmt.Errorf("not implemented")
}

func (m *mockTaskManager) CancelTask(taskID string) error {
	if m.cancelTaskFn != nil {
		return m.cancelTaskFn(taskID)
	}
	return fmt.Errorf("not implemented")
}

func setupRouter(giteaClient gitea.GiteaClient, cfg *config.AppConfig) *gin.Engine {
	return setupRouterWithTaskMgr(giteaClient, cfg, nil)
}

func setupRouterWithTaskMgr(giteaClient gitea.GiteaClient, cfg *config.AppConfig, taskMgr task.TaskManager) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(giteaClient, cfg, taskMgr)
	h.RegisterRoutes(r)
	return r
}

func setupRouterWithBasePath(giteaClient gitea.GiteaClient, cfg *config.AppConfig, basePath string) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	h := NewHandler(giteaClient, cfg, nil)
	h.RegisterRoutesWithBasePath(r, basePath)
	return r
}

func TestHandleClearDeployHistory(t *testing.T) {
	cleared := false
	taskMgr := &mockTaskManager{
		clearHistoryFn: func() (int, error) {
			cleared = true
			return 3, nil
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)
	req := httptest.NewRequest(http.MethodDelete, "/api/deploy/records", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	require.True(t, cleared)
	assert.Equal(t, http.StatusOK, w.Code)
	var resp map[string]interface{}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	assert.Equal(t, float64(0), resp["code"])
	assert.Equal(t, float64(3), resp["data"].(map[string]interface{})["deleted"])
}

func TestHandleClearDeployHistory_RejectsActiveTasks(t *testing.T) {
	taskMgr := &mockTaskManager{
		clearHistoryFn: func() (int, error) {
			return 0, task.ErrDeploymentsInProgress
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)
	req := httptest.NewRequest(http.MethodDelete, "/api/deploy/records", nil)
	w := httptest.NewRecorder()

	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
	assert.Contains(t, w.Body.String(), "进行中的部署任务")
}

func testConfig() *config.AppConfig {
	devRule := config.UnitEnvConfig{
		Build: config.BuildConfig{
			BuildCmd:  "go build ./...",
			Artifacts: []config.ArtifactConfig{{Key: "app", BuildOutput: "app.tar.gz", RenameTo: "app.tar.gz"}},
		},
		Deployments: map[string]config.DeploymentConfig{
			"dev-server": {Label: "开发服务器", Server: "dev-server", DeployScript: "echo deploy"},
		},
	}
	return &config.AppConfig{
		ConfigVersion: config.CurrentVersion,
		Servers: map[string]config.ServerConfig{
			"dev-server": {
				Label: "开发服务器",
				Host:  "192.168.1.10",
				Port:  22,
				User:  "deploy",
			},
		},
		Environments: map[string]config.EnvConfig{
			"dev": {
				Label:   "开发环境",
				Servers: []string{"dev-server"},
			},
			"sit": {
				Label: "集成测试环境",
			},
			"prod": {
				Label: "生产环境",
			},
		},
		Projects: map[string]config.ProjectConfig{
			"project-a": {
				Label:        "项目A",
				Mode:         "single",
				BuildType:    "backend",
				Environments: map[string]config.UnitEnvConfig{"dev": devRule},
				Components:   map[string]config.ComponentConfig{},
			},
			"project-b": {
				Label:        "项目B",
				Mode:         "monorepo",
				Environments: map[string]config.UnitEnvConfig{},
				Components: map[string]config.ComponentConfig{
					"web": {
						Label:        "Web",
						Path:         "apps/web",
						BuildType:    "frontend",
						Environments: map[string]config.UnitEnvConfig{"dev": devRule},
					},
				},
			},
		},
	}
}

func TestEditableConfig_ServerSummaryIncludesConnectionAddress(t *testing.T) {
	cfg := testConfig()
	server := cfg.Servers["dev-server"]
	server.DeployPath = "/root/temp"
	cfg.Servers["dev-server"] = server

	editable := editableConfig(cfg)

	require.Len(t, editable.Servers, 1)
	assert.Equal(t, "192.168.1.10", editable.Servers[0].Host)
	assert.Equal(t, 22, editable.Servers[0].Port)
	assert.Equal(t, "/root/temp", editable.Servers[0].DeployPath)
}

func TestHealthEndpoint(t *testing.T) {
	r := setupRouter(&mockGiteaClient{}, testConfig())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/health", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])
	assert.Equal(t, "ok", resp["data"])
}

func TestHealthEndpointWithBasePath(t *testing.T) {
	r := setupRouterWithBasePath(&mockGiteaClient{}, testConfig(), "/deploy")

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/deploy/api/health", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])
	assert.Equal(t, "ok", resp["data"])
}

func TestListProjects_Success(t *testing.T) {
	mock := &mockGiteaClient{
		repos: []gitea.Repository{
			{Owner: "org", Name: "project-a", FullName: "org/project-a", Description: "Project A", CloneURL: "http://gitea.example.com/org/project-a.git"},
			{Owner: "org", Name: "project-b", FullName: "org/project-b", Description: "Project B", CloneURL: "http://gitea.example.com/org/project-b.git"},
		},
	}
	r := setupRouter(mock, testConfig())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/projects", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])

	data, ok := resp["data"].([]interface{})
	require.True(t, ok)
	assert.Len(t, data, 2)

	first := data[0].(map[string]interface{})
	assert.Equal(t, "org", first["owner"])
	assert.Equal(t, "project-a", first["name"])
	assert.Equal(t, "项目A", first["label"])
	assert.Equal(t, "single", first["mode"])
	assert.Equal(t, true, first["selectable"])
	assert.Empty(t, first["components"])
}

func TestListProjects_GiteaError(t *testing.T) {
	mock := &mockGiteaClient{
		repoErr: assert.AnError,
	}
	r := setupRouter(mock, testConfig())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/projects", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(-1), resp["code"])
	assert.Contains(t, resp["message"], "获取项目列表失败")
}

func TestListBranches_Success(t *testing.T) {
	commitTime := time.Date(2026, 7, 21, 9, 30, 0, 0, time.UTC)
	mock := &mockGiteaClient{
		branches: []gitea.Branch{
			{Name: "main", CommitID: "abc123"},
			{Name: "develop", CommitID: "def456", CommitMessage: "add deploy config", CommitTime: &commitTime},
		},
	}
	r := setupRouter(mock, testConfig())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/projects/org/my-repo/branches", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])

	data, ok := resp["data"].([]interface{})
	require.True(t, ok)
	assert.Len(t, data, 2)

	first := data[0].(map[string]interface{})
	assert.Equal(t, "develop", first["name"])
	assert.Equal(t, "def456", first["commit_id"])
	assert.Equal(t, "add deploy config", first["commit_message"])
	assert.Equal(t, "2026-07-21T09:30:00Z", first["commit_time"])
}

func TestListTags_Success(t *testing.T) {
	commitTime := time.Date(2026, 7, 20, 18, 45, 0, 0, time.UTC)
	mock := &mockGiteaClient{
		tags: []gitea.Tag{
			{Name: "v1.1.0", CommitID: "abc123"},
			{Name: "v1.0.0", CommitID: "def456", CommitMessage: "release v1.0.0", CommitTime: &commitTime},
		},
	}
	r := setupRouter(mock, testConfig())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/projects/org/my-repo/tags", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])

	data, ok := resp["data"].([]interface{})
	require.True(t, ok)
	assert.Len(t, data, 2)

	first := data[0].(map[string]interface{})
	assert.Equal(t, "v1.0.0", first["name"])
	assert.Equal(t, "def456", first["commit_id"])
	assert.Equal(t, "release v1.0.0", first["commit_message"])
	assert.Equal(t, "2026-07-20T18:45:00Z", first["commit_time"])
}

func TestListBranches_GiteaError(t *testing.T) {
	mock := &mockGiteaClient{
		branchErr: assert.AnError,
	}
	r := setupRouter(mock, testConfig())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/projects/org/my-repo/branches", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadGateway, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(-1), resp["code"])
	assert.Contains(t, resp["message"], "获取分支列表失败")
}

func TestListEnvironments(t *testing.T) {
	r := setupRouter(&mockGiteaClient{}, testConfig())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/environments", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])

	data, ok := resp["data"].([]interface{})
	require.True(t, ok)
	assert.Len(t, data, 3)

	// Collect all keys and labels from the response
	keys := make(map[string]string)
	for _, item := range data {
		env := item.(map[string]interface{})
		key := env["key"].(string)
		label := env["label"].(string)
		keys[key] = label
	}

	assert.Equal(t, "开发环境", keys["dev"])
	assert.Equal(t, "集成测试环境", keys["sit"])
	assert.Equal(t, "生产环境", keys["prod"])
}

func TestListEnvironmentsForSelectedProjectReturnsDeploymentTargets(t *testing.T) {
	r := setupRouter(&mockGiteaClient{}, testConfig())

	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/environments?project=project-a", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	var response struct {
		Code int               `json:"code"`
		Data []EnvironmentItem `json:"data"`
	}
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &response))
	require.Equal(t, 0, response.Code)
	require.NotEmpty(t, response.Data)

	var dev EnvironmentItem
	for _, environment := range response.Data {
		if environment.Key == "dev" {
			dev = environment
			break
		}
	}
	require.False(t, dev.Disabled)
	require.Len(t, dev.Targets, 1)
	assert.Equal(t, "dev-server", dev.Targets[0].Key)
	assert.Equal(t, "dev-server", dev.Targets[0].Server)
	assert.Equal(t, "开发服务器", dev.Targets[0].ServerLabel)
	assert.Equal(t, "192.168.1.10", dev.Targets[0].ServerHost)
	assert.Equal(t, 22, dev.Targets[0].ServerPort)
}

func TestListEnvironmentsForMonorepoRequiresRealComponent(t *testing.T) {
	r := setupRouter(&mockGiteaClient{}, testConfig())

	withoutComponent := httptest.NewRecorder()
	r.ServeHTTP(withoutComponent, httptest.NewRequest(http.MethodGet, "/api/environments?project=project-b", nil))
	assert.Contains(t, withoutComponent.Body.String(), `"disabled":true`)

	withComponent := httptest.NewRecorder()
	r.ServeHTTP(withComponent, httptest.NewRequest(http.MethodGet, "/api/environments?project=project-b&component=web", nil))
	assert.Contains(t, withComponent.Body.String(), `"key":"dev-server"`)
}

func TestAllEndpoints_NoAuth(t *testing.T) {
	mock := &mockGiteaClient{
		repos:    []gitea.Repository{},
		branches: []gitea.Branch{},
	}
	taskMgr := &mockTaskManager{
		listRecordsFn: func(filter task.RecordFilter) ([]task.DeployRecord, int, error) {
			return []task.DeployRecord{}, 0, nil
		},
	}
	r := setupRouterWithTaskMgr(mock, testConfig(), taskMgr)

	// All endpoints should be accessible without any auth headers
	endpoints := []struct {
		method string
		path   string
	}{
		{http.MethodGet, "/api/health"},
		{http.MethodGet, "/api/projects"},
		{http.MethodGet, "/api/projects/org/repo/branches"},
		{http.MethodGet, "/api/environments"},
		{http.MethodGet, "/api/deploy/records"},
	}

	for _, ep := range endpoints {
		t.Run(ep.method+" "+ep.path, func(t *testing.T) {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest(ep.method, ep.path, nil)
			// No Authorization header set
			r.ServeHTTP(w, req)

			// Should not return 401 or 403
			assert.NotEqual(t, http.StatusUnauthorized, w.Code)
			assert.NotEqual(t, http.StatusForbidden, w.Code)
		})
	}
}

func TestListProjects_EmptyList(t *testing.T) {
	mock := &mockGiteaClient{
		repos: []gitea.Repository{},
	}
	r := setupRouter(mock, testConfig())

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/projects", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])

	data, ok := resp["data"].([]interface{})
	require.True(t, ok)
	assert.Len(t, data, 0)
}

// --- Deploy endpoint tests ---

func TestHandleDeploy_Success(t *testing.T) {
	now := time.Now().UTC()
	taskMgr := &mockTaskManager{
		createTaskFn: func(req task.DeployRequest) (*db.DeployTask, error) {
			return &db.DeployTask{
				ID:           "test-uuid-123",
				ProjectOwner: req.ProjectOwner,
				ProjectName:  req.ProjectName,
				Branch:       req.Branch,
				Environment:  req.Environment,
				Status:       "pending",
				CreatedAt:    now,
				UpdatedAt:    now,
			}, nil
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)

	body := `{"project_owner":"org","project_name":"project-a","branch":"main","environment":"dev","target":"dev-server"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/deploy", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])

	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "test-uuid-123", data["task_id"])
	assert.Equal(t, "pending", data["status"])
	assert.NotEmpty(t, data["created_at"])
}

func TestHandleDeploy_InvalidJSON(t *testing.T) {
	taskMgr := &mockTaskManager{}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)

	body := `{invalid json}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/deploy", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(-1), resp["code"])
	assert.Contains(t, resp["message"], "请求参数错误")
}

func TestHandleDeploy_ValidationError(t *testing.T) {
	taskMgr := &mockTaskManager{
		createTaskFn: func(req task.DeployRequest) (*db.DeployTask, error) {
			return nil, fmt.Errorf("project_owner must not be empty")
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)

	body := `{"project_owner":"","project_name":"project-a","branch":"main","environment":"dev","target":"dev-server"}`
	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodPost, "/api/deploy", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(-1), resp["code"])
	assert.Contains(t, resp["message"], "project_owner must not be empty")
}

func TestHandleDeploy_RejectsDisabledDeploymentTarget(t *testing.T) {
	cfg := testConfig()
	disabled := true
	project := cfg.Projects["project-a"]
	rule := project.Environments["dev"]
	deployment := rule.Deployments["dev-server"]
	deployment.Disabled = &disabled
	rule.Deployments["dev-server"] = deployment
	project.Environments["dev"] = rule
	cfg.Projects["project-a"] = project
	taskMgr := &mockTaskManager{
		createTaskFn: func(req task.DeployRequest) (*db.DeployTask, error) {
			t.Fatal("disabled environment must not create a deployment task")
			return nil, nil
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, cfg, taskMgr)

	body := `{"project_owner":"org","project_name":"project-a","branch":"main","environment":"dev","target":"dev-server"}`
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/deploy", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusForbidden, w.Code)
	assert.Contains(t, w.Body.String(), "未开放部署")
}

func TestHandleDeployStatus_Success(t *testing.T) {
	now := time.Now().UTC()
	taskMgr := &mockTaskManager{
		getStatusFn: func(taskID string) (*task.TaskStatus, error) {
			return &task.TaskStatus{
				TaskID:      taskID,
				Status:      "building",
				ProjectName: "my-app",
				Branch:      "main",
				Environment: "dev",
				CreatedAt:   now,
				UpdatedAt:   now,
			}, nil
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/deploy/test-uuid-123/status", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])

	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "test-uuid-123", data["task_id"])
	assert.Equal(t, "building", data["status"])
	assert.Equal(t, "my-app", data["project_name"])
	assert.Equal(t, "main", data["branch"])
	assert.Equal(t, "dev", data["environment"])
}

func TestHandleDeployStatus_NotFound(t *testing.T) {
	taskMgr := &mockTaskManager{
		getStatusFn: func(taskID string) (*task.TaskStatus, error) {
			return nil, fmt.Errorf("task not found: %s", taskID)
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/deploy/nonexistent/status", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(-1), resp["code"])
	assert.Contains(t, resp["message"], "task not found")
}

func TestHandleDeployLogs_Success(t *testing.T) {
	taskMgr := &mockTaskManager{
		getLogsFn: func(taskID string) (string, error) {
			return "[2024-01-01T00:00:00Z] [cloning] 开始拉取代码...", nil
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/deploy/test-uuid-123/logs", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])

	data := resp["data"].(map[string]interface{})
	assert.Equal(t, "test-uuid-123", data["task_id"])
	assert.Contains(t, data["logs"], "开始拉取代码")
}

func TestHandleDeployLogs_NotFound(t *testing.T) {
	taskMgr := &mockTaskManager{
		getLogsFn: func(taskID string) (string, error) {
			return "", fmt.Errorf("task not found: %s", taskID)
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/deploy/nonexistent/logs", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(-1), resp["code"])
	assert.Contains(t, resp["message"], "task not found")
}

func TestHandleDeployRecords_Success(t *testing.T) {
	now := time.Now().UTC()
	commitTime := time.Date(2026, 7, 21, 10, 15, 0, 0, time.UTC)
	taskMgr := &mockTaskManager{
		listRecordsFn: func(filter task.RecordFilter) ([]task.DeployRecord, int, error) {
			records := []task.DeployRecord{
				{
					ID:            "uuid-1",
					ProjectOwner:  "org",
					ProjectName:   "my-app",
					Branch:        "main",
					CommitMessage: "add history commit info",
					CommitTime:    &commitTime,
					Environment:   "dev",
					Status:        "success",
					CreatedAt:     now,
				},
			}
			return records, 1, nil
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/deploy/records", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])

	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(1), data["total"])

	records := data["records"].([]interface{})
	assert.Len(t, records, 1)

	first := records[0].(map[string]interface{})
	assert.Equal(t, "uuid-1", first["id"])
	assert.Equal(t, "my-app", first["project_name"])
	assert.Equal(t, "add history commit info", first["commit_message"])
	assert.Equal(t, "2026-07-21T10:15:00Z", first["commit_time"])
	assert.Equal(t, "success", first["status"])
}

func TestHandleDeployRecords_WithFilters(t *testing.T) {
	var capturedFilter task.RecordFilter
	taskMgr := &mockTaskManager{
		listRecordsFn: func(filter task.RecordFilter) ([]task.DeployRecord, int, error) {
			capturedFilter = filter
			return []task.DeployRecord{}, 0, nil
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/deploy/records?project=my-app&project_keyword=app&sub_project_keyword=admin&environment=dev&status=success&page=2&page_size=10", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "my-app", capturedFilter.Project)
	assert.Equal(t, "app", capturedFilter.ProjectKeyword)
	assert.Equal(t, "admin", capturedFilter.SubProjectKeyword)
	assert.Equal(t, "dev", capturedFilter.Environment)
	assert.Equal(t, "success", capturedFilter.Status)
	assert.Equal(t, 2, capturedFilter.Page)
	assert.Equal(t, 10, capturedFilter.PageSize)
}

func TestHandleDeployRecords_EmptyList(t *testing.T) {
	taskMgr := &mockTaskManager{
		listRecordsFn: func(filter task.RecordFilter) ([]task.DeployRecord, int, error) {
			return []task.DeployRecord{}, 0, nil
		},
	}
	r := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), taskMgr)

	w := httptest.NewRecorder()
	req, _ := http.NewRequest(http.MethodGet, "/api/deploy/records", nil)
	r.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)

	var resp map[string]interface{}
	err := json.Unmarshal(w.Body.Bytes(), &resp)
	require.NoError(t, err)
	assert.Equal(t, float64(0), resp["code"])

	data := resp["data"].(map[string]interface{})
	assert.Equal(t, float64(0), data["total"])

	records := data["records"].([]interface{})
	assert.Len(t, records, 0)
}
