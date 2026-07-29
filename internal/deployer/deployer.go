package deployer

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"auto-deploy-platform/internal/config"

	"golang.org/x/crypto/ssh"
)

// Deployer defines the interface for deploying build artifacts to target servers via SSH.
type Deployer interface {
	// Upload transfers a local file to the target server's deploy path over SSH.
	Upload(ctx context.Context, localPath string, server config.ServerConfig) error

	// Execute runs a script on the target server via SSH and returns the stdout output.
	Execute(ctx context.Context, server config.ServerConfig, script string) (string, error)
}

// deployer is the concrete implementation of Deployer using golang.org/x/crypto/ssh.
type deployer struct{}

// NewDeployer creates a new Deployer instance.
func NewDeployer() Deployer {
	return &deployer{}
}

// newSSHClient creates an SSH client connection to the target server using password authentication.
func newSSHClient(server config.ServerConfig) (*ssh.Client, error) {
	sshConfig := &ssh.ClientConfig{
		User: server.User,
		Auth: []ssh.AuthMethod{
			ssh.Password(server.Password),
		},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         120 * time.Second,
	}

	addr := fmt.Sprintf("%s:%d", server.Host, server.Port)
	client, err := ssh.Dial("tcp", addr, sshConfig)
	if err != nil {
		return nil, fmt.Errorf("SSH connection to %s failed: %w", addr, err)
	}
	return client, nil
}

func closeOnCancel(ctx context.Context, client *ssh.Client, session *ssh.Session) func() {
	done := make(chan struct{})
	go func() {
		select {
		case <-ctx.Done():
			_ = session.Close()
			_ = client.Close()
		case <-done:
		}
	}()
	return func() {
		close(done)
	}
}

func waitSession(ctx context.Context, client *ssh.Client, session *ssh.Session) error {
	done := make(chan error, 1)
	go func() {
		done <- session.Wait()
	}()

	select {
	case err := <-done:
		return err
	case <-ctx.Done():
		_ = session.Close()
		_ = client.Close()
		return ctx.Err()
	}
}

// Upload streams localPath to an explicit temporary file on the target server,
// atomically moves it into place, and verifies the final file size.
func (d *deployer) Upload(ctx context.Context, localPath string, server config.ServerConfig) error {
	if err := ctx.Err(); err != nil {
		return err
	}

	fileInfo, err := os.Stat(localPath)
	if err != nil {
		return fmt.Errorf("failed to stat local file %s: %w", localPath, err)
	}
	if !fileInfo.Mode().IsRegular() {
		return fmt.Errorf("local artifact is not a regular file: %s", localPath)
	}

	file, err := os.Open(localPath)
	if err != nil {
		return fmt.Errorf("failed to open local file %s: %w", localPath, err)
	}
	defer file.Close()

	client, err := newSSHClient(server)
	if err != nil {
		return err
	}
	defer client.Close()

	remotePath := path.Join(server.DeployPath, filepath.Base(localPath))
	temporaryPath := remotePath + ".uploading"

	// Ensure the configured remote deploy directory exists.
	mkdirSession, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	mkdirStop := closeOnCancel(ctx, client, mkdirSession)
	if err := mkdirSession.Start("mkdir -p " + shellQuote(server.DeployPath)); err != nil {
		mkdirStop()
		mkdirSession.Close()
		return fmt.Errorf("failed to create remote deploy directory: %w", err)
	}
	if err := waitSession(ctx, client, mkdirSession); err != nil {
		mkdirStop()
		mkdirSession.Close()
		return fmt.Errorf("failed to create remote deploy directory: %w", err)
	}
	mkdirStop()
	mkdirSession.Close()

	// Stream to a temporary file, then atomically publish the final path.
	session, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create SSH session: %w", err)
	}
	var stderr bytes.Buffer
	session.Stdin = file
	session.Stderr = &stderr

	uploadCommand := fmt.Sprintf(
		"cat > %s && chmod 0644 %s && mv -f %s %s",
		shellQuote(temporaryPath),
		shellQuote(temporaryPath),
		shellQuote(temporaryPath),
		shellQuote(remotePath),
	)
	if err := session.Start(uploadCommand); err != nil {
		session.Close()
		return fmt.Errorf("failed to start remote upload to %s: %w", remotePath, err)
	}
	stop := closeOnCancel(ctx, client, session)
	if err := waitSession(ctx, client, session); err != nil {
		stop()
		session.Close()
		return fmt.Errorf("upload to %s failed: %s: %w", remotePath, stderr.String(), err)
	}
	stop()
	session.Close()

	// Do not report success until the exact final path exists with the expected size.
	verifySession, err := client.NewSession()
	if err != nil {
		return fmt.Errorf("failed to create upload verification session: %w", err)
	}
	var verifyStdout bytes.Buffer
	var verifyStderr bytes.Buffer
	verifySession.Stdout = &verifyStdout
	verifySession.Stderr = &verifyStderr
	verifyCommand := fmt.Sprintf("test -f %s && wc -c < %s", shellQuote(remotePath), shellQuote(remotePath))
	if err := verifySession.Start(verifyCommand); err != nil {
		verifySession.Close()
		return fmt.Errorf("failed to verify remote artifact %s: %w", remotePath, err)
	}
	verifyStop := closeOnCancel(ctx, client, verifySession)
	if err := waitSession(ctx, client, verifySession); err != nil {
		verifyStop()
		verifySession.Close()
		return fmt.Errorf("remote artifact verification failed for %s: %s: %w", remotePath, verifyStderr.String(), err)
	}
	verifyStop()
	verifySession.Close()

	remoteSize, err := strconv.ParseInt(strings.TrimSpace(verifyStdout.String()), 10, 64)
	if err != nil {
		return fmt.Errorf("invalid remote artifact size for %s: %q", remotePath, strings.TrimSpace(verifyStdout.String()))
	}
	if remoteSize != fileInfo.Size() {
		return fmt.Errorf("remote artifact size mismatch for %s: expected %d bytes, got %d bytes", remotePath, fileInfo.Size(), remoteSize)
	}
	return nil
}

func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}

// Execute runs the given script on the target server via SSH and returns stdout.
func (d *deployer) Execute(ctx context.Context, server config.ServerConfig, script string) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}

	client, err := newSSHClient(server)
	if err != nil {
		return "", err
	}
	defer client.Close()

	session, err := client.NewSession()
	if err != nil {
		return "", fmt.Errorf("failed to create SSH session: %w", err)
	}
	defer session.Close()

	var stdout bytes.Buffer
	var stderr bytes.Buffer
	session.Stdout = &stdout
	session.Stderr = &stderr

	if err := session.Start(script); err != nil {
		return "", fmt.Errorf("failed to start remote command: %w", err)
	}
	stop := closeOnCancel(ctx, client, session)
	defer stop()

	if err := waitSession(ctx, client, session); err != nil {
		// Check if it's a network error vs command error
		if _, ok := err.(*net.OpError); ok {
			return "", fmt.Errorf("SSH connection lost during execution: %w", err)
		}
		return "", fmt.Errorf("remote command failed: %s%s: %w", stdout.String(), stderr.String(), err)
	}

	return stdout.String(), nil
}
