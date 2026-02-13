package firecracker

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"

	"github.com/slok/sbx/internal/conventions"
	"github.com/slok/sbx/internal/model"
)

// ProxyPorts holds the allocated ports for the proxy process.
type ProxyPorts struct {
	HTTPPort int `json:"http_port"`
	DNSPort  int `json:"dns_port"`
}

// spawnProxy starts the sbx internal-vm-proxy process with the given egress policy.
// It writes the PID file and port file to vmDir. Returns the PID and allocated ports.
func (e *Engine) spawnProxy(vmDir string, egress model.EgressPolicy) (int, ProxyPorts, error) {
	sbxBinary, err := os.Executable()
	if err != nil {
		return 0, ProxyPorts{}, fmt.Errorf("could not find sbx binary: %w", err)
	}

	httpPort, err := getFreePort()
	if err != nil {
		return 0, ProxyPorts{}, fmt.Errorf("could not allocate HTTP proxy port: %w", err)
	}

	dnsPort, err := getFreeUDPPort()
	if err != nil {
		return 0, ProxyPorts{}, fmt.Errorf("could not allocate DNS proxy port: %w", err)
	}

	args := buildProxyArgs(egress, httpPort, dnsPort)

	logPath := filepath.Join(vmDir, conventions.ProxyLogFile)
	logFile, err := os.Create(logPath)
	if err != nil {
		return 0, ProxyPorts{}, fmt.Errorf("could not create proxy log file: %w", err)
	}

	cmd := exec.Command(sbxBinary, args...)
	cmd.Dir = vmDir
	cmd.Stdout = logFile
	cmd.Stderr = logFile

	if err := cmd.Start(); err != nil {
		logFile.Close()
		return 0, ProxyPorts{}, fmt.Errorf("could not start proxy process: %w", err)
	}
	logFile.Close()

	pid := cmd.Process.Pid

	// Write PID file.
	pidPath := filepath.Join(vmDir, conventions.ProxyPIDFile)
	if err := os.WriteFile(pidPath, []byte(fmt.Sprintf("%d", pid)), 0644); err != nil {
		e.logger.Warningf("Could not write proxy PID file: %v", err)
	}

	// Write port file.
	ports := ProxyPorts{HTTPPort: httpPort, DNSPort: dnsPort}
	portData, err := json.Marshal(ports)
	if err != nil {
		e.logger.Warningf("Could not marshal proxy ports: %v", err)
	} else {
		portPath := filepath.Join(vmDir, conventions.ProxyPortFile)
		if err := os.WriteFile(portPath, portData, 0644); err != nil {
			e.logger.Warningf("Could not write proxy port file: %v", err)
		}
	}

	return pid, ports, nil
}

// buildProxyArgs constructs the command-line arguments for the proxy process.
func buildProxyArgs(egress model.EgressPolicy, httpPort, dnsPort int) []string {
	args := []string{
		"--no-log",
		"internal-vm-proxy",
		"--port", strconv.Itoa(httpPort),
		"--dns-port", strconv.Itoa(dnsPort),
		"--default-policy", egress.Default,
	}

	for _, r := range egress.Rules {
		ruleJSON := fmt.Sprintf(`{"action":%q,"domain":%q}`, r.Action, r.Domain)
		args = append(args, "--rule", ruleJSON)
	}

	return args
}

// killProxy kills the proxy process by reading the PID file.
func (e *Engine) killProxy(vmDir string) error {
	pidPath := filepath.Join(vmDir, conventions.ProxyPIDFile)
	pidData, err := os.ReadFile(pidPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil // No PID file, no proxy running.
		}
		return fmt.Errorf("could not read proxy PID file: %w", err)
	}

	pid, err := strconv.Atoi(strings.TrimSpace(string(pidData)))
	if err != nil {
		return fmt.Errorf("invalid proxy PID: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return nil
	}

	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if err == os.ErrProcessDone {
			return nil
		}
		return nil
	}

	_ = proc.Signal(syscall.SIGKILL)

	return nil
}

// readProxyPorts reads the allocated proxy ports from the port file.
func readProxyPorts(vmDir string) (ProxyPorts, error) {
	portPath := filepath.Join(vmDir, conventions.ProxyPortFile)
	data, err := os.ReadFile(portPath)
	if err != nil {
		return ProxyPorts{}, fmt.Errorf("could not read proxy port file: %w", err)
	}

	var ports ProxyPorts
	if err := json.Unmarshal(data, &ports); err != nil {
		return ProxyPorts{}, fmt.Errorf("could not parse proxy port file: %w", err)
	}

	return ports, nil
}

// getFreePort returns an available TCP port on localhost.
func getFreePort() (int, error) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := listener.Addr().(*net.TCPAddr).Port
	listener.Close()
	return port, nil
}

// getFreeUDPPort returns an available UDP port on localhost.
func getFreeUDPPort() (int, error) {
	pc, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		return 0, err
	}
	port := pc.LocalAddr().(*net.UDPAddr).Port
	pc.Close()
	return port, nil
}
