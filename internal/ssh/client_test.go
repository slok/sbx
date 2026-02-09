package ssh

import (
	"bytes"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/ssh"

	"github.com/slok/sbx/internal/log"
)

// testSSHServer is an in-process SSH server for testing.
type testSSHServer struct {
	listener net.Listener
	config   *ssh.ServerConfig
	addr     string
	wg       sync.WaitGroup
	done     chan struct{}
}

func newTestSSHServer(t *testing.T, privKeyBytes []byte) *testSSHServer {
	t.Helper()

	config := &ssh.ServerConfig{
		PublicKeyCallback: func(conn ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			// Accept any key — we're testing client behavior, not auth.
			return nil, nil
		},
	}

	signer, err := ssh.ParsePrivateKey(privKeyBytes)
	require.NoError(t, err)
	config.AddHostKey(signer)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)

	s := &testSSHServer{
		listener: listener,
		config:   config,
		addr:     listener.Addr().String(),
		done:     make(chan struct{}),
	}

	s.wg.Add(1)
	go s.serve(t)

	return s
}

func (s *testSSHServer) serve(t *testing.T) {
	t.Helper()
	defer s.wg.Done()

	for {
		conn, err := s.listener.Accept()
		if err != nil {
			select {
			case <-s.done:
				return
			default:
				// Listener was closed.
				return
			}
		}

		go s.handleConn(t, conn)
	}
}

func (s *testSSHServer) handleConn(t *testing.T, netConn net.Conn) {
	t.Helper()

	sshConn, chans, reqs, err := ssh.NewServerConn(netConn, s.config)
	if err != nil {
		return
	}
	defer sshConn.Close()

	go ssh.DiscardRequests(reqs)

	for newChannel := range chans {
		switch newChannel.ChannelType() {
		case "session":
			go s.handleSession(t, newChannel)
		case "direct-tcpip":
			go s.handleDirectTCPIP(t, newChannel)
		default:
			_ = newChannel.Reject(ssh.UnknownChannelType, "unknown channel type")
		}
	}
}

func (s *testSSHServer) handleSession(t *testing.T, newChannel ssh.NewChannel) {
	t.Helper()

	channel, requests, err := newChannel.Accept()
	if err != nil {
		return
	}
	defer channel.Close()

	for req := range requests {
		switch req.Type {
		case "exec":
			// Parse the command from the request payload.
			// The payload format is: uint32 length + string command.
			if len(req.Payload) < 4 {
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
				continue
			}
			cmdLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
			if len(req.Payload) < 4+cmdLen {
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
				continue
			}
			command := string(req.Payload[4 : 4+cmdLen])

			if req.WantReply {
				_ = req.Reply(true, nil)
			}

			// Execute the command.
			cmd := exec.Command("sh", "-c", command)
			cmd.Stdin = channel
			cmd.Stdout = channel
			cmd.Stderr = channel.Stderr()

			exitCode := 0
			if err := cmd.Run(); err != nil {
				if exitErr, ok := err.(*exec.ExitError); ok {
					exitCode = exitErr.ExitCode()
				} else {
					exitCode = 1
				}
			}

			// Send exit status.
			exitPayload := []byte{0, 0, 0, 0}
			exitPayload[0] = byte(exitCode >> 24)
			exitPayload[1] = byte(exitCode >> 16)
			exitPayload[2] = byte(exitCode >> 8)
			exitPayload[3] = byte(exitCode)
			_, _ = channel.SendRequest("exit-status", false, exitPayload)
			return

		case "subsystem":
			// Parse subsystem name.
			if len(req.Payload) < 4 {
				if req.WantReply {
					_ = req.Reply(false, nil)
				}
				continue
			}
			nameLen := int(req.Payload[0])<<24 | int(req.Payload[1])<<16 | int(req.Payload[2])<<8 | int(req.Payload[3])
			subsystem := string(req.Payload[4 : 4+nameLen])

			if subsystem == "sftp" {
				if req.WantReply {
					_ = req.Reply(true, nil)
				}
				server, err := sftp.NewServer(channel)
				if err != nil {
					return
				}
				_ = server.Serve()
				return
			}

			if req.WantReply {
				_ = req.Reply(false, nil)
			}

		default:
			if req.WantReply {
				_ = req.Reply(false, nil)
			}
		}
	}
}

// handleDirectTCPIP handles SSH direct-tcpip channel requests (for port forwarding).
func (s *testSSHServer) handleDirectTCPIP(t *testing.T, newChannel ssh.NewChannel) {
	t.Helper()

	// Parse the direct-tcpip extra data to get the target address.
	type directTCPIPData struct {
		DestHost   string
		DestPort   uint32
		OriginHost string
		OriginPort uint32
	}
	var data directTCPIPData
	if err := ssh.Unmarshal(newChannel.ExtraData(), &data); err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, "failed to parse direct-tcpip data")
		return
	}

	destAddr := net.JoinHostPort(data.DestHost, fmt.Sprintf("%d", data.DestPort))

	// Connect to the target.
	targetConn, err := net.DialTimeout("tcp", destAddr, 5*time.Second)
	if err != nil {
		_ = newChannel.Reject(ssh.ConnectionFailed, fmt.Sprintf("failed to connect to %s", destAddr))
		return
	}
	defer targetConn.Close()

	channel, _, err := newChannel.Accept()
	if err != nil {
		return
	}
	defer channel.Close()

	// Bidirectional copy.
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		_, _ = io.Copy(channel, targetConn)
	}()
	go func() {
		defer wg.Done()
		_, _ = io.Copy(targetConn, channel)
	}()
	wg.Wait()
}

func (s *testSSHServer) close() {
	close(s.done)
	s.listener.Close()
	s.wg.Wait()
}

// testParsePort extracts host and port from a net.Listener address.
func testParseHostPort(t *testing.T, addr string) (string, int) {
	t.Helper()
	host, portStr, err := net.SplitHostPort(addr)
	require.NoError(t, err)
	port, err := strconv.Atoi(portStr)
	require.NoError(t, err)
	return host, port
}

// generateTestKeyPair generates an Ed25519 key pair and returns PEM-encoded private key bytes.
func generateTestKeyPair(t *testing.T) []byte {
	t.Helper()

	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	pemBlock, err := ssh.MarshalPrivateKey(privKey, "test-key")
	require.NoError(t, err)

	return pem.EncodeToMemory(pemBlock)
}

func TestClient_NewClient(t *testing.T) {
	privKey := generateTestKeyPair(t)
	server := newTestSSHServer(t, privKey)
	defer server.close()

	host, port := testParseHostPort(t, server.addr)

	tests := map[string]struct {
		cfg    ClientConfig
		expErr bool
	}{
		"Valid config should connect successfully.": {
			cfg: ClientConfig{
				Host:       host,
				Port:       port,
				User:       "root",
				PrivateKey: privKey,
				Logger:     log.Noop,
			},
		},

		"Missing host should fail.": {
			cfg: ClientConfig{
				User:       "root",
				PrivateKey: privKey,
			},
			expErr: true,
		},

		"Missing user should fail.": {
			cfg: ClientConfig{
				Host:       host,
				Port:       port,
				PrivateKey: privKey,
			},
			expErr: true,
		},

		"Missing private key should fail.": {
			cfg: ClientConfig{
				Host: host,
				Port: port,
				User: "root",
			},
			expErr: true,
		},

		"Invalid private key should fail.": {
			cfg: ClientConfig{
				Host:       host,
				Port:       port,
				User:       "root",
				PrivateKey: []byte("not-a-key"),
			},
			expErr: true,
		},

		"Connection to non-existent host should fail.": {
			cfg: ClientConfig{
				Host:           "192.0.2.1", // RFC 5737 TEST-NET, guaranteed unreachable.
				Port:           22,
				User:           "root",
				PrivateKey:     privKey,
				ConnectTimeout: 1 * time.Second,
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			client, err := NewClient(ctx, test.cfg)
			if test.expErr {
				assert.Error(t, err)
				return
			}

			require.NoError(t, err)
			assert.NotNil(t, client)
			assert.NoError(t, client.Close())
		})
	}
}

func TestClient_Exec(t *testing.T) {
	privKey := generateTestKeyPair(t)
	server := newTestSSHServer(t, privKey)
	defer server.close()

	host, port := testParseHostPort(t, server.addr)

	tests := map[string]struct {
		command     string
		opts        ExecOpts
		expExitCode int
		expStdout   string
		expErr      bool
	}{
		"Simple echo should return exit code 0 and output.": {
			command:     "echo hello world",
			expExitCode: 0,
			expStdout:   "hello world\n",
		},

		"Failed command should return non-zero exit code.": {
			command:     "exit 42",
			expExitCode: 42,
		},

		"Command with stderr should capture stderr.": {
			command:     "echo error >&2",
			expExitCode: 0,
		},

		"Command reading stdin should work.": {
			command:     "cat",
			opts:        ExecOpts{Stdin: strings.NewReader("from stdin")},
			expExitCode: 0,
			expStdout:   "from stdin",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			client, err := NewClient(ctx, ClientConfig{
				Host:       host,
				Port:       port,
				User:       "root",
				PrivateKey: privKey,
				Logger:     log.Noop,
			})
			require.NoError(err)
			defer client.Close()

			var stdout bytes.Buffer
			opts := test.opts
			if opts.Stdout == nil {
				opts.Stdout = &stdout
			}

			exitCode, err := client.Exec(ctx, test.command, opts)
			if test.expErr {
				assert.Error(err)
				return
			}

			assert.NoError(err)
			assert.Equal(test.expExitCode, exitCode)

			if test.expStdout != "" {
				assert.Equal(test.expStdout, stdout.String())
			}
		})
	}
}

func TestClient_Exec_ContextCancellation(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	privKey := generateTestKeyPair(t)
	server := newTestSSHServer(t, privKey)
	defer server.close()

	host, port := testParseHostPort(t, server.addr)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Host:       host,
		Port:       port,
		User:       "root",
		PrivateKey: privKey,
		Logger:     log.Noop,
	})
	require.NoError(err)
	defer client.Close()

	// Cancel context immediately to test cancellation.
	cancelCtx, cancelFunc := context.WithCancel(context.Background())
	cancelFunc()

	_, err = client.Exec(cancelCtx, "sleep 60", ExecOpts{})
	assert.Error(err)
	assert.ErrorIs(err, context.Canceled)
}

func TestClient_CopyTo(t *testing.T) {
	privKey := generateTestKeyPair(t)
	server := newTestSSHServer(t, privKey)
	defer server.close()

	host, port := testParseHostPort(t, server.addr)

	tests := map[string]struct {
		setup    func(t *testing.T) (srcLocal, dstRemote string, cleanup func())
		expErr   bool
		validate func(t *testing.T, dstRemote string)
	}{
		"Copy single file should work.": {
			setup: func(t *testing.T) (string, string, func()) {
				srcDir := t.TempDir()
				dstDir := t.TempDir()

				srcFile := filepath.Join(srcDir, "test.txt")
				require.NoError(t, os.WriteFile(srcFile, []byte("hello world"), 0644))

				dstFile := filepath.Join(dstDir, "test.txt")
				return srcFile, dstFile, func() {}
			},
			validate: func(t *testing.T, dstRemote string) {
				data, err := os.ReadFile(dstRemote)
				require.NoError(t, err)
				assert.Equal(t, "hello world", string(data))
			},
		},

		"Copy directory should work.": {
			setup: func(t *testing.T) (string, string, func()) {
				srcDir := t.TempDir()
				dstDir := t.TempDir()

				// Create nested structure.
				subDir := filepath.Join(srcDir, "subdir")
				require.NoError(t, os.MkdirAll(subDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(srcDir, "file1.txt"), []byte("file1"), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(subDir, "file2.txt"), []byte("file2"), 0644))

				dstPath := filepath.Join(dstDir, "copied")
				return srcDir, dstPath, func() {}
			},
			validate: func(t *testing.T, dstRemote string) {
				data1, err := os.ReadFile(filepath.Join(dstRemote, "file1.txt"))
				require.NoError(t, err)
				assert.Equal(t, "file1", string(data1))

				data2, err := os.ReadFile(filepath.Join(dstRemote, "subdir", "file2.txt"))
				require.NoError(t, err)
				assert.Equal(t, "file2", string(data2))
			},
		},

		"Copy non-existent source should fail.": {
			setup: func(t *testing.T) (string, string, func()) {
				return "/nonexistent/path", "/tmp/dst", func() {}
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			client, err := NewClient(ctx, ClientConfig{
				Host:       host,
				Port:       port,
				User:       "root",
				PrivateKey: privKey,
				Logger:     log.Noop,
			})
			require.NoError(err)
			defer client.Close()

			srcLocal, dstRemote, cleanup := test.setup(t)
			defer cleanup()

			err = client.CopyTo(ctx, srcLocal, dstRemote)
			if test.expErr {
				assert.Error(t, err)
				return
			}

			require.NoError(err)
			if test.validate != nil {
				test.validate(t, dstRemote)
			}
		})
	}
}

func TestClient_CopyFrom(t *testing.T) {
	privKey := generateTestKeyPair(t)
	server := newTestSSHServer(t, privKey)
	defer server.close()

	host, port := testParseHostPort(t, server.addr)

	tests := map[string]struct {
		setup    func(t *testing.T) (srcRemote, dstLocal string)
		expErr   bool
		validate func(t *testing.T, dstLocal string)
	}{
		"Copy single remote file should work.": {
			setup: func(t *testing.T) (string, string) {
				// Create a "remote" file (it's local since test server runs locally).
				remoteDir := t.TempDir()
				remoteFile := filepath.Join(remoteDir, "remote.txt")
				require.NoError(t, os.WriteFile(remoteFile, []byte("remote data"), 0644))

				localDir := t.TempDir()
				localFile := filepath.Join(localDir, "local.txt")

				return remoteFile, localFile
			},
			validate: func(t *testing.T, dstLocal string) {
				data, err := os.ReadFile(dstLocal)
				require.NoError(t, err)
				assert.Equal(t, "remote data", string(data))
			},
		},

		"Copy remote directory should work.": {
			setup: func(t *testing.T) (string, string) {
				remoteDir := t.TempDir()
				subDir := filepath.Join(remoteDir, "sub")
				require.NoError(t, os.MkdirAll(subDir, 0755))
				require.NoError(t, os.WriteFile(filepath.Join(remoteDir, "a.txt"), []byte("aaa"), 0644))
				require.NoError(t, os.WriteFile(filepath.Join(subDir, "b.txt"), []byte("bbb"), 0644))

				localDir := t.TempDir()
				localPath := filepath.Join(localDir, "copied")

				return remoteDir, localPath
			},
			validate: func(t *testing.T, dstLocal string) {
				data1, err := os.ReadFile(filepath.Join(dstLocal, "a.txt"))
				require.NoError(t, err)
				assert.Equal(t, "aaa", string(data1))

				data2, err := os.ReadFile(filepath.Join(dstLocal, "sub", "b.txt"))
				require.NoError(t, err)
				assert.Equal(t, "bbb", string(data2))
			},
		},

		"Copy non-existent remote path should fail.": {
			setup: func(t *testing.T) (string, string) {
				return "/nonexistent/remote/path", t.TempDir()
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			require := require.New(t)

			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()

			client, err := NewClient(ctx, ClientConfig{
				Host:       host,
				Port:       port,
				User:       "root",
				PrivateKey: privKey,
				Logger:     log.Noop,
			})
			require.NoError(err)
			defer client.Close()

			srcRemote, dstLocal := test.setup(t)

			err = client.CopyFrom(ctx, srcRemote, dstLocal)
			if test.expErr {
				assert.Error(t, err)
				return
			}

			require.NoError(err)
			if test.validate != nil {
				test.validate(t, dstLocal)
			}
		})
	}
}

func TestClient_Forward(t *testing.T) {
	assert := assert.New(t)
	require := require.New(t)

	privKey := generateTestKeyPair(t)
	server := newTestSSHServer(t, privKey)
	defer server.close()

	host, port := testParseHostPort(t, server.addr)

	// Start a TCP echo server to forward to.
	echoListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)
	defer echoListener.Close()

	_, echoPort := testParseHostPort(t, echoListener.Addr().String())

	go func() {
		for {
			conn, err := echoListener.Accept()
			if err != nil {
				return
			}
			go func(c net.Conn) {
				defer c.Close()
				_, _ = io.Copy(c, c)
			}(conn)
		}
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Host:       host,
		Port:       port,
		User:       "root",
		PrivateKey: privKey,
		Logger:     log.Noop,
	})
	require.NoError(err)
	defer client.Close()

	// Find a free local port.
	freeListener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(err)
	_, freePort := testParseHostPort(t, freeListener.Addr().String())
	freeListener.Close()

	// Start forwarding in background.
	forwardCtx, forwardCancel := context.WithCancel(ctx)
	defer forwardCancel()

	forwardDone := make(chan error, 1)
	go func() {
		forwardDone <- client.Forward(forwardCtx, []PortForward{
			{LocalPort: freePort, RemotePort: echoPort},
		})
	}()

	// Give the forwarder time to start listening.
	time.Sleep(200 * time.Millisecond)

	// Connect to the forwarded port and test echo.
	conn, err := net.DialTimeout("tcp", fmt.Sprintf("127.0.0.1:%d", freePort), 2*time.Second)
	require.NoError(err)
	defer conn.Close()

	_, err = conn.Write([]byte("test data"))
	require.NoError(err)

	buf := make([]byte, 100)
	require.NoError(conn.SetReadDeadline(time.Now().Add(2 * time.Second)))
	n, err := conn.Read(buf)
	require.NoError(err)
	assert.Equal("test data", string(buf[:n]))

	// Cancel forward and verify it returns.
	forwardCancel()
	err = <-forwardDone
	assert.ErrorIs(err, context.Canceled)
}

func TestClient_Forward_EmptyPorts(t *testing.T) {
	privKey := generateTestKeyPair(t)
	server := newTestSSHServer(t, privKey)
	defer server.close()

	host, port := testParseHostPort(t, server.addr)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	client, err := NewClient(ctx, ClientConfig{
		Host:       host,
		Port:       port,
		User:       "root",
		PrivateKey: privKey,
		Logger:     log.Noop,
	})
	require.NoError(t, err)
	defer client.Close()

	err = client.Forward(ctx, []PortForward{})
	assert.Error(t, err)
}
