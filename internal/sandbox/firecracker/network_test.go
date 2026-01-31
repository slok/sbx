package firecracker

import (
	"testing"
)

func TestEngine_subnetFromGateway(t *testing.T) {
	e := &Engine{}

	tests := []struct {
		gateway string
		want    string
	}{
		{"10.1.2.1", "10.1.2.0/24"},
		{"192.168.1.1", "192.168.1.0/24"},
		{"172.16.0.1", "172.16.0.0/24"},
		{"10.163.242.1", "10.163.242.0/24"},
	}

	for _, tt := range tests {
		t.Run(tt.gateway, func(t *testing.T) {
			got := e.subnetFromGateway(tt.gateway)
			if got != tt.want {
				t.Errorf("subnetFromGateway(%s) = %s, want %s", tt.gateway, got, tt.want)
			}
		})
	}
}

func TestEngine_getDefaultInterface(t *testing.T) {
	e := &Engine{}

	// This test just verifies the function runs without panicking
	// The actual result depends on the system's network configuration
	iface, err := e.getDefaultInterface()
	if err != nil {
		// Not an error if there's no default route (e.g., in isolated test env)
		t.Logf("getDefaultInterface returned error (may be ok in test env): %v", err)
		return
	}

	if iface == "" {
		t.Error("getDefaultInterface returned empty interface name")
	}
	t.Logf("Default interface: %s", iface)
}
