package main

import (
	"fmt"
	"html"
	"io"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"auto-deploy-platform/internal/api"
	"auto-deploy-platform/internal/auth"
	"auto-deploy-platform/internal/builder"
	"auto-deploy-platform/internal/config"
	"auto-deploy-platform/internal/db"
	"auto-deploy-platform/internal/deployer"
	"auto-deploy-platform/internal/gitea"
	"auto-deploy-platform/internal/task"

	"github.com/gin-gonic/gin"
)

func main() {
	// Determine config file path
	configPath := "config.yaml"
	if envPath := os.Getenv("CONFIG_PATH"); envPath != "" {
		configPath = envPath
	}

	// Initialize config manager (supports dynamic reload)
	cfgManager, err := config.NewManager(configPath)
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	cfg := cfgManager.Get()

	// Initialize SQLite database
	dbPath := "deploy.db"
	if envDB := os.Getenv("DB_PATH"); envDB != "" {
		dbPath = envDB
	}
	database, err := db.InitDB(dbPath)
	if err != nil {
		log.Fatalf("Failed to initialize database: %v", err)
	}
	defer database.Close()

	// Create Gitea client
	giteaCfg := cfg.GetGiteaConfig()
	giteaClient := gitea.NewGiteaClient(giteaCfg.URL, giteaCfg.Token)

	// Create Builder and Deployer
	bldr := builder.NewBuilder()
	dplyr := deployer.NewDeployer()

	// Create TaskManager
	taskMgr := task.NewTaskManagerWithConfigManager(database, bldr, dplyr, cfgManager)

	// Create API handler with config manager for dynamic config support
	handler := api.NewHandlerWithManager(giteaClient, cfgManager, taskMgr)

	// Initialize Gin engine
	r := gin.New()
	r.Use(safeRequestLogger(), gin.CustomRecoveryWithWriter(io.Discard, func(c *gin.Context, _ any) {
		log.Print("Request failed with an internal panic")
		c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"code": -1, "message": "服务暂时不可用"})
	}))
	basePath := resolveBasePath(cfg)
	handler.SetAccessService(auth.NewService(database, cfgManager.Get, basePath))
	handler.RegisterRoutesWithBasePath(r, basePath)

	// Serve frontend static files in production mode
	distDir := "./web/dist"
	if _, err := os.Stat(distDir); err == nil {
		indexFile := distDir + "/index.html"
		r.Use(frontendAssetCacheHeaders(basePath))
		r.Static(joinAppPath(basePath, "/assets"), distDir+"/assets")
		r.GET(joinAppPath(basePath, "/"), func(c *gin.Context) {
			serveFrontendIndex(c, indexFile, basePath)
		})
		if basePath != "" {
			r.GET(basePath, func(c *gin.Context) {
				c.Redirect(http.StatusMovedPermanently, basePath+"/")
			})
		}
		// SPA fallback: serve index.html for any route not matched by API or static assets
		r.NoRoute(func(c *gin.Context) {
			requestPath := c.Request.URL.Path
			apiPath := joinAppPath(basePath, "/api")
			if requestPath == apiPath || strings.HasPrefix(requestPath, apiPath+"/") {
				c.Status(http.StatusNotFound)
				return
			}
			if basePath == "" || requestPath == basePath || strings.HasPrefix(requestPath, basePath+"/") {
				serveFrontendIndex(c, indexFile, basePath)
				return
			}
			c.Status(http.StatusNotFound)
		})
		log.Printf("Serving frontend static files from %s under base path %q", distDir, displayBasePath(basePath))
	} else {
		log.Printf("Warning: frontend dist directory %s not found, skipping static file serving", distDir)
	}

	// Determine port
	port := cfg.Server.Port
	if port == 0 {
		port = 8080
	}

	addr := fmt.Sprintf(":%d", port)
	log.Printf("Starting server on %s", addr)
	log.Printf("Config file: %s (supports dynamic reload via API)", configPath)
	if err := r.Run(addr); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

// Query strings and request headers can contain credentials and authorization codes.
func safeRequestLogger() gin.HandlerFunc {
	return func(c *gin.Context) {
		started := time.Now()
		c.Next()
		log.Printf("HTTP %s %s status=%d elapsed=%s", c.Request.Method, c.Request.URL.EscapedPath(), c.Writer.Status(), time.Since(started))
	}
}

func resolveBasePath(cfg *config.AppConfig) string {
	basePath := ""
	if cfg != nil {
		basePath = cfg.Server.BasePath
	}
	if envBasePath := os.Getenv("BASE_PATH"); envBasePath != "" {
		basePath = envBasePath
	}
	return normalizeBasePath(basePath)
}

func normalizeBasePath(basePath string) string {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" || basePath == "/" {
		return ""
	}
	return "/" + strings.Trim(basePath, "/")
}

func joinAppPath(basePath, path string) string {
	if basePath == "" {
		return path
	}
	return basePath + path
}

func displayBasePath(basePath string) string {
	if basePath == "" {
		return "/"
	}
	return basePath
}

func frontendAssetCacheHeaders(basePath string) gin.HandlerFunc {
	assetsPath := joinAppPath(basePath, "/assets") + "/"
	return func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, assetsPath) {
			c.Header("Cache-Control", "public, max-age=31536000, immutable")
		}
		c.Next()
	}
}

func serveFrontendIndex(c *gin.Context, indexFile string, basePaths ...string) {
	c.Header("Cache-Control", "no-store, no-cache, must-revalidate, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Expires", "0")
	content, err := os.ReadFile(indexFile)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	basePath := ""
	if len(basePaths) > 0 {
		basePath = basePaths[0]
	}
	// Relative Vite assets must resolve from the application root on deep links.
	markup := strings.Replace(string(content), "<head>", "<head><base href=\""+html.EscapeString(joinAppPath(basePath, "/"))+"\">", 1)
	c.Data(http.StatusOK, "text/html; charset=utf-8", []byte(markup))
}
