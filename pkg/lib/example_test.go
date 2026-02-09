package lib_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/slok/sbx/pkg/lib"
)

// This example shows how to create a client using the fake engine for testing.
func Example_testing() {
	ctx := context.Background()

	// Use a temp directory and fake engine for testing.
	dir, err := os.MkdirTemp("", "sbx-example-test-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	client, err := lib.New(ctx, lib.Config{
		DBPath: filepath.Join(dir, "sbx.db"),
		Engine: lib.EngineFake,
	})
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Create a sandbox.
	sb, err := client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name:   "test-sandbox",
		Engine: lib.EngineFake,
		Resources: lib.Resources{
			VCPUs:    1,
			MemoryMB: 512,
			DiskGB:   5,
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("Created: %s (status: %s)\n", sb.Name, sb.Status)

	// Output:
	// Created: test-sandbox (status: stopped)
}

// This example shows the full sandbox lifecycle: create, start, exec, stop, remove.
func Example_lifecycle() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "sbx-example-lifecycle-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	client, err := lib.New(ctx, lib.Config{
		DBPath: filepath.Join(dir, "sbx.db"),
		Engine: lib.EngineFake,
	})
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Create.
	_, err = client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name:      "my-sandbox",
		Engine:    lib.EngineFake,
		Resources: lib.Resources{VCPUs: 2, MemoryMB: 1024, DiskGB: 10},
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("1. Created")

	// Start.
	_, err = client.StartSandbox(ctx, "my-sandbox", nil)
	if err != nil {
		panic(err)
	}
	fmt.Println("2. Started")

	// Exec a command.
	result, err := client.Exec(ctx, "my-sandbox", []string{"echo", "hello"}, nil)
	if err != nil {
		panic(err)
	}
	fmt.Printf("3. Exec exit code: %d\n", result.ExitCode)

	// Stop.
	_, err = client.StopSandbox(ctx, "my-sandbox")
	if err != nil {
		panic(err)
	}
	fmt.Println("4. Stopped")

	// Remove.
	_, err = client.RemoveSandbox(ctx, "my-sandbox", false)
	if err != nil {
		panic(err)
	}
	fmt.Println("5. Removed")

	// Output:
	// 1. Created
	// 2. Started
	// 3. Exec exit code: 0
	// 4. Stopped
	// 5. Removed
}

// This example shows how to capture command output using ExecOpts.
func ExampleClient_Exec() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "sbx-example-exec-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	client, err := lib.New(ctx, lib.Config{
		DBPath: filepath.Join(dir, "sbx.db"),
		Engine: lib.EngineFake,
	})
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Setup: create and start a sandbox.
	_, _ = client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name:      "exec-demo",
		Engine:    lib.EngineFake,
		Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
	})
	_, _ = client.StartSandbox(ctx, "exec-demo", nil)

	// Exec with captured stdout.
	var stdout bytes.Buffer
	result, err := client.Exec(ctx, "exec-demo", []string{"echo", "hello"}, &lib.ExecOpts{
		Stdout: &stdout,
		Env:    map[string]string{"MY_VAR": "my-value"},
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("exit code: %d\n", result.ExitCode)
	// Note: fake engine doesn't actually run commands, so stdout is empty.

	// Cleanup.
	_, _ = client.RemoveSandbox(ctx, "exec-demo", true)

	// Output:
	// exit code: 0
}

// This example shows how to start a sandbox with session environment variables.
func ExampleClient_StartSandbox() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "sbx-example-start-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	client, err := lib.New(ctx, lib.Config{
		DBPath: filepath.Join(dir, "sbx.db"),
		Engine: lib.EngineFake,
	})
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Create.
	_, _ = client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name:      "env-demo",
		Engine:    lib.EngineFake,
		Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
	})

	// Start with session environment variables.
	sb, err := client.StartSandbox(ctx, "env-demo", &lib.StartSandboxOpts{
		Env: map[string]string{
			"APP_ENV":   "production",
			"LOG_LEVEL": "debug",
		},
	})
	if err != nil {
		panic(err)
	}

	fmt.Printf("status: %s\n", sb.Status)

	// Cleanup.
	_, _ = client.RemoveSandbox(ctx, "env-demo", true)

	// Output:
	// status: running
}

// This example shows how to list sandboxes with a status filter.
func ExampleClient_ListSandboxes() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "sbx-example-list-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	client, err := lib.New(ctx, lib.Config{
		DBPath: filepath.Join(dir, "sbx.db"),
		Engine: lib.EngineFake,
	})
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Create some sandboxes.
	_, _ = client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name: "sb-1", Engine: lib.EngineFake,
		Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
	})
	_, _ = client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name: "sb-2", Engine: lib.EngineFake,
		Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
	})

	// Start only one.
	_, _ = client.StartSandbox(ctx, "sb-1", nil)

	// List all.
	all, err := client.ListSandboxes(ctx, nil)
	if err != nil {
		panic(err)
	}
	fmt.Printf("total: %d\n", len(all))

	// List only running.
	running := lib.SandboxStatusRunning
	filtered, err := client.ListSandboxes(ctx, &lib.ListSandboxesOpts{Status: &running})
	if err != nil {
		panic(err)
	}
	fmt.Printf("running: %d\n", len(filtered))

	// Cleanup.
	_, _ = client.RemoveSandbox(ctx, "sb-1", true)
	_, _ = client.RemoveSandbox(ctx, "sb-2", true)

	// Output:
	// total: 2
	// running: 1
}

// This example shows how to handle SDK errors using errors.Is.
func Example_errorHandling() {
	ctx := context.Background()

	dir, err := os.MkdirTemp("", "sbx-example-errors-*")
	if err != nil {
		panic(err)
	}
	defer os.RemoveAll(dir)

	client, err := lib.New(ctx, lib.Config{
		DBPath: filepath.Join(dir, "sbx.db"),
		Engine: lib.EngineFake,
	})
	if err != nil {
		panic(err)
	}
	defer client.Close()

	// Try to get a non-existent sandbox.
	_, err = client.GetSandbox(ctx, "does-not-exist")
	if errors.Is(err, lib.ErrNotFound) {
		fmt.Println("sandbox not found (expected)")
	}

	// Create and try to create duplicate.
	_, _ = client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name: "dup", Engine: lib.EngineFake,
		Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
	})
	_, err = client.CreateSandbox(ctx, lib.CreateSandboxOpts{
		Name: "dup", Engine: lib.EngineFake,
		Resources: lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
	})
	if errors.Is(err, lib.ErrAlreadyExists) {
		fmt.Println("duplicate name (expected)")
	}

	// Try to stop a non-running sandbox.
	_, err = client.StopSandbox(ctx, "dup")
	if errors.Is(err, lib.ErrNotValid) {
		fmt.Println("invalid operation (expected)")
	}

	// Cleanup.
	_, _ = client.RemoveSandbox(ctx, "dup", true)

	// Output:
	// sandbox not found (expected)
	// duplicate name (expected)
	// invalid operation (expected)
}

// This example shows a Firecracker sandbox configuration (will not actually
// run without real infrastructure, but demonstrates the API).
func ExampleCreateSandboxOpts() {
	// Firecracker sandbox configuration.
	opts := lib.CreateSandboxOpts{
		Name:   "dev-environment",
		Engine: lib.EngineFirecracker,
		Firecracker: &lib.FirecrackerConfig{
			RootFS:      "/home/user/.sbx/images/v0.1.0/rootfs.ext4",
			KernelImage: "/home/user/.sbx/images/v0.1.0/vmlinux",
		},
		Resources: lib.Resources{
			VCPUs:    2,
			MemoryMB: 2048,
			DiskGB:   20,
		},
	}

	fmt.Printf("name=%s engine=%s vcpus=%.0f mem=%dMB disk=%dGB\n",
		opts.Name, opts.Engine,
		opts.Resources.VCPUs, opts.Resources.MemoryMB, opts.Resources.DiskGB)

	// Output:
	// name=dev-environment engine=firecracker vcpus=2 mem=2048MB disk=20GB
}
