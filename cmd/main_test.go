package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestFrontendAssetCacheHeaders(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(frontendAssetCacheHeaders("/deploy"))
	router.GET("/deploy/assets/app.js", func(c *gin.Context) {
		c.String(http.StatusOK, "asset")
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/deploy/assets/app.js", nil)
	router.ServeHTTP(response, request)

	if got := response.Header().Get("Cache-Control"); got != "public, max-age=31536000, immutable" {
		t.Fatalf("unexpected asset Cache-Control header: %q", got)
	}
}

func TestFrontendAssetCacheHeadersIgnoreAPI(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(frontendAssetCacheHeaders(""))
	router.GET("/api/health", func(c *gin.Context) {
		c.Status(http.StatusOK)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/api/health", nil)
	router.ServeHTTP(response, request)

	if got := response.Header().Get("Cache-Control"); got != "" {
		t.Fatalf("API response must not receive frontend asset cache header: %q", got)
	}
}

func TestServeFrontendIndexDisablesCaching(t *testing.T) {
	gin.SetMode(gin.TestMode)
	indexFile := filepath.Join(t.TempDir(), "index.html")
	if err := os.WriteFile(indexFile, []byte("<!doctype html><title>app</title>"), 0o600); err != nil {
		t.Fatalf("write temporary index: %v", err)
	}

	router := gin.New()
	router.GET("/", func(c *gin.Context) {
		serveFrontendIndex(c, indexFile)
	})

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/", nil)
	router.ServeHTTP(response, request)

	if got := response.Header().Get("Cache-Control"); got != "no-store, no-cache, must-revalidate, max-age=0" {
		t.Fatalf("unexpected index Cache-Control header: %q", got)
	}
	if got := response.Header().Get("Pragma"); got != "no-cache" {
		t.Fatalf("unexpected index Pragma header: %q", got)
	}
	if got := response.Header().Get("Expires"); got != "0" {
		t.Fatalf("unexpected index Expires header: %q", got)
	}
}
