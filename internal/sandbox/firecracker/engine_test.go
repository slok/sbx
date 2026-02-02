package firecracker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/slok/sbx/internal/log"
	"github.com/slok/sbx/internal/model"
)

func TestEngine_allocateNetwork(t *testing.T) {
	e := &Engine{}

	tests := []struct {
		name      string
		sandboxID string
		wantMAC   string
		wantGW    string
		wantVMIP  string
		wantTAP   string
	}{
		{
			name:      "deterministic allocation",
			sandboxID: "01JMTEST1234567890ABCDEF",
			// Hash of this ID determines the values
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mac1, gw1, vmIP1, tap1 := e.allocateNetwork(tt.sandboxID)
			mac2, gw2, vmIP2, tap2 := e.allocateNetwork(tt.sandboxID)

			// Same ID should produce same results
			if mac1 != mac2 || gw1 != gw2 || vmIP1 != vmIP2 || tap1 != tap2 {
				t.Error("allocateNetwork should be deterministic for same ID")
			}

			// Verify format
			if mac1[:6] != "06:00:" {
				t.Errorf("MAC should start with 06:00:, got %s", mac1)
			}
			if gw1[:3] != "10." {
				t.Errorf("Gateway should start with 10., got %s", gw1)
			}
			if vmIP1[:3] != "10." {
				t.Errorf("VM IP should start with 10., got %s", vmIP1)
			}
			if tap1[:4] != "sbx-" {
				t.Errorf("TAP should start with sbx-, got %s", tap1)
			}
		})
	}
}

func TestEngine_allocateNetwork_differentIDs(t *testing.T) {
	e := &Engine{}

	mac1, gw1, vmIP1, tap1 := e.allocateNetwork("sandbox-1")
	mac2, gw2, vmIP2, tap2 := e.allocateNetwork("sandbox-2")

	// Different IDs should produce different results (with high probability)
	if mac1 == mac2 && gw1 == gw2 && vmIP1 == vmIP2 && tap1 == tap2 {
		t.Error("different sandbox IDs should produce different network allocations")
	}
}

func TestEngine_expandPath(t *testing.T) {
	e := &Engine{}

	tests := []struct {
		name     string
		path     string
		wantHome bool // if true, should expand ~ to home
	}{
		{
			name:     "tilde path",
			path:     "~/.sbx/images/vmlinux",
			wantHome: true,
		},
		{
			name:     "absolute path",
			path:     "/opt/firecracker/vmlinux",
			wantHome: false,
		},
		{
			name:     "relative path",
			path:     "./images/vmlinux",
			wantHome: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := e.expandPath(tt.path)
			if tt.wantHome {
				if result == tt.path {
					t.Error("tilde path should be expanded")
				}
				if result[0] != '/' {
					t.Error("expanded path should be absolute")
				}
			} else {
				if result != tt.path {
					t.Errorf("path should not change, got %s", result)
				}
			}
		})
	}
}

func TestEngine_Check(t *testing.T) {
	// This test just verifies Check returns results without panicking
	// Actual values depend on system state
	e := &Engine{}

	ctx := context.Background()
	results := e.Check(ctx)

	// Should return at least 4 checks
	if len(results) < 4 {
		t.Errorf("Check should return at least 4 results, got %d", len(results))
	}

	// Verify expected check IDs are present
	expectedIDs := map[string]bool{
		"kvm_available":      false,
		"firecracker_binary": false,
		"ip_forward":         false,
		"iptables":           false,
	}

	for _, r := range results {
		if _, ok := expectedIDs[r.ID]; ok {
			expectedIDs[r.ID] = true
		}
		// Verify status is valid
		if r.Status != model.CheckStatusOK && r.Status != model.CheckStatusWarning && r.Status != model.CheckStatusError {
			t.Errorf("invalid status for check %s: %s", r.ID, r.Status)
		}
	}

	for id, found := range expectedIDs {
		if !found {
			t.Errorf("expected check ID %s not found", id)
		}
	}
}

func TestEngine_CheckKernelImage(t *testing.T) {
	e := &Engine{}

	// Non-existent path should return error
	result := e.CheckKernelImage("/nonexistent/path/vmlinux")
	if result.Status != model.CheckStatusError {
		t.Errorf("non-existent kernel should return error, got %s", result.Status)
	}
	if result.ID != "kernel_image" {
		t.Errorf("expected ID kernel_image, got %s", result.ID)
	}
}

func TestEngine_CheckRootFS(t *testing.T) {
	e := &Engine{}

	// Non-existent path should return error
	result := e.CheckRootFS("/nonexistent/path/rootfs.ext4")
	if result.Status != model.CheckStatusError {
		t.Errorf("non-existent rootfs should return error, got %s", result.Status)
	}
	if result.ID != "base_rootfs" {
		t.Errorf("expected ID base_rootfs, got %s", result.ID)
	}
}

func TestEngine_Create_DiskGBValidation(t *testing.T) {
	tests := map[string]struct {
		diskGB    int
		expErr    bool
		expErrMsg string
	}{
		"Valid disk_gb at maximum should succeed validation": {
			diskGB: 25,
			expErr: false,
		},
		"Valid disk_gb below maximum should succeed validation": {
			diskGB: 10,
			expErr: false,
		},
		"disk_gb exceeding maximum should fail early": {
			diskGB:    26,
			expErr:    true,
			expErrMsg: "exceeds maximum allowed",
		},
		"disk_gb way over maximum should fail early": {
			diskGB:    100,
			expErr:    true,
			expErrMsg: "exceeds maximum allowed",
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			assert := assert.New(t)
			require := require.New(t)

			e, err := NewEngine(EngineConfig{
				DataDir: t.TempDir(),
				Logger:  log.Noop,
			})
			require.NoError(err)

			cfg := model.SandboxConfig{
				Name: "test-sandbox",
				FirecrackerEngine: &model.FirecrackerEngineConfig{
					KernelImage: "/fake/vmlinux",
					RootFS:      "/fake/rootfs.ext4",
				},
				Resources: model.Resources{
					VCPUs:    1,
					MemoryMB: 512,
					DiskGB:   tc.diskGB,
				},
			}

			_, err = e.Create(context.Background(), cfg)

			if tc.expErr {
				// Should fail early with validation error (before trying to access files)
				assert.Error(err)
				assert.Contains(err.Error(), tc.expErrMsg)
			} else {
				// Will fail later due to fake paths, but NOT with the validation error
				assert.Error(err) // It will fail because kernel/rootfs don't exist
				assert.NotContains(err.Error(), "exceeds maximum allowed")
			}
		})
	}
}
