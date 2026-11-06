package builder

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var testRuntime = dependencyRuntime{Node: "node-22-linux-x64", Pnpm: "10.29.2", Config: `{"node-linker":"isolated"}`}

// 缓存测试只使用本地 Git 和极小依赖目录，避免访问包仓库或污染真实工作区。
func dependencyFixture(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main")
	runGit(t, dir, "config", "user.email", "test@test.com")
	runGit(t, dir, "config", "user.name", "Test")
	writeFixture(t, dir, "package.json", `{"name":"cache-test","private":true}`)
	writeFixture(t, dir, "pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters: {}\n")
	writeFixture(t, dir, "pnpm-workspace.yaml", "packages:\n  - apps/*\n")
	writeFixture(t, dir, ".gitignore", "node_modules/\ndist/\n")
	writeFixture(t, dir, "apps/site/package.json", `{"name":"site","private":true}`)
	writeFixture(t, dir, "apps/site/src.js", "export const text='latest';")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "fixture")
	return dir
}

func writeFixture(t *testing.T, dir, name, content string) {
	t.Helper()
	path := filepath.Join(dir, name)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0644))
}

func saveTestCache(t *testing.T, dir string) []byte {
	t.Helper()
	cache, err := prepareCacheWithRuntime(dir, "dev", testRuntime, nil, func(string) {})
	require.NoError(t, err)
	writeFixture(t, dir, "node_modules/dependency/index.js", "dependency")
	writeFixture(t, dir, "apps/site/node_modules/dependency/index.js", "dependency")
	require.NoError(t, cache.commit())
	data, err := os.ReadFile(dependencyStatePath(dir))
	require.NoError(t, err)
	return data
}

func TestDependencyCache_PreservesDependenciesButCleansOldCodeAndOutputs(t *testing.T) {
	dir := dependencyFixture(t)
	previous := saveTestCache(t, dir)
	writeFixture(t, dir, "apps/site/dist/old.js", "stale artifact")
	writeFixture(t, dir, "untracked-source.js", "stale source")
	writeFixture(t, dir, "node_modules/.vite/stale.js", "stale compiled cache")
	writeFixture(t, dir, "apps/site/node_modules/.cache/stale.js", "stale compiled cache")
	require.NoError(t, NewBuilder().CleanWorkDir(dir, WorkspaceOptions{PreserveDependencies: true}))
	assert.FileExists(t, filepath.Join(dir, "node_modules/dependency/index.js"))
	assert.NoFileExists(t, filepath.Join(dir, "untracked-source.js"))
	assert.NoDirExists(t, filepath.Join(dir, "apps/site/dist"))
	var logs []string
	_, err := prepareCacheWithRuntime(dir, "prod", testRuntime, previous, func(s string) { logs = append(logs, s) })
	require.NoError(t, err)
	assert.Contains(t, strings.Join(logs, ""), "缓存命中")
	assert.NoDirExists(t, filepath.Join(dir, "node_modules/.vite"))
	assert.NoDirExists(t, filepath.Join(dir, "apps/site/node_modules/.cache"))
	assert.FileExists(t, filepath.Join(dir, "node_modules/dependency/index.js"))
}

func TestDependencyCache_InvalidatesChangedInputs(t *testing.T) {
	for _, field := range []string{"lockfile", "manifest", "workspace", "node", "pnpm", "settings", "patch"} {
		t.Run(field, func(t *testing.T) {
			dir := dependencyFixture(t)
			previous := saveTestCache(t, dir)
			tools := testRuntime
			switch field {
			case "lockfile":
				writeFixture(t, dir, "pnpm-lock.yaml", "lockfileVersion: '9.0'\nimporters: {changed: {}}\n")
			case "manifest":
				writeFixture(t, dir, "apps/site/package.json", `{"name":"site","dependencies":{"new":"1.0.0"}}`)
			case "workspace":
				writeFixture(t, dir, "pnpm-workspace.yaml", "packages: [apps/*, packages/*]\n")
			case "node":
				tools.Node = "node-24-linux-x64"
			case "pnpm":
				tools.Pnpm = "10.30.0"
			case "settings":
				tools.Config = `{"node-linker":"hoisted"}`
			case "patch":
				writeFixture(t, dir, "patches/change.patch", "changed dependency")
				runGit(t, dir, "add", "patches/change.patch")
			}
			var logs []string
			_, err := prepareCacheWithRuntime(dir, "dev", tools, previous, func(s string) { logs = append(logs, s) })
			require.NoError(t, err)
			assert.Contains(t, strings.Join(logs, ""), "缓存未命中")
			assert.NoDirExists(t, filepath.Join(dir, "node_modules"))
			assert.NoDirExists(t, filepath.Join(dir, "apps/site/node_modules"))
		})
	}
}

func TestDependencyCache_BranchSwitchUsesNewSourceAndKeepsCompatibleDependencies(t *testing.T) {
	remote := dependencyFixture(t)
	bare := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, "", "clone", "--bare", remote, bare)
	dir := filepath.Join(t.TempDir(), "checkout")
	b := NewBuilder()
	options := WorkspaceOptions{PreserveDependencies: true}
	_, err := b.CloneOrPull(bare, "main", "branch", "", dir, options)
	require.NoError(t, err)
	previous := saveTestCache(t, dir)
	writeFixture(t, dir, "apps/site/src.js", "dirty old source")
	writeFixture(t, dir, "apps/site/dist/old.js", "old artifact")
	runGit(t, remote, "checkout", "-b", "next")
	writeFixture(t, remote, "apps/site/src.js", "new branch source")
	writeFixture(t, remote, "packages/shared/new.js", "new shared source")
	runGit(t, remote, "add", ".")
	runGit(t, remote, "commit", "-m", "update source")
	runGit(t, remote, "push", bare, "next")
	_, err = b.CloneOrPull(bare, "next", "branch", "", dir, options)
	require.NoError(t, err)
	source, err := os.ReadFile(filepath.Join(dir, "apps/site/src.js"))
	require.NoError(t, err)
	assert.Equal(t, "new branch source", string(source))
	assert.FileExists(t, filepath.Join(dir, "packages/shared/new.js"))
	assert.NoDirExists(t, filepath.Join(dir, "apps/site/dist"))
	assert.FileExists(t, filepath.Join(dir, "node_modules/dependency/index.js"))
	var logs []string
	_, err = prepareCacheWithRuntime(dir, "dev", testRuntime, previous, func(s string) { logs = append(logs, s) })
	require.NoError(t, err)
	assert.Contains(t, strings.Join(logs, ""), "缓存命中")
}

func TestDependencyCache_CustomInstallInvalidatesOnSourceCommitAndEnvironment(t *testing.T) {
	dir := dependencyFixture(t)
	writeFixture(t, dir, "package.json", `{"name":"cache-test","scripts":{"postinstall":"node setup.js"}}`)
	runGit(t, dir, "add", "package.json")
	runGit(t, dir, "commit", "-m", "install hook")
	a, err := dependencyFingerprint(dir, "dev", testRuntime)
	require.NoError(t, err)
	b, err := dependencyFingerprint(dir, "prod", testRuntime)
	require.NoError(t, err)
	assert.NotEqual(t, a, b)
	writeFixture(t, dir, "apps/site/src.js", "changed source")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "source update")
	c, err := dependencyFingerprint(dir, "dev", testRuntime)
	require.NoError(t, err)
	assert.NotEqual(t, a, c)
}

func TestDependencyCache_RejectsLockfileChangesDuringBuild(t *testing.T) {
	dir := dependencyFixture(t)
	cache, err := prepareCacheWithRuntime(dir, "dev", testRuntime, nil, func(string) {})
	require.NoError(t, err)
	writeFixture(t, dir, "pnpm-lock.yaml", "changed during build")
	require.ErrorContains(t, cache.commit(), "构建修改了依赖清单或锁文件")
	assert.False(t, hasDependencyCache(dir))
}

func TestDependencyCache_UnverifiedInstallationIsRemoved(t *testing.T) {
	dir := dependencyFixture(t)
	saveTestCache(t, dir)
	require.NoError(t, removeDependencyState(dir))
	writeFixture(t, dir, "node_modules/partial.js", "failed install")
	require.NoError(t, NewBuilder().CleanWorkDir(dir, WorkspaceOptions{PreserveDependencies: true}))
	assert.NoDirExists(t, filepath.Join(dir, "node_modules"))
	assert.NoDirExists(t, filepath.Join(dir, "apps/site/node_modules"))
}

func TestDependencyCache_NonPnpmBuildKeepsOriginalBehavior(t *testing.T) {
	cache, err := prepareDependencyCache(context.Background(), t.TempDir(), "mvn clean package", "dev", func(string) {})
	require.NoError(t, err)
	assert.Nil(t, cache)
}

func TestRemoveWorkspacePath_RejectsOtherProject(t *testing.T) {
	dir := t.TempDir()
	other := t.TempDir()
	writeFixture(t, other, "data.txt", "keep")
	require.Error(t, removeWorkspacePath(dir, other))
	require.Error(t, removeWorkspacePath(dir, dir))
	assert.FileExists(t, filepath.Join(other, "data.txt"))
}

func TestCloneOrPull_RejectsDeletedRemoteBranch(t *testing.T) {
	remote := dependencyFixture(t)
	runGit(t, remote, "branch", "obsolete")
	bare := filepath.Join(t.TempDir(), "remote.git")
	runGit(t, "", "clone", "--bare", remote, bare)
	dir := filepath.Join(t.TempDir(), "checkout")
	b := NewBuilder()
	_, err := b.CloneOrPull(bare, "obsolete", "branch", "", dir)
	require.NoError(t, err)
	runGit(t, remote, "push", bare, ":obsolete")
	_, err = b.CloneOrPull(bare, "obsolete", "branch", "", dir)
	require.Error(t, err)
}

func TestDependencyState_ContainsNoConfigurationValues(t *testing.T) {
	dir := dependencyFixture(t)
	state := saveTestCache(t, dir)
	var data map[string]any
	require.NoError(t, json.Unmarshal(state, &data))
	assert.Len(t, data, 2)
	assert.NotContains(t, string(state), testRuntime.Config)
	assert.NotContains(t, string(state), testRuntime.Node)
}

// 工具存在时验证真实环境信息读取；不运行依赖安装或业务构建。
func TestReadDependencyRuntime(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	if _, err := exec.LookPath("pnpm"); err != nil {
		t.Skip("pnpm unavailable")
	}
	tools, err := readDependencyRuntime(context.Background(), t.TempDir())
	require.NoError(t, err)
	assert.Contains(t, tools.Node, "version")
	assert.NotEmpty(t, tools.Pnpm)
	assert.NotEmpty(t, tools.Config)
}

func TestBuild_PnpmCacheRebuildsLatestWorkspaceSource(t *testing.T) {
	if _, err := exec.LookPath("node"); err != nil {
		t.Skip("node unavailable")
	}
	if _, err := exec.LookPath("pnpm"); err != nil {
		t.Skip("pnpm unavailable")
	}
	dir := dependencyFixture(t)
	writeFixture(t, dir, "pnpm-workspace.yaml", "packages:\n  - apps/*\n  - packages/*\n")
	writeFixture(t, dir, ".npmrc", "update-notifier=false\n")
	writeFixture(t, dir, "apps/site/package.json", `{"name":"site","private":true,"dependencies":{"@fixture/shared":"workspace:*"},"scripts":{"build:prod":"node build.cjs"}}`)
	writeFixture(t, dir, "packages/shared/package.json", `{"name":"@fixture/shared","version":"1.0.0","main":"index.js"}`)
	writeFixture(t, dir, "packages/shared/index.js", `module.exports='first-source'`)
	writeFixture(t, dir, "apps/site/build.cjs", `const fs=require('fs'); fs.mkdirSync('dist',{recursive:true}); fs.writeFileSync('dist/current.txt',require('@fixture/shared'));`)
	writeFixture(t, dir, "pnpm-lock.yaml", "lockfileVersion: '9.0'\nsettings:\n  autoInstallPeers: true\n  excludeLinksFromLockfile: false\nimporters:\n  .: {}\n  apps/site:\n    dependencies:\n      '@fixture/shared':\n        specifier: workspace:*\n        version: link:../../packages/shared\n  packages/shared: {}\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "local-only workspace")
	b := NewBuilder()
	command := "pnpm install --offline && pnpm --filter site build:prod"
	output, err := b.Build(context.Background(), dir, command, "prod", nil)
	require.NoError(t, err, output)
	assert.True(t, hasDependencyCache(dir))
	artifact := filepath.Join(dir, "apps/site/dist/current.txt")
	data, err := os.ReadFile(artifact)
	require.NoError(t, err)
	assert.Equal(t, "first-source", string(data))
	require.NoError(t, b.CleanWorkDir(dir, WorkspaceOptions{PreserveDependencies: true}))
	assert.NoFileExists(t, artifact)
	writeFixture(t, dir, "packages/shared/index.js", `module.exports='latest-source'`)
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-m", "update shared source only")
	output, err = b.Build(context.Background(), dir, command, "prod", nil)
	require.NoError(t, err, output)
	assert.Contains(t, output, "缓存命中")
	data, err = os.ReadFile(artifact)
	require.NoError(t, err)
	assert.Equal(t, "latest-source", string(data))
	// 锁文件不一致必须阻止编译和发布；已有安装结果不能掩盖缺失依赖。
	writeFixture(t, dir, "apps/site/package.json", `{"name":"site","private":true,"dependencies":{"@fixture/shared":"workspace:*","missing-package":"1.0.0"},"scripts":{"build:prod":"node build.cjs"}}`)
	require.NoError(t, b.CleanWorkDir(dir, WorkspaceOptions{PreserveDependencies: true}))
	output, err = b.Build(context.Background(), dir, command, "prod", nil)
	require.Error(t, err)
	assert.Contains(t, output, "ERR_PNPM_OUTDATED_LOCKFILE")
	assert.False(t, hasDependencyCache(dir))
	assert.NoFileExists(t, artifact)
}
