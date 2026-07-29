package builder

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// initBareRepo creates a bare git repo with one commit on the given branch,
// returning the path to the bare repo.
func initBareRepo(t *testing.T, branch string) string {
	t.Helper()

	// Create a temporary working repo to build the initial commit.
	workDir := t.TempDir()
	runGit(t, workDir, "init", "-b", branch)
	runGit(t, workDir, "config", "user.email", "test@test.com")
	runGit(t, workDir, "config", "user.name", "Test")

	// Create a file and commit it.
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "README.md"), []byte("hello"), 0644))
	runGit(t, workDir, "add", ".")
	runGit(t, workDir, "commit", "-m", "initial commit")

	// Clone to a bare repo that can act as a remote.
	bareDir := t.TempDir()
	runGit(t, "", "clone", "--bare", workDir, bareDir)

	return bareDir
}

// runGit is a test helper that runs a git command and fails the test on error.
func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", args...)
	if dir != "" {
		cmd.Dir = dir
	}
	out, err := cmd.CombinedOutput()
	require.NoError(t, err, "git %v failed: %s", args, string(out))
}

func TestCloneOrPull_Clone(t *testing.T) {
	bareRepo := initBareRepo(t, "main")
	workDir := filepath.Join(t.TempDir(), "clone-target")

	b := NewBuilder()
	_, err := b.CloneOrPull(bareRepo, "main", "branch", "", workDir)
	require.NoError(t, err)

	// Verify the clone created the directory with the expected file.
	_, err = os.Stat(filepath.Join(workDir, "README.md"))
	assert.NoError(t, err, "README.md should exist after clone")
}

func TestCloneOrPull_Pull(t *testing.T) {
	bareRepo := initBareRepo(t, "main")
	workDir := filepath.Join(t.TempDir(), "pull-target")

	b := NewBuilder()

	// First clone.
	_, err := b.CloneOrPull(bareRepo, "main", "branch", "", workDir)
	require.NoError(t, err)

	// Push a new commit to the bare repo via a separate working copy.
	pushDir := t.TempDir()
	runGit(t, "", "clone", bareRepo, pushDir)
	runGit(t, pushDir, "config", "user.email", "test@test.com")
	runGit(t, pushDir, "config", "user.name", "Test")
	require.NoError(t, os.WriteFile(filepath.Join(pushDir, "new.txt"), []byte("new content"), 0644))
	runGit(t, pushDir, "add", ".")
	runGit(t, pushDir, "commit", "-m", "second commit")
	runGit(t, pushDir, "push", "origin", "main")

	// Pull should pick up the new commit.
	_, err = b.CloneOrPull(bareRepo, "main", "branch", "", workDir)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(workDir, "new.txt"))
	assert.NoError(t, err, "new.txt should exist after pull")
}

func TestCloneOrPull_ForceRefreshMovedTag(t *testing.T) {
	bareRepo := initBareRepo(t, "main")
	workDir := filepath.Join(t.TempDir(), "tag-target")
	tagName := "v0.0.0"

	pushDir := t.TempDir()
	runGit(t, "", "clone", bareRepo, pushDir)
	runGit(t, pushDir, "config", "user.email", "test@test.com")
	runGit(t, pushDir, "config", "user.name", "Test")
	runGit(t, pushDir, "tag", tagName)
	runGit(t, pushDir, "push", "origin", tagName)

	b := NewBuilder()
	_, err := b.CloneOrPull(bareRepo, "main", "branch", "", workDir)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(filepath.Join(pushDir, "moved.txt"), []byte("new tag target"), 0644))
	runGit(t, pushDir, "add", ".")
	runGit(t, pushDir, "commit", "-m", "move tag target")
	runGit(t, pushDir, "tag", "-f", tagName)
	runGit(t, pushDir, "push", "--force", "origin", tagName)

	_, err = b.CloneOrPull(bareRepo, tagName, "tag", "", workDir)
	require.NoError(t, err)

	_, err = os.Stat(filepath.Join(workDir, "moved.txt"))
	assert.NoError(t, err, "moved tag target should be checked out")
}

func TestCloneOrPull_InvalidRepo(t *testing.T) {
	workDir := filepath.Join(t.TempDir(), "bad-clone")

	b := NewBuilder()
	_, err := b.CloneOrPull("http://invalid-host-that-does-not-exist.local/repo.git", "main", "branch", "", workDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git clone failed")
}

func TestSanitizeGitOutputMasksCredentials(t *testing.T) {
	repoURL := "http://deploy-user:secret-token@gitea.example.com/admin/repo.git"
	output := "fatal: Authentication failed for 'http://deploy-user:secret-token@gitea.example.com/admin/repo.git'"

	sanitized := sanitizeGitOutput(output, repoURL)

	assert.NotContains(t, sanitized, "secret-token")
	assert.Contains(t, sanitized, "http://%2A%2A%2A:%2A%2A%2A@gitea.example.com/admin/repo.git")
}

func TestCloneOrPull_InvalidBranchOnPull(t *testing.T) {
	bareRepo := initBareRepo(t, "main")
	workDir := filepath.Join(t.TempDir(), "branch-test")

	b := NewBuilder()

	// Clone with valid branch first.
	_, err := b.CloneOrPull(bareRepo, "main", "branch", "", workDir)
	require.NoError(t, err)

	// Try to pull a non-existent branch.
	_, err = b.CloneOrPull(bareRepo, "nonexistent-branch", "branch", "", workDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "git checkout branch")
}

func TestCloneOrPull_VerifiesSelectedCommit(t *testing.T) {
	bareRepo := initBareRepo(t, "main")
	workDir := filepath.Join(t.TempDir(), "commit-check")

	b := NewBuilder()
	_, err := b.CloneOrPull(bareRepo, "main", "branch", "0000000000000000000000000000000000000000", workDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "selected commit mismatch")
}

func TestCloneOrPull_RejectsUnknownRefType(t *testing.T) {
	bareRepo := initBareRepo(t, "main")
	workDir := filepath.Join(t.TempDir(), "bad-ref-type")

	b := NewBuilder()
	_, err := b.CloneOrPull(bareRepo, "main", "unknown", "", workDir)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported ref type")
}

func TestBuild_Success(t *testing.T) {
	workDir := t.TempDir()

	b := NewBuilder()
	output, err := b.Build(context.Background(), workDir, "echo build-ok", "dev", nil)
	require.NoError(t, err)
	assert.Contains(t, output, "build-ok")
}

func TestBuild_SetsEnvVariable(t *testing.T) {
	workDir := t.TempDir()

	b := NewBuilder()
	command := "echo $BUILD_ENV"
	if runtime.GOOS == "windows" {
		command = "echo %BUILD_ENV%"
	}
	output, err := b.Build(context.Background(), workDir, command, "sit", nil)
	require.NoError(t, err)
	assert.Contains(t, strings.TrimSpace(output), "sit")
}

func TestBuild_Failure(t *testing.T) {
	workDir := t.TempDir()

	b := NewBuilder()
	_, err := b.Build(context.Background(), workDir, "exit 1", "dev", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build command failed")
}

func TestBuild_CapturesStderr(t *testing.T) {
	workDir := t.TempDir()

	b := NewBuilder()
	command := "echo error-output >&2; exit 1"
	if runtime.GOOS == "windows" {
		command = "echo error-output 1>&2 && exit /b 1"
	}
	output, err := b.Build(context.Background(), workDir, command, "prod", nil)
	require.Error(t, err)
	assert.Contains(t, output, "error-output")
}

func TestBuild_InvalidWorkDir(t *testing.T) {
	b := NewBuilder()
	_, err := b.Build(context.Background(), "/nonexistent-dir-xyz", "echo hello", "dev", nil)
	require.Error(t, err)
}

func TestBuild_StreamsOutput(t *testing.T) {
	workDir := t.TempDir()
	lines := []string{}

	b := NewBuilder()
	output, err := b.Build(context.Background(), workDir, "echo first && echo second", "dev", func(line string) {
		lines = append(lines, strings.TrimSpace(line))
	})
	require.NoError(t, err)
	assert.Contains(t, output, "first")
	assert.Contains(t, output, "second")
	assert.Contains(t, lines, "first")
	assert.Contains(t, lines, "second")
}

func TestBuild_ContextCanceled(t *testing.T) {
	workDir := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	b := NewBuilder()
	_, err := b.Build(ctx, workDir, "echo should-not-run", "prod", nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "build command canceled")
}

func TestCleanWorkDir_RemovesGeneratedFiles(t *testing.T) {
	bareRepo := initBareRepo(t, "main")
	workDir := filepath.Join(t.TempDir(), "clean-target")
	b := NewBuilder()
	_, err := b.CloneOrPull(bareRepo, "main", "branch", "", workDir)
	require.NoError(t, err)

	require.NoError(t, os.MkdirAll(filepath.Join(workDir, "node_modules"), 0755))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "node_modules", "package.js"), []byte("generated"), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(workDir, "artifact.tar.gz"), []byte("generated"), 0644))

	require.NoError(t, b.CleanWorkDir(workDir))
	_, err = os.Stat(filepath.Join(workDir, "node_modules"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(workDir, "artifact.tar.gz"))
	assert.True(t, os.IsNotExist(err))
	_, err = os.Stat(filepath.Join(workDir, "README.md"))
	assert.NoError(t, err)
	_, err = os.Stat(filepath.Join(workDir, ".git"))
	assert.NoError(t, err)
}
