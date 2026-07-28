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
	// CloneOrPull clones a repository if the workDir doesn't exist,
	// or fetches and checks out the specified branch if it does.
	CloneOrPull(repoURL, branch, workDir string) error

	// Build executes the given build command in workDir with BUILD_ENV set to env.
	// Returns the combined stdout/stderr output on success, or an error with output on failure.
	Build(ctx context.Context, workDir string, buildCmd string, env string, onOutput func(string)) (string, error)
	CleanWorkDir(workDir string) error
}

// builder is the concrete implementation of Builder using os/exec to run git and shell commands.
type builder struct{}

// NewBuilder creates a new Builder instance.
func NewBuilder() Builder {
	return &builder{}
}

// CloneOrPull clones the repository into workDir if it doesn't exist,
// or fetches and checks out the specified branch if workDir already exists.
func (b *builder) CloneOrPull(repoURL, branch, workDir string) error {
	if _, err := os.Stat(workDir); os.IsNotExist(err) {
		// Directory doesn't exist — clone the repo.
		return b.cloneRepo(repoURL, branch, workDir)
	}
	// Directory exists — fetch, checkout, and pull.
	return b.pullRepo(repoURL, branch, workDir)
}

// cloneRepo runs git clone for the specified branch into workDir.
func (b *builder) cloneRepo(repoURL, branch, workDir string) error {
	cmd := exec.Command("git", "clone", "--branch", branch, repoURL, workDir)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr

	if err := cmd.Run(); err != nil {
		return fmt.Errorf("git clone failed: %s: %w", sanitizeGitOutput(stderr.String(), repoURL), err)
	}
	return nil
}

// pullRepo fetches, checks out, and pulls the specified ref (branch or tag) inside workDir.
func (b *builder) pullRepo(repoURL, branch, workDir string) error {
	if repoURL != "" {
		remoteCmd := exec.Command("git", "remote", "set-url", "origin", repoURL)
		remoteCmd.Dir = workDir
		var remoteStderr bytes.Buffer
		remoteCmd.Stderr = &remoteStderr
		if err := remoteCmd.Run(); err != nil {
			return fmt.Errorf("git remote set-url failed: %s: %w", sanitizeGitOutput(remoteStderr.String(), repoURL), err)
		}
	}

	// Ensure the remote fetch refspec covers all branches and tags.
	configCmd := exec.Command("git", "config", "remote.origin.fetch", "+refs/heads/*:refs/remotes/origin/*")
	configCmd.Dir = workDir
	configCmd.Run() // ignore errors

	// git clean -fd — remove untracked files/dirs left by previous builds
	cleanCmd := exec.Command("git", "clean", "-fdx")
	cleanCmd.Dir = workDir
	cleanCmd.Run() // ignore errors

	// Force-refresh remote branches and tags so moved tags do not block deployments.
	fetchCmd := exec.Command("git", "fetch", "origin", "+refs/heads/*:refs/remotes/origin/*", "+refs/tags/*:refs/tags/*")
	fetchCmd.Dir = workDir
	var fetchStderr bytes.Buffer
	fetchCmd.Stderr = &fetchStderr
	if err := fetchCmd.Run(); err != nil {
		return fmt.Errorf("git fetch failed: %s: %w", sanitizeGitOutput(fetchStderr.String(), repoURL), err)
	}

	// Try checkout as branch first: git checkout -f -B <branch> origin/<branch>
	checkoutCmd := exec.Command("git", "checkout", "-f", "-B", branch, "origin/"+branch)
	checkoutCmd.Dir = workDir
	var checkoutStderr bytes.Buffer
	checkoutCmd.Stderr = &checkoutStderr
	if err := checkoutCmd.Run(); err != nil {
		// Branch checkout failed — try as tag: git checkout -f <tag>
		tagCmd := exec.Command("git", "checkout", "-f", branch)
		tagCmd.Dir = workDir
		var tagStderr bytes.Buffer
		tagCmd.Stderr = &tagStderr
		if tagErr := tagCmd.Run(); tagErr != nil {
			return fmt.Errorf("git checkout failed (not a branch or tag): %s: %w", sanitizeGitOutput(tagStderr.String(), repoURL), tagErr)
		}
		// Tag checkout succeeded, no pull needed for tags
		return nil
	}

	// git pull origin <branch>
	pullCmd := exec.Command("git", "pull", "origin", branch)
	pullCmd.Dir = workDir
	var pullStderr bytes.Buffer
	pullCmd.Stderr = &pullStderr
	if err := pullCmd.Run(); err != nil {
		return fmt.Errorf("git pull failed: %s: %w", sanitizeGitOutput(pullStderr.String(), repoURL), err)
	}

	return nil
}

// CleanWorkDir removes generated files from an existing Git checkout while
// retaining tracked source files and Git metadata for the next deployment.
func (b *builder) CleanWorkDir(workDir string) error {
	cmd := exec.Command("git", "clean", "-fdx")
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

	name, args := shellCommand(buildCmd)
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Dir = workDir
	cmd.Env = append(os.Environ(), fmt.Sprintf("BUILD_ENV=%s", env))

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
