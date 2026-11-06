package webproxy

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"auto-deploy-platform/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"
)

func TestSSHForwardingPinsHostKeyAndTarget(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	signer, err := ssh.NewSignerFromKey(private)
	require.NoError(t, err)
	serverConfig := &ssh.ServerConfig{PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
		if string(password) != "test-password" {
			return nil, errors.New("invalid login")
		}
		return nil, nil
	}}
	serverConfig.AddHostKey(signer)
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer listener.Close()
	forwarded := make(chan string, 4)
	go func() {
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				connection, channels, requests, err := ssh.NewServerConn(raw, serverConfig)
				if err != nil {
					_ = raw.Close()
					return
				}
				defer connection.Close()
				go ssh.DiscardRequests(requests)
				for request := range channels {
					if request.ChannelType() != "direct-tcpip" {
						_ = request.Reject(ssh.UnknownChannelType, "unsupported")
						continue
					}
					var destination struct {
						Host       string
						Port       uint32
						Origin     string
						OriginPort uint32
					}
					if ssh.Unmarshal(request.ExtraData(), &destination) != nil {
						continue
					}
					address := net.JoinHostPort(destination.Host, strconv.Itoa(int(destination.Port)))
					forwarded <- address
					socket, err := net.DialTimeout("tcp", address, time.Second)
					if err != nil {
						_ = request.Reject(ssh.ConnectionFailed, "unavailable")
						continue
					}
					channel, requests, err := request.Accept()
					if err != nil {
						_ = socket.Close()
						continue
					}
					go ssh.DiscardRequests(requests)
					go func() {
						defer socket.Close()
						defer channel.Close()
						go func() { _, _ = io.Copy(channel, socket); _ = channel.Close() }()
						_, _ = io.Copy(socket, channel)
					}()
				}
			}()
		}
	}()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = io.WriteString(w, "through-ssh") }))
	defer upstream.Close()
	target, err := url.Parse(upstream.URL)
	require.NoError(t, err)
	known := filepath.Join(t.TempDir(), "known_hosts")
	require.NoError(t, os.WriteFile(known, []byte(knownhosts.Line([]string{listener.Addr().String()}, signer.PublicKey())+"\n"), 0600))
	host, portText, _ := net.SplitHostPort(listener.Addr().String())
	port, _ := strconv.Atoi(portText)
	server := config.ServerConfig{Host: host, Port: port, User: "test", Password: "test-password"}
	transport := newSSHTransport(server, known, target)
	defer transport.Close()
	client := &http.Client{Transport: transport, Timeout: 5 * time.Second}
	response, err := client.Get(target.String())
	require.NoError(t, err)
	body, err := io.ReadAll(response.Body)
	_ = response.Body.Close()
	require.NoError(t, err)
	assert.Equal(t, "through-ssh", string(body))
	assert.Equal(t, target.Host, <-forwarded)
	_, err = transport.dial(context.Background(), "tcp", "unregistered.example:80")
	require.Error(t, err)
	_, wrongPrivate, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)
	wrong, err := ssh.NewSignerFromKey(wrongPrivate)
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(known, []byte(knownhosts.Line([]string{listener.Addr().String()}, wrong.PublicKey())+"\n"), 0600))
	bad := newSSHTransport(server, known, target)
	defer bad.Close()
	_, err = (&http.Client{Transport: bad, Timeout: 5 * time.Second}).Get(target.String())
	require.Error(t, err, "a changed SSH key must not be accepted")
	missing := newSSHTransport(server, known+".missing", target)
	defer missing.Close()
	_, err = (&http.Client{Transport: missing, Timeout: 5 * time.Second}).Get(target.String())
	require.Error(t, err)
}
