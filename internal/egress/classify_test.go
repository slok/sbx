package egress

import (
	"crypto/tls"
	"net"
	"net/http"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractSNI(t *testing.T) {
	tests := map[string]struct {
		buildClientHello func(t *testing.T) []byte
		expSNI           string
		expErr           bool
	}{
		"Real TLS ClientHello with SNI should extract hostname.": {
			buildClientHello: func(t *testing.T) []byte {
				return captureTLSClientHello(t, "github.com")
			},
			expSNI: "github.com",
		},
		"Real TLS ClientHello with subdomain SNI.": {
			buildClientHello: func(t *testing.T) []byte {
				return captureTLSClientHello(t, "api.github.com")
			},
			expSNI: "api.github.com",
		},
		"Empty buffer should error.": {
			buildClientHello: func(t *testing.T) []byte {
				return []byte{}
			},
			expErr: true,
		},
		"Non-TLS data should error.": {
			buildClientHello: func(t *testing.T) []byte {
				return []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
			},
			expErr: true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			buf := test.buildClientHello(t)
			sni, err := extractSNI(buf)

			if test.expErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.Equal(t, test.expSNI, sni)
			}
		})
	}
}

func TestExtractHTTPHost(t *testing.T) {
	tests := map[string]struct {
		input   string
		expHost string
		expOK   bool
	}{
		"GET request with Host header.": {
			input:   "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n",
			expHost: "example.com",
			expOK:   true,
		},
		"POST request with Host header.": {
			input:   "POST /api HTTP/1.1\r\nHost: api.github.com\r\nContent-Type: application/json\r\n\r\n{}",
			expHost: "api.github.com",
			expOK:   true,
		},
		"Host header with port should strip port.": {
			input:   "GET / HTTP/1.1\r\nHost: example.com:8080\r\n\r\n",
			expHost: "example.com",
			expOK:   true,
		},
		"Non-HTTP data.": {
			input:   "hello world",
			expHost: "",
			expOK:   false,
		},
		"HTTP request without Host header.": {
			input:   "GET / HTTP/1.1\r\n\r\n",
			expHost: "",
			expOK:   true,
		},
		"DELETE request.": {
			input:   "DELETE /resource HTTP/1.1\r\nHost: api.example.com\r\n\r\n",
			expHost: "api.example.com",
			expOK:   true,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			host, ok := extractHTTPHost([]byte(test.input))
			assert.Equal(t, test.expOK, ok)
			assert.Equal(t, test.expHost, host)
		})
	}
}

func TestClassify(t *testing.T) {
	tests := map[string]struct {
		input    func(t *testing.T) []byte
		expProto Protocol
		expHost  string
	}{
		"TLS connection should be classified as TLS with SNI.": {
			input: func(t *testing.T) []byte {
				return captureTLSClientHello(t, "github.com")
			},
			expProto: ProtoTLS,
			expHost:  "github.com",
		},
		"HTTP request should be classified as HTTP with Host.": {
			input: func(t *testing.T) []byte {
				return []byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")
			},
			expProto: ProtoHTTP,
			expHost:  "example.com",
		},
		"Unknown protocol should be classified as unknown.": {
			input: func(t *testing.T) []byte {
				return []byte{0x00, 0x01, 0x02, 0x03}
			},
			expProto: ProtoUnknown,
			expHost:  "",
		},
		"Empty buffer should be unknown.": {
			input: func(t *testing.T) []byte {
				return []byte{}
			},
			expProto: ProtoUnknown,
			expHost:  "",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			result := Classify(test.input(t))
			assert.Equal(t, test.expProto, result.Protocol)
			assert.Equal(t, test.expHost, result.Host)
		})
	}
}

// captureTLSClientHello generates a real TLS ClientHello by starting
// a TLS handshake against a local listener and capturing the first bytes.
func captureTLSClientHello(t *testing.T, serverName string) []byte {
	t.Helper()

	// Create a local TCP listener.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	// Channel to capture the ClientHello bytes.
	ch := make(chan []byte, 1)

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			ch <- nil
			return
		}
		defer conn.Close()

		buf := make([]byte, 4096)
		n, _ := conn.Read(buf)
		ch <- buf[:n]
	}()

	// Initiate TLS handshake (will fail since server isn't TLS, but we capture the ClientHello).
	conn, err := net.Dial("tcp", ln.Addr().String())
	require.NoError(t, err)

	tlsConn := tls.Client(conn, &tls.Config{
		ServerName:         serverName,
		InsecureSkipVerify: true,
	})
	// Start handshake (will fail, that's OK — we just need the ClientHello).
	go func() {
		_ = tlsConn.Handshake()
		tlsConn.Close()
	}()

	buf := <-ch
	require.NotEmpty(t, buf, "failed to capture ClientHello")

	// Verify it starts with TLS handshake byte.
	require.Equal(t, byte(0x16), buf[0], "captured data is not a TLS record")

	return buf
}

// TestExtractSNIWithRealHTTPRequest ensures extractSNI properly rejects HTTP data.
func TestExtractSNIWithRealHTTPRequest(t *testing.T) {
	req, _ := http.NewRequest("GET", "http://example.com", nil)
	_ = req
	_, err := extractSNI([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
	assert.Error(t, err)
}
