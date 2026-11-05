package api

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"auto-deploy-platform/internal/db"
	"auto-deploy-platform/internal/task"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAllBusinessRoutesRequireAccess(t *testing.T) {
	cfg := testConfig()
	cfg.AdminToken = "admin-secret"
	gin.SetMode(gin.TestMode)
	router := gin.New()
	NewHandler(&mockGiteaClient{}, cfg, &mockTaskManager{}).RegisterRoutes(router)
	public := map[string]bool{"/api/health": true, "/api/site-info": true, "/api/auth/session": true, "/api/auth/oidc/login": true, "/api/auth/oidc/callback": true}
	for _, route := range router.Routes() {
		if public[route.Path] {
			continue
		}
		t.Run(route.Method+route.Path, func(t *testing.T) {
			path := strings.NewReplacer(":owner", "owner", ":repo", "repo", ":id", "task-1", ":key", "dev").Replace(route.Path)
			request := httptest.NewRequest(route.Method, path, bytes.NewBufferString(`{}`))
			request.RemoteAddr = "198.51.100.1:1234"
			request.Header.Set("X-Forwarded-For", "192.0.2.1")
			request.Header.Set("Authorization", "Bearer "+cfg.AdminToken)
			request.Header.Set("X-CICD-Request", "1")
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			assert.Equal(t, 401, response.Code)
			assert.Contains(t, response.Body.String(), `"code":40101`)
		})
	}
	for _, path := range []string{"/api/health", "/api/site-info", "/api/auth/session"} {
		request := httptest.NewRequest("GET", path, nil)
		request.RemoteAddr = "198.51.100.1:1234"
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		assert.Equal(t, 200, response.Code)
	}
}

func TestDeployUsesServerEstablishedOperator(t *testing.T) {
	var captured task.DeployRequest
	mgr := &mockTaskManager{createTaskFn: func(req task.DeployRequest) (*db.DeployTask, error) {
		captured = req
		return &db.DeployTask{ID: "task-1", Status: "pending"}, nil
	}}
	router := setupRouterWithTaskMgr(&mockGiteaClient{}, testConfig(), mgr)
	request := httptest.NewRequest("POST", "/api/deploy", bytes.NewBufferString(`{"project_owner":"org","project_name":"project-a","branch":"main","environment":"dev","target":"dev-server","operator":"forged","operator_type":"oidc","principal":{"Type":"oidc","Value":"19955552780"}}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code, response.Body.String())
	assert.Equal(t, "ip", captured.Principal.Type)
	assert.Equal(t, "192.0.2.1", captured.Principal.Value)
	assert.Equal(t, "192.0.2.1", captured.Principal.IP)
}

func TestAccessDoesNotReplaceInternalPasswords(t *testing.T) {
	cfg := testConfig()
	cfg.AdminToken = "admin-secret"
	env := cfg.Environments["dev"]
	env.Password = "deploy-secret"
	cfg.Environments["dev"] = env
	router := setupRouterWithTaskMgr(&mockGiteaClient{}, cfg, &mockTaskManager{})
	request := httptest.NewRequest("POST", "/api/deploy", bytes.NewBufferString(`{"project_owner":"org","project_name":"project-a","branch":"main","environment":"dev","target":"dev-server"}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, 403, response.Code)
	assert.Contains(t, response.Body.String(), "部署密码错误")
	request = httptest.NewRequest("GET", "/api/config/editor", nil)
	response = httptest.NewRecorder()
	router.ServeHTTP(response, request)
	assert.Equal(t, 401, response.Code)
	assert.NotContains(t, response.Body.String(), `"code":40101`)
}
