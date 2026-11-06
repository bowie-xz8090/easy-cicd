package webproxy

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"sync"
	"time"

	"auto-deploy-platform/internal/config"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

// sshTransport opens only the registered service address from the SSH peer.
// No client request can select a different host, port, proxy or SSH account.
type sshTransport struct {
	mu         sync.Mutex
	client     *ssh.Client
	server     config.ServerConfig
	knownHosts string
	address    string
	http       *http.Transport
}

func newSSHTransport(server config.ServerConfig, knownHosts string, target *url.URL) *sshTransport {
	port := target.Port()
	if port == "" {
		port = "80"
		if target.Scheme == "https" {
			port = "443"
		}
	}
	t := &sshTransport{server: server, knownHosts: knownHosts, address: net.JoinHostPort(target.Hostname(), port)}
	t.http = &http.Transport{Proxy: nil, DialContext: t.dial, DisableCompression: true,
		ResponseHeaderTimeout: 30 * time.Second, TLSHandshakeTimeout: 10 * time.Second,
		IdleConnTimeout: 60 * time.Second, MaxIdleConnsPerHost: 8, MaxConnsPerHost: 32}
	return t
}

func (t *sshTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	return t.http.RoundTrip(request)
}

func (t *sshTransport) dial(ctx context.Context, network, address string) (net.Conn, error) {
	if network != "tcp" || address != t.address {
		return nil, errors.New("unregistered proxy address")
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	for attempt := 0; attempt < 2; attempt++ {
		t.mu.Lock()
		client := t.client
		if client == nil {
			var err error
			client, err = t.connect(ctx)
			if err != nil {
				t.mu.Unlock()
				return nil, err
			}
			t.client = client
		}
		t.mu.Unlock()
		channelContext, cancelChannel := context.WithTimeout(ctx, 12*time.Second)
		connection, err := client.DialContext(channelContext, "tcp", t.address)
		cancelChannel()
		if err == nil {
			return connection, nil
		}
		t.mu.Lock()
		if t.client == client {
			_ = t.client.Close()
			t.client = nil
		}
		t.mu.Unlock()
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, errors.New("SSH service connection unavailable")
}

func (t *sshTransport) connect(ctx context.Context) (*ssh.Client, error) {
	verify, err := knownhosts.New(t.knownHosts)
	if err != nil {
		return nil, errors.New("SSH host verification unavailable")
	}
	port := t.server.Port
	if port == 0 {
		port = 22
	}
	address := net.JoinHostPort(t.server.Host, strconv.Itoa(port))
	connection, err := (&net.Dialer{Timeout: 10 * time.Second}).DialContext(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	deadline := time.Now().Add(15 * time.Second)
	if bound, ok := ctx.Deadline(); ok && bound.Before(deadline) {
		deadline = bound
	}
	_ = connection.SetDeadline(deadline)
	stopCancellation := context.AfterFunc(ctx, func() { _ = connection.Close() })
	defer stopCancellation()
	clientConfig := &ssh.ClientConfig{User: t.server.User, Auth: []ssh.AuthMethod{ssh.Password(t.server.Password)}, HostKeyCallback: verify}
	if data, err := os.ReadFile(t.knownHosts); err == nil {
		seen := map[string]bool{}
		for len(data) > 0 {
			_, _, key, _, remaining, err := ssh.ParseKnownHosts(data)
			if err != nil {
				break
			}
			data = remaining
			if verify(address, connection.RemoteAddr(), key) != nil {
				continue
			}
			algorithms := []string{key.Type()}
			if key.Type() == ssh.KeyAlgoRSA {
				algorithms = []string{ssh.KeyAlgoRSASHA512, ssh.KeyAlgoRSASHA256}
			}
			for _, algorithm := range algorithms {
				if !seen[algorithm] {
					clientConfig.HostKeyAlgorithms = append(clientConfig.HostKeyAlgorithms, algorithm)
					seen[algorithm] = true
				}
			}
		}
	}
	clientConnection, channels, requests, err := ssh.NewClientConn(connection, address, clientConfig)
	if err != nil {
		_ = connection.Close()
		return nil, err
	}
	_ = connection.SetDeadline(time.Time{})
	return ssh.NewClient(clientConnection, channels, requests), nil
}

func (t *sshTransport) Close() {
	t.http.CloseIdleConnections()
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.client != nil {
		_ = t.client.Close()
		t.client = nil
	}
}
