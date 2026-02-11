package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/slok/sbx/pkg/lib"
)

const (
	defaultName  = "opencode-sandbox"
	defaultImage = "v0.1.0-rc.2"
	defaultPort  = 3000
	defaultVCPUs = 1
	defaultMem   = 1024
	defaultDisk  = 1

	opencodeBin = "/root/.opencode/bin/opencode"

	healthRetryInterval = 2 * time.Second
	healthTimeout       = 60 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	// Parse flags.
	name := flag.String("name", defaultName, "Sandbox name")
	image := flag.String("image", defaultImage, "SBX image version (must be pre-pulled)")
	port := flag.Int("port", defaultPort, "OpenCode web server port")
	apiKey := flag.String("api-key", os.Getenv("ANTHROPIC_API_KEY"), "Anthropic API key (default: $ANTHROPIC_API_KEY)")
	flag.Parse()

	if *apiKey == "" {
		return fmt.Errorf("ANTHROPIC_API_KEY not set: use --api-key or export ANTHROPIC_API_KEY")
	}

	ctx := context.Background()

	// 1. Create SBX client.
	fmt.Println("Creating SBX client...")
	client, err := lib.New(ctx, lib.Config{})
	if err != nil {
		return fmt.Errorf("creating sbx client: %w", err)
	}
	defer client.Close()

	// 2. Create sandbox.
	fmt.Printf("Creating sandbox %q from image %s...\n", *name, *image)
	sb, err := client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name:      *name,
		Engine:    lib.EngineFirecracker,
		FromImage: *image,
		Resources: lib.Resources{
			VCPUs:    defaultVCPUs,
			MemoryMB: defaultMem,
			DiskGB:   defaultDisk,
		},
	})
	if err != nil {
		return fmt.Errorf("creating sandbox: %w", err)
	}
	fmt.Printf("Sandbox created: %s (ID: %s)\n", sb.Name, sb.ID)

	// 3. Start sandbox with ANTHROPIC_API_KEY injected.
	fmt.Println("Starting sandbox...")
	sb, err = client.StartSandbox(ctx, *name, &lib.StartSandboxOpts{
		Env: map[string]string{
			"ANTHROPIC_API_KEY": *apiKey,
		},
	})
	if err != nil {
		return fmt.Errorf("starting sandbox: %w", err)
	}
	fmt.Printf("Sandbox running (started at %s)\n", sb.StartedAt.Format(time.RFC3339))

	// 4. Install OpenCode.
	fmt.Println("Installing OpenCode...")
	result, err := client.Exec(ctx, *name,
		[]string{"bash", "-c", "curl -fsSL https://opencode.ai/install | bash"},
		&lib.ExecOpts{
			Stdout: os.Stdout,
			Stderr: os.Stderr,
		},
	)
	if err != nil {
		return fmt.Errorf("installing opencode: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("opencode install failed with exit code %d", result.ExitCode)
	}
	fmt.Println("OpenCode installed.")

	// 5. Start OpenCode web server in background.
	fmt.Printf("Starting OpenCode web server on port %d...\n", *port)

	opencodeConfig := map[string]any{
		"$schema":    "https://opencode.ai/config.json",
		"permission": "allow",
	}
	configJSON, err := json.Marshal(opencodeConfig)
	if err != nil {
		return fmt.Errorf("marshaling opencode config: %w", err)
	}

	startCmd := fmt.Sprintf(
		"nohup %s web --port %d --hostname 0.0.0.0 >/tmp/opencode.log 2>&1 &",
		opencodeBin, *port,
	)
	result, err = client.Exec(ctx, *name,
		[]string{"bash", "-c", startCmd},
		&lib.ExecOpts{
			Env: map[string]string{
				"OPENCODE_CONFIG_CONTENT": string(configJSON),
			},
		},
	)
	if err != nil {
		return fmt.Errorf("starting opencode web: %w", err)
	}
	if result.ExitCode != 0 {
		return fmt.Errorf("opencode web start failed with exit code %d", result.ExitCode)
	}

	// 6. Wait for OpenCode to be ready.
	fmt.Println("Waiting for OpenCode to be ready...")
	if err := waitForHealth(ctx, client, *name, *port); err != nil {
		// Dump logs for debugging.
		fmt.Fprintln(os.Stderr, "--- opencode logs ---")
		_, _ = client.Exec(ctx, *name,
			[]string{"cat", "/tmp/opencode.log"},
			&lib.ExecOpts{Stdout: os.Stderr, Stderr: os.Stderr},
		)
		fmt.Fprintln(os.Stderr, "--- end logs ---")
		return fmt.Errorf("waiting for opencode: %w", err)
	}

	// 7. Print instructions.
	fmt.Println()
	fmt.Println("========================================")
	fmt.Println(" OpenCode is running in sandbox!")
	fmt.Println("========================================")
	fmt.Println()
	fmt.Println("To access the web UI, forward the port:")
	fmt.Println()
	fmt.Printf("  sbx forward %s %d\n", *name, *port)
	fmt.Println()
	fmt.Printf("Then open: http://localhost:%d\n", *port)
	fmt.Println()
	fmt.Println("To get a shell inside the sandbox:")
	fmt.Println()
	fmt.Printf("  sbx shell %s\n", *name)
	fmt.Println()
	fmt.Println("To cleanup when done:")
	fmt.Println()
	fmt.Printf("  sbx rm --force %s\n", *name)
	fmt.Println()

	return nil
}

func waitForHealth(ctx context.Context, client *lib.Client, name string, port int) error {
	healthURL := fmt.Sprintf("http://localhost:%d/global/health", port)
	deadline := time.Now().Add(healthTimeout)

	for {
		if time.Now().After(deadline) {
			return fmt.Errorf("timeout after %s waiting for OpenCode health endpoint", healthTimeout)
		}

		var stdout bytes.Buffer
		result, err := client.Exec(ctx, name,
			[]string{"curl", "-sf", healthURL},
			&lib.ExecOpts{Stdout: &stdout},
		)
		if err == nil && result.ExitCode == 0 {
			fmt.Printf("OpenCode ready: %s\n", stdout.String())
			return nil
		}

		time.Sleep(healthRetryInterval)
	}
}
