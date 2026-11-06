package builder

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"sync"
)

// Builder defines the interface for code fetching and project building.
type Builder interface {
	// CloneOrPull clones or refreshes a repository, checks out the exact branch
	// or tag requested by the user, and verifies the selected commit when set.
	CloneOrPull(repoURL, ref, refType, expectedCommit, workDir string, options ...WorkspaceOptions) (string, error)

	// Build executes the given build command in workDir with BUILD_ENV set to env.
	// Returns the combined stdout/stderr output on success, or an error with output on failure.
	Build(ctx context.Context, workDir string, buildCmd string, env string, onOutput func(string)) (string, error)
	CleanWorkDir(workDir string, options ...WorkspaceOptions) error
}

// WorkspaceOptions 只允许保留已验证的依赖目录，源码与构建产物始终清理。
type WorkspaceOptions struct {
	PreserveDependencies bool
}

// builder is the concrete implementation of Builder using os/exec to run git and shell commands.
type builder struct{}

// NewBuilder creates a new Builder instance.
func NewBuilder() Builder {
	return &builder{}
}

// CloneOrPull clones the repository into workDir if it doesn't exist,
// or fetches and checks out the specified branch if workDir already exists.
func (b *builder) CloneOrPull(repoURL, ref, refType, expectedCommit, workDir string, options ...WorkspaceOptions) (string, error) {
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		if err := b.cloneRepo(repoURL, workDir); err != nil {
			return "", err
		}
	}
	return b.checkoutRef(repoURL, ref, refType, expectedCommit, workDir, options...)
}

// cloneRepo creates the checkout without selecting a potentially ambiguous ref.
func (b *builder) cloneRepo(repoURL, workDir string) error {
	cmd := exec.Command("git", "clone", "--no-checkout", repoURL, workDir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %s: %w", sanitizeGitOutput(stderr.String(), repoURL), err)
	}
	return nil
}

// checkoutRef fetches and checks out an explicitly typed branch or tag.
func (b *builder) checkoutRef(repoURL, ref, refType, expectedCommit, workDir string, options ...WorkspaceOptions) (string, error) {
	if repoURL != "" {
		remoteCmd := exec.Command("git", "remote", "set-url", "origin", repoURL)
		remoteCmd.Dir = workDir
		var remoteStderr bytes.Buffer
		remoteCmd.Stderr = &remoteStderr
		if err := remoteCmd.Run(); err != nil {
			return "", fmt.Errorf("git remote set-url failed: %s: %w", sanitizeGitOutput(remoteStderr.String(), repoURL), err)
		}
	}

	// Ensure the remote fetch refspec covers all branches and tags.
	configCmd := exec.Command("git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	configCmd.Dir = workDir
	if err := configCmd.Run(); err != nil {
		return "", fmt.Errorf("git fetch configuration failed: %w", err)
	}

	if err := b.CleanWorkDir(workDir, options...); err != nil {
		return "", err
	}

	// Force-refresh remote branches and tags so moved tags do not block deployments.
	// 清除远端已删除的引用，避免把已删除分支的旧代码误当作最新版本。
	fetchCmd := exec.Command("git", "fetch", "--prune", "origin", "+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*")
	fetchCmd.Dir = workDir
	var fetchStderr bytes.Buffer
	fetchCmd.Stderr = &fetchStderr
	if err := fetchCmd.Run(); err != nil {
		return "", fmt.Errorf("git fetch failed: %s: %w", sanitizeGitOutput(fetchStderr.String(), repoURL), err)
	}

	var target string
	switch refType {
	case "branch", "":
		target = "refs/remotes/origin/" + ref
	case "tag":
		target = "refs/tags/" + ref
	default:
		return "", fmt.Errorf("unsupported ref type %q", refType)
	}

	// A detached checkout avoids local branch state and makes branch/tag names
	// unambiguous because target is always a fully-qualified Git ref.
	checkoutCmd := exec.Command("git", "checkout", "-f", "--detach", target)
	checkoutCmd.Dir = workDir
	var checkoutStderr bytes.Buffer
	checkoutCmd.Stderr = &checkoutStderr
	if err := checkoutCmd.Run(); err != nil {
		return "", fmt.Errorf("git checkout %s %q failed: %s: %w", refType, ref, sanitizeGitOutput(checkoutStderr.String(), repoURL), err)
	}

	headCmd := exec.Command("git", "rev-parse", "HEAD")
	headCmd.Dir = workDir
	headOutput, err := headCmd.Output()
	if err != nil {
		return "", fmt.Errorf("git rev-parse HEAD failed: %w", err)
	}
	actualCommit := strings.TrimSpace(string(headOutput))

	expectedCommit = strings.TrimSpace(expectedCommit)
	if expectedCommit != "" &&
		!strings.HasPrefix(actualCommit, expectedCommit) &&
		!strings.HasPrefix(expectedCommit, actualCommit) {
		return "", fmt.Errorf(
			"selected commit mismatch: expected %s, checked out %s",
			expectedCommit,
			actualCommit,
		)
	}

	return actualCommit, nil
}

// CleanWorkDir removes generated files from an existing Git checkout while
// retaining tracked source files and Git metadata for the next deployment.
func (b *builder) CleanWorkDir(workDir string, options ...WorkspaceOptions) error {
	args := []string{"clean", "-fdx"}
	if len(options) > 0 && options[0].PreserveDependencies && hasDependencyCache(workDir) {
		// 仅保留 node_modules；旧 dist、临时源码和项目级编译缓存全部删除。
		args = append(args, "-e", "node_modules/")
	}
	cmd := exec.Command("git", args...)
	cmd.Dir = workDir
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clean failed: %s: %w", stderr.String(), err)
	}
	return nil
}

func sanitizeGitOutput(output, repoURL string) string {
	if repoURL == "" || output == "" {
		return output
	}

	sanitizedURL := sanitizeURL(repoURL)
	output = strings.ReplaceAll(output, repoURL, sanitizedURL)

	parsed, err := url.Parse(repoURL)
	if err != nil || parsed.User == nil {
		return output
	}
	if password, ok := parsed.User.Password(); ok && password != "" {
		output = strings.ReplaceAll(output, password, "***")
	}
	if username := parsed.User.Username(); username != "" {
		output = strings.ReplaceAll(output, username+":"+"***", "***:***")
	}
	return output
}

func sanitizeURL(rawURL string) string {
	parsed, err := url.Parse(rawURL)
	if err != nil || parsed.User == nil {
		return rawURL
	}
	parsed.User = url.UserPassword("***", "***")
	return parsed.String()
}

// Build executes the build command in workDir with BUILD_ENV set to the given env value.
// It returns the combined output on success, or an error containing the output on failure.
func (b *builder) Build(ctx context.Context, workDir string, buildCmd string, env string, onOutput func(string)) (string, error) {
	if ctx.Err() != nil {
		return "", fmt.Errorf("build command canceled: %w", ctx.Err())
	}

	var output bytes.Buffer
	var outputMu sync.Mutex
	appendOutput := func(line string) {
		outputMu.Lock()
		defer outputMu.Unlock()
		output.WriteString(line)
		output.WriteByte('\n')
		if onOutput != nil {
			onOutput(line)
		}
	}

	cache, err := prepareDependencyCache(ctx, workDir, buildCmd, env, appendOutput)
	if err != nil {
		return output.String(), fmt.Errorf("dependency cache preparation failed: %w", err)
	}
	name, args := shellCommand(buildCmd)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), fmt.Sprintf("BUILD_ENV=%s", env))
	if cache != nil {
		// pnpm 在 CI 中校验锁文件，仍执行项目原有安装命令及所有生命周期脚本。
		cmd.Env = setEnv(cmd.Env, "CI", "true")
		cmd.Env = setEnv(cmd.Env, "npm_config_frozen_lockfile", "true")
		cmd.Env = setEnv(cmd.Env, "npm_config_prefer_offline", "true")
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", fmt.Errorf("failed to capture build stdout: %w", err)
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		return "", fmt.Errorf("failed to capture build stderr: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("build command failed to start: %w", err)
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go scanOutput(stdout, appendOutput, &wg)
	go scanOutput(stderr, appendOutput, &wg)

	err = cmd.Wait()
	wg.Wait()

	outputMu.Lock()
	outputText := output.String()
	outputMu.Unlock()

	if err != nil {
		if ctx.Err() != nil {
			return outputText, fmt.Errorf("build command canceled: %w", ctx.Err())
		}
		return outputText, fmt.Errorf("build command failed: %w", err)
	}
	if cache != nil {
		if err := cache.commit(); err != nil {
			return outputText, fmt.Errorf("dependency cache validation failed: %w", err)
		}
	}

	return outputText, nil
}

func scanOutput(reader io.Reader, onLine func(string), wg *sync.WaitGroup) {
	defer wg.Done()
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line != "" {
			onLine(line)
		}
	}
}

func shellCommand(command string) (string, []string) {
	if runtime.GOOS == "windows" {
		return "cmd", []string{"/C", command}
	}
	return "sh", []string{"-c", command}
}
