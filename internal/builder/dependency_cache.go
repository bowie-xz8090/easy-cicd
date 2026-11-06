package builder

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strings"
)

const dependencyCacheVersion = 1

var pnpmCommand = regexp.MustCompile(`(^|[\s&|;])pnpm(\s|$)`)

type dependencyRuntime struct {
	Node   string
	Pnpm   string
	Config string
}

type dependencyCacheState struct {
	Version int    `json:"version"`
	Key     string `json:"key"`
}

// dependencyCache 每个仓库仅保留一份安装结果，避免按分支无限增加磁盘占用。
// 元数据位于 .git 内，只保存摘要，不保存配置明文或打包产物。
type dependencyCache struct {
	workDir  string
	buildEnv string
	runtime  dependencyRuntime
	key      string
}

func dependencyStatePath(workDir string) string {
	return filepath.Join(workDir, ".git", "cicd-dependencies.json")
}

func hasDependencyCache(workDir string) bool {
	if _, err := os.Stat(filepath.Join(workDir, "pnpm-lock.yaml")); err != nil {
		return false
	}
	data, err := os.ReadFile(dependencyStatePath(workDir))
	var state dependencyCacheState
	return err == nil && json.Unmarshal(data, &state) == nil && state.Version == dependencyCacheVersion && len(state.Key) == 64
}

func prepareDependencyCache(ctx context.Context, workDir, command, env string, log func(string)) (*dependencyCache, error) {
	if !pnpmCommand.MatchString(command) {
		return nil, nil
	}
	if _, err := os.Stat(filepath.Join(workDir, "pnpm-lock.yaml")); os.IsNotExist(err) {
		return nil, nil
	} else if err != nil {
		return nil, err
	}
	// 准备、安装、编译任一步中断后都不留下有效标记，下次必须重新校验。
	previous, _ := os.ReadFile(dependencyStatePath(workDir))
	if err := removeDependencyState(workDir); err != nil {
		return nil, err
	}
	tools, err := readDependencyRuntime(ctx, workDir)
	if err != nil {
		return nil, err
	}
	return prepareCacheWithRuntime(workDir, env, tools, previous, log)
}

func prepareCacheWithRuntime(workDir, env string, tools dependencyRuntime, previous []byte, log func(string)) (*dependencyCache, error) {
	key, err := dependencyFingerprint(workDir, env, tools)
	if err != nil {
		return nil, err
	}
	var state dependencyCacheState
	hit := json.Unmarshal(previous, &state) == nil && state.Version == dependencyCacheVersion && state.Key == key
	modules, err := dependencyDirectories(workDir)
	if err != nil {
		return nil, err
	}
	if len(modules) == 0 {
		hit = false
	}
	for _, dir := range modules {
		info, err := os.Lstat(dir)
		if err != nil {
			return nil, err
		}
		if !hit || info.Mode()&os.ModeSymlink != 0 {
			if err := removeWorkspacePath(workDir, dir); err != nil {
				return nil, err
			}
			continue
		}
		// 工具自己的编译缓存可能依赖源码或环境，不能随第三方依赖一起复用。
		for _, name := range []string{".vite", ".vite-temp", ".cache"} {
			if err := removeWorkspacePath(workDir, filepath.Join(dir, name)); err != nil {
				return nil, err
			}
		}
	}
	if hit {
		log("依赖缓存命中；仍执行锁文件校验、安装检查和完整编译")
	} else {
		log("依赖缓存未命中或输入已变化；重新安装，复用 pnpm 下载缓存")
	}
	return &dependencyCache{workDir: workDir, buildEnv: env, runtime: tools, key: key}, nil
}

func readDependencyRuntime(ctx context.Context, workDir string) (dependencyRuntime, error) {
	run := func(name string, args ...string) (string, error) {
		cmd := exec.CommandContext(ctx, name, args...)
		cmd.Dir = workDir
		cmd.Env = setEnv(os.Environ(), "CI", "true")
		cmd.Env = setEnv(cmd.Env, "npm_config_frozen_lockfile", "true")
		cmd.Env = setEnv(cmd.Env, "npm_config_prefer_offline", "true")
		out, err := cmd.Output()
		if err != nil {
			// 工具错误可能含 registry 凭据，只返回步骤与退出错误。
			return "", fmt.Errorf("读取依赖工具信息失败 (%s): %w", name, err)
		}
		return strings.TrimSpace(string(out)), nil
	}
	node, err := run("node", "-p", `JSON.stringify({version:process.version,platform:process.platform,arch:process.arch,abi:process.versions.modules,glibc:process.report.getReport().header.glibcVersionRuntime})`)
	if err != nil {
		return dependencyRuntime{}, err
	}
	name, args := shellCommand("pnpm --version")
	pnpm, err := run(name, args...)
	if err != nil {
		return dependencyRuntime{}, err
	}
	name, args = shellCommand("pnpm config list --json")
	settings, err := run(name, args...)
	if err != nil {
		return dependencyRuntime{}, err
	}
	var values map[string]any
	if err := json.Unmarshal([]byte(settings), &values); err != nil {
		return dependencyRuntime{}, fmt.Errorf("无法解析 pnpm 配置")
	}
	canonical, _ := json.Marshal(values)
	return dependencyRuntime{Node: node, Pnpm: pnpm, Config: string(canonical)}, nil
}

func dependencyFingerprint(workDir, env string, tools dependencyRuntime) (string, error) {
	cmd := exec.Command("git", "ls-files", "-z")
	cmd.Dir = workDir
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("读取依赖清单失败: %w", err)
	}
	files := strings.Split(strings.TrimRight(string(out), "\x00"), "\x00")
	sort.Strings(files)
	hash := sha256.New()
	fmt.Fprintf(hash, "%d\x00%s\x00%s\x00%+v\x00", dependencyCacheVersion, runtime.GOOS, runtime.GOARCH, tools)
	customInstall := false
	for _, name := range files {
		base := filepath.Base(name)
		if base != "package.json" && base != "pnpm-lock.yaml" && base != "pnpm-workspace.yaml" && base != ".npmrc" && base != ".pnpmfile.cjs" && base != ".pnpmfile.mjs" && !strings.HasSuffix(name, ".patch") {
			continue
		}
		data, err := os.ReadFile(filepath.Join(workDir, name))
		if err != nil {
			return "", fmt.Errorf("读取依赖文件失败 (%s): %w", name, err)
		}
		fmt.Fprintf(hash, "%s\x00%d\x00", name, len(data))
		_, _ = hash.Write(data)
		if base == "package.json" {
			var manifest struct {
				Scripts              map[string]string `json:"scripts"`
				Dependencies         map[string]string `json:"dependencies"`
				DevDependencies      map[string]string `json:"devDependencies"`
				OptionalDependencies map[string]string `json:"optionalDependencies"`
			}
			if json.Unmarshal(data, &manifest) != nil {
				return "", fmt.Errorf("依赖清单不是有效 JSON: %s", name)
			}
			for _, hook := range []string{"preinstall", "install", "postinstall", "prepare"} {
				customInstall = customInstall || manifest.Scripts[hook] != ""
			}
			for _, dependencies := range []map[string]string{manifest.Dependencies, manifest.DevDependencies, manifest.OptionalDependencies} {
				for _, version := range dependencies {
					customInstall = customInstall || strings.HasPrefix(version, "file:") || strings.HasPrefix(version, "link:")
				}
			}
		}
		customInstall = customInstall || strings.HasPrefix(base, ".pnpmfile.")
	}
	if customInstall {
		// 本地文件依赖或安装钩子可能消费任意源码；此时保守地按提交和环境失效。
		cmd := exec.Command("git", "rev-parse", "HEAD")
		cmd.Dir = workDir
		commit, err := cmd.Output()
		if err != nil {
			return "", err
		}
		fmt.Fprintf(hash, "\x00%s\x00%s", commit, env)
	}
	return hex.EncodeToString(hash.Sum(nil)), nil
}

func dependencyDirectories(workDir string) ([]string, error) {
	var directories []string
	err := filepath.WalkDir(workDir, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.Name() == ".git" && entry.IsDir() {
			return filepath.SkipDir
		}
		if entry.Name() == "node_modules" {
			directories = append(directories, path)
			if entry.IsDir() {
				return filepath.SkipDir
			}
		}
		return nil
	})
	return directories, err
}

func removeWorkspacePath(workDir, target string) error {
	root, err := filepath.Abs(workDir)
	if err != nil {
		return err
	}
	abs, err := filepath.Abs(target)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return fmt.Errorf("拒绝清理工作区以外的路径")
	}
	// 不沿中间目录的符号链接清理，避免缓存路径逃逸到其他项目。
	for parent := filepath.Dir(abs); parent != root; parent = filepath.Dir(parent) {
		info, err := os.Lstat(parent)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("拒绝沿符号链接清理缓存")
		}
	}
	return os.RemoveAll(abs)
}

func removeDependencyState(workDir string) error {
	err := os.Remove(dependencyStatePath(workDir))
	if os.IsNotExist(err) {
		return nil
	}
	return err
}

func (c *dependencyCache) commit() error {
	key, err := dependencyFingerprint(c.workDir, c.buildEnv, c.runtime)
	if err != nil {
		return err
	}
	if key != c.key {
		return fmt.Errorf("构建修改了依赖清单或锁文件，请提交一致的依赖配置后重试")
	}
	data, _ := json.Marshal(dependencyCacheState{Version: dependencyCacheVersion, Key: key})
	file, err := os.CreateTemp(filepath.Join(c.workDir, ".git"), "cicd-dependencies-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	if _, err := file.Write(data); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	return os.Rename(file.Name(), dependencyStatePath(c.workDir))
}

func setEnv(values []string, key, value string) []string {
	result := make([]string, 0, len(values)+1)
	for _, item := range values {
		name, _, _ := strings.Cut(item, "=")
		if name == key || (runtime.GOOS == "windows" && strings.EqualFold(name, key)) {
			continue
		}
		result = append(result, item)
	}
	return append(result, key+"="+value)
}
