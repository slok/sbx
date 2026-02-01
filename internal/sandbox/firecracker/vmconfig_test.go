package firecracker

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

func TestEngine_findFirecrackerBinary(t *testing.T) {
	e := &Engine{}

	// Test with explicit path that doesn't exist
	e.firecrackerBinary = "/nonexistent/path/firecracker"
	path, err := e.findFirecrackerBinary()
	if path == "/nonexistent/path/firecracker" {
		t.Error("should not return nonexistent explicit path")
	}
	// It may find it in PATH or ./bin, so we just check it doesn't panic
	_ = err
}

func TestBootSource_JSON(t *testing.T) {
	bs := BootSource{
		KernelImagePath: "/path/to/vmlinux",
		BootArgs:        "console=ttyS0 reboot=k panic=1 pci=off",
	}

	data, err := json.Marshal(bs)
	if err != nil {
		t.Fatalf("failed to marshal BootSource: %v", err)
	}

	var decoded BootSource
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal BootSource: %v", err)
	}

	if decoded.KernelImagePath != bs.KernelImagePath {
		t.Errorf("KernelImagePath mismatch: got %s, want %s", decoded.KernelImagePath, bs.KernelImagePath)
	}
	if decoded.BootArgs != bs.BootArgs {
		t.Errorf("BootArgs mismatch: got %s, want %s", decoded.BootArgs, bs.BootArgs)
	}
}

func TestDrive_JSON(t *testing.T) {
	d := Drive{
		DriveID:      "rootfs",
		PathOnHost:   "/path/to/rootfs.ext4",
		IsRootDevice: true,
		IsReadOnly:   false,
	}

	data, err := json.Marshal(d)
	if err != nil {
		t.Fatalf("failed to marshal Drive: %v", err)
	}

	var decoded Drive
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal Drive: %v", err)
	}

	if decoded.DriveID != d.DriveID {
		t.Errorf("DriveID mismatch: got %s, want %s", decoded.DriveID, d.DriveID)
	}
	if decoded.IsRootDevice != d.IsRootDevice {
		t.Errorf("IsRootDevice mismatch: got %v, want %v", decoded.IsRootDevice, d.IsRootDevice)
	}
}

func TestMachineConfig_JSON(t *testing.T) {
	mc := MachineConfig{
		VCPUCount:  2,
		MemSizeMib: 1024,
		Smt:        true,
	}

	data, err := json.Marshal(mc)
	if err != nil {
		t.Fatalf("failed to marshal MachineConfig: %v", err)
	}

	var decoded MachineConfig
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal MachineConfig: %v", err)
	}

	if decoded.VCPUCount != mc.VCPUCount {
		t.Errorf("VCPUCount mismatch: got %d, want %d", decoded.VCPUCount, mc.VCPUCount)
	}
	if decoded.MemSizeMib != mc.MemSizeMib {
		t.Errorf("MemSizeMib mismatch: got %d, want %d", decoded.MemSizeMib, mc.MemSizeMib)
	}
}

func TestNetworkInterface_JSON(t *testing.T) {
	ni := NetworkInterface{
		IfaceID:     "eth0",
		GuestMAC:    "06:00:0A:01:02:02",
		HostDevName: "sbx-0102",
	}

	data, err := json.Marshal(ni)
	if err != nil {
		t.Fatalf("failed to marshal NetworkInterface: %v", err)
	}

	var decoded NetworkInterface
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatalf("failed to unmarshal NetworkInterface: %v", err)
	}

	if decoded.IfaceID != ni.IfaceID {
		t.Errorf("IfaceID mismatch: got %s, want %s", decoded.IfaceID, ni.IfaceID)
	}
	if decoded.GuestMAC != ni.GuestMAC {
		t.Errorf("GuestMAC mismatch: got %s, want %s", decoded.GuestMAC, ni.GuestMAC)
	}
	if decoded.HostDevName != ni.HostDevName {
		t.Errorf("HostDevName mismatch: got %s, want %s", decoded.HostDevName, ni.HostDevName)
	}
}

func TestInstanceActionInfo_JSON(t *testing.T) {
	action := InstanceActionInfo{
		ActionType: "InstanceStart",
	}

	data, err := json.Marshal(action)
	if err != nil {
		t.Fatalf("failed to marshal InstanceActionInfo: %v", err)
	}

	expected := `{"action_type":"InstanceStart"}`
	if string(data) != expected {
		t.Errorf("JSON mismatch: got %s, want %s", string(data), expected)
	}
}

func TestEngine_waitForSocket(t *testing.T) {
	e := &Engine{logger: log.Noop}

	// Create a temporary socket
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	// Start a listener in the background
	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create socket: %v", err)
	}
	defer listener.Close()

	// Should succeed immediately
	err = e.waitForSocket(socketPath, 1*time.Second)
	if err != nil {
		t.Errorf("waitForSocket should succeed: %v", err)
	}

	// Non-existent socket should timeout
	err = e.waitForSocket("/nonexistent/socket.sock", 100*time.Millisecond)
	if err == nil {
		t.Error("waitForSocket should fail for non-existent socket")
	}
}

func TestEngine_newUnixHTTPClient(t *testing.T) {
	e := &Engine{}

	// Create a test server on a Unix socket
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create socket: %v", err)
	}
	defer listener.Close()

	// Create a test handler
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut && r.URL.Path == "/test" {
			w.WriteHeader(http.StatusNoContent)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})

	go func() { _ = http.Serve(listener, handler) }()

	client := e.newUnixHTTPClient(socketPath)
	if client == nil {
		t.Fatal("newUnixHTTPClient returned nil")
	}

	// Test that client can connect
	req, _ := http.NewRequest(http.MethodPut, "http://localhost/test", nil)
	resp, err := client.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		t.Errorf("unexpected status: got %d, want %d", resp.StatusCode, http.StatusNoContent)
	}
}

func TestEngine_apiPUT(t *testing.T) {
	e := &Engine{logger: log.Noop}

	// Create a test server on a Unix socket
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create socket: %v", err)
	}
	defer listener.Close()

	var receivedPath string
	var receivedBody BootSource

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Content-Type should be application/json")
		}
		_ = json.NewDecoder(r.Body).Decode(&receivedBody)
		w.WriteHeader(http.StatusNoContent)
	})

	go func() { _ = http.Serve(listener, handler) }()

	client := e.newUnixHTTPClient(socketPath)

	bootSource := BootSource{
		KernelImagePath: "/path/to/vmlinux",
		BootArgs:        "console=ttyS0",
	}

	err = e.apiPUT(context.Background(), client, "/boot-source", bootSource)
	if err != nil {
		t.Fatalf("apiPUT failed: %v", err)
	}

	// Give the server time to process
	time.Sleep(10 * time.Millisecond)

	if receivedPath != "/boot-source" {
		t.Errorf("path mismatch: got %s, want /boot-source", receivedPath)
	}
	if receivedBody.KernelImagePath != bootSource.KernelImagePath {
		t.Errorf("body mismatch: got %s, want %s", receivedBody.KernelImagePath, bootSource.KernelImagePath)
	}
}

func TestEngine_apiPUT_error(t *testing.T) {
	e := &Engine{logger: log.Noop}

	// Create a test server that returns an error
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "test.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create socket: %v", err)
	}
	defer listener.Close()

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"error": "invalid configuration"}`))
	})

	go func() { _ = http.Serve(listener, handler) }()

	client := e.newUnixHTTPClient(socketPath)

	err = e.apiPUT(context.Background(), client, "/boot-source", BootSource{})
	if err == nil {
		t.Error("apiPUT should return error for non-2xx status")
	}
}

// TestMockFirecrackerAPI_configureVM simulates a Firecracker API for testing configureVM.
func TestMockFirecrackerAPI_configureVM(t *testing.T) {
	// Create a mock Firecracker API server
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "firecracker.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create socket: %v", err)
	}
	defer listener.Close()

	// Track API calls
	apiCalls := make(map[string]int)

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		apiCalls[r.URL.Path]++
		w.WriteHeader(http.StatusNoContent)
	})

	go func() { _ = http.Serve(listener, handler) }()

	// Create engine and configure VM
	e := &Engine{logger: log.Noop}

	// Create dummy rootfs file
	rootfsPath := filepath.Join(tmpDir, "rootfs.ext4")
	_ = os.WriteFile(rootfsPath, []byte("dummy"), 0644)

	resources := model.Resources{
		VCPUs:    2,
		MemoryMB: 1024,
	}

	err = e.configureVM(
		context.Background(),
		socketPath,
		"/path/to/vmlinux",
		tmpDir,
		"06:00:0A:01:02:02",
		"sbx-0102",
		resources,
	)
	if err != nil {
		t.Fatalf("configureVM failed: %v", err)
	}

	// Give server time to process
	time.Sleep(50 * time.Millisecond)

	// Verify all API endpoints were called
	expectedCalls := []string{
		"/boot-source",
		"/drives/rootfs",
		"/machine-config",
		"/network-interfaces/eth0",
	}

	for _, path := range expectedCalls {
		if apiCalls[path] != 1 {
			t.Errorf("expected 1 call to %s, got %d", path, apiCalls[path])
		}
	}
}

func TestMockFirecrackerAPI_bootVM(t *testing.T) {
	tmpDir := t.TempDir()
	socketPath := filepath.Join(tmpDir, "firecracker.sock")

	listener, err := net.Listen("unix", socketPath)
	if err != nil {
		t.Fatalf("failed to create socket: %v", err)
	}
	defer listener.Close()

	var receivedAction InstanceActionInfo

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/actions" {
			_ = json.NewDecoder(r.Body).Decode(&receivedAction)
		}
		w.WriteHeader(http.StatusNoContent)
	})

	go func() { _ = http.Serve(listener, handler) }()

	e := &Engine{logger: log.Noop}

	err = e.bootVM(context.Background(), socketPath)
	if err != nil {
		t.Fatalf("bootVM failed: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	if receivedAction.ActionType != "InstanceStart" {
		t.Errorf("expected InstanceStart action, got %s", receivedAction.ActionType)
	}
}

// TestHTTPTestServer_compatible tests the HTTP client mock setup used in tests.
func TestHTTPTestServer_compatible(t *testing.T) {
	// Verify httptest.Server can be used for mocking
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Errorf("unexpected status: %d", resp.StatusCode)
	}
}
