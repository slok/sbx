package egress

import (
	"fmt"
	"strings"
)

// Protocol represents the detected application protocol.
type Protocol int

const (
	// ProtoUnknown is an unrecognized protocol.
	ProtoUnknown Protocol = iota
	// ProtoTLS is a TLS connection (SNI extracted).
	ProtoTLS
	// ProtoHTTP is a plain HTTP connection (Host header extracted).
	ProtoHTTP
)

func (p Protocol) String() string {
	switch p {
	case ProtoTLS:
		return "tls"
	case ProtoHTTP:
		return "http"
	default:
		return "unknown"
	}
}

// ClassifyResult holds the result of protocol classification.
type ClassifyResult struct {
	Protocol Protocol
	Host     string // domain from SNI or Host header (empty if unknown).
}

// Classify inspects the first bytes of a connection and determines the protocol.
// Returns the detected protocol and host (domain) if available.
func Classify(buf []byte) ClassifyResult {
	if len(buf) == 0 {
		return ClassifyResult{Protocol: ProtoUnknown}
	}

	// TLS ClientHello starts with content type 0x16 (handshake).
	if buf[0] == 0x16 {
		sni, err := extractSNI(buf)
		if err == nil && sni != "" {
			return ClassifyResult{Protocol: ProtoTLS, Host: sni}
		}
		// TLS but couldn't extract SNI — still TLS.
		return ClassifyResult{Protocol: ProtoTLS}
	}

	// HTTP request starts with a method: GET, POST, PUT, DELETE, HEAD, OPTIONS, PATCH, CONNECT.
	if host, ok := extractHTTPHost(buf); ok {
		return ClassifyResult{Protocol: ProtoHTTP, Host: host}
	}

	return ClassifyResult{Protocol: ProtoUnknown}
}

// extractSNI parses a TLS ClientHello message and extracts the SNI hostname.
// Returns an error if the data is not a valid ClientHello or doesn't contain SNI.
func extractSNI(buf []byte) (string, error) {
	// Minimum TLS record: 5 bytes header + 1 byte content.
	if len(buf) < 6 {
		return "", fmt.Errorf("too short for TLS record")
	}

	// Record header: content_type(1) + version(2) + length(2).
	if buf[0] != 0x16 { // handshake
		return "", fmt.Errorf("not a handshake record")
	}
	recordLen := int(buf[3])<<8 | int(buf[4])
	data := buf[5:]
	if len(data) >= recordLen {
		// Trim to the exact record length if we have enough data.
		// If we have a partial record, use what we have (no trim needed).
		data = data[:recordLen]
	}

	// Handshake header: type(1) + length(3).
	if len(data) < 4 {
		return "", fmt.Errorf("too short for handshake header")
	}
	if data[0] != 0x01 { // client_hello
		return "", fmt.Errorf("not a ClientHello")
	}
	hsLen := int(data[1])<<16 | int(data[2])<<8 | int(data[3])
	data = data[4:]
	if len(data) >= hsLen {
		data = data[:hsLen]
	}

	// ClientHello: version(2) + random(32) = 34 bytes minimum.
	if len(data) < 34 {
		return "", fmt.Errorf("too short for ClientHello")
	}
	data = data[34:] // skip version + random

	// Session ID: length(1) + data.
	if len(data) < 1 {
		return "", fmt.Errorf("missing session ID length")
	}
	sessIDLen := int(data[0])
	data = data[1:]
	if len(data) < sessIDLen {
		return "", fmt.Errorf("session ID truncated")
	}
	data = data[sessIDLen:]

	// Cipher suites: length(2) + data.
	if len(data) < 2 {
		return "", fmt.Errorf("missing cipher suites length")
	}
	csLen := int(data[0])<<8 | int(data[1])
	data = data[2:]
	if len(data) < csLen {
		return "", fmt.Errorf("cipher suites truncated")
	}
	data = data[csLen:]

	// Compression methods: length(1) + data.
	if len(data) < 1 {
		return "", fmt.Errorf("missing compression methods length")
	}
	compLen := int(data[0])
	data = data[1:]
	if len(data) < compLen {
		return "", fmt.Errorf("compression methods truncated")
	}
	data = data[compLen:]

	// Extensions: length(2) + data.
	if len(data) < 2 {
		return "", fmt.Errorf("no extensions")
	}
	extLen := int(data[0])<<8 | int(data[1])
	data = data[2:]
	if len(data) >= extLen {
		data = data[:extLen]
	}

	// Walk extensions looking for server_name (type 0x0000).
	for len(data) >= 4 {
		extType := int(data[0])<<8 | int(data[1])
		extDataLen := int(data[2])<<8 | int(data[3])
		data = data[4:]

		if len(data) < extDataLen {
			break
		}

		if extType == 0x0000 { // server_name
			return parseSNIExtension(data[:extDataLen])
		}

		data = data[extDataLen:]
	}

	return "", fmt.Errorf("no SNI extension found")
}

// parseSNIExtension parses the server_name extension payload.
func parseSNIExtension(data []byte) (string, error) {
	// ServerNameList: length(2) + list entries.
	if len(data) < 2 {
		return "", fmt.Errorf("SNI extension too short")
	}
	listLen := int(data[0])<<8 | int(data[1])
	data = data[2:]
	if len(data) >= listLen {
		data = data[:listLen]
	}

	// Each entry: type(1) + length(2) + name.
	for len(data) >= 3 {
		nameType := data[0]
		nameLen := int(data[1])<<8 | int(data[2])
		data = data[3:]
		if len(data) < nameLen {
			break
		}

		if nameType == 0x00 { // host_name
			return string(data[:nameLen]), nil
		}

		data = data[nameLen:]
	}

	return "", fmt.Errorf("no host_name in SNI")
}

// extractHTTPHost checks if buf starts with an HTTP request and extracts the Host header.
// Returns the host value and true if this looks like HTTP.
func extractHTTPHost(buf []byte) (string, bool) {
	// Quick check: first bytes should be an HTTP method.
	s := string(buf)

	methods := []string{"GET ", "POST ", "PUT ", "DELETE ", "HEAD ", "OPTIONS ", "PATCH ", "CONNECT "}
	isHTTP := false
	for _, m := range methods {
		if strings.HasPrefix(s, m) {
			isHTTP = true
			break
		}
	}
	if !isHTTP {
		return "", false
	}

	// Look for Host header (case-insensitive).
	lower := strings.ToLower(s)
	idx := strings.Index(lower, "\r\nhost: ")
	if idx == -1 {
		idx = strings.Index(lower, "\nhost: ")
		if idx == -1 {
			return "", true // HTTP but no Host header found yet.
		}
		idx += 7 // len("\nhost: ")
	} else {
		idx += 8 // len("\r\nhost: ")
	}

	// Extract value until end of line.
	end := strings.IndexAny(s[idx:], "\r\n")
	if end == -1 {
		// Header might be partial.
		return strings.TrimSpace(s[idx:]), true
	}

	host := strings.TrimSpace(s[idx : idx+end])

	// Strip port if present.
	if colonIdx := strings.LastIndex(host, ":"); colonIdx != -1 {
		host = host[:colonIdx]
	}

	return host, true
}
