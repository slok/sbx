// Package lib provides a Go SDK for managing sbx sandboxes programmatically.
//
// This package allows applications to create, manage, and interact with
// sandboxes without shelling out to the sbx CLI binary. It is useful for
// scripting, automation, and building tools on top of sbx.
//
// # Quick Start
//
// Create a client, manage a sandbox lifecycle, and execute commands:
//
//	client, err := lib.New(ctx, lib.Config{})
//	if err != nil {
//	    log.Fatal(err)
//	}
//	defer client.Close()
//
//	// Create a sandbox.
//	sb, err := client.CreateSandbox(ctx, lib.CreateSandboxOpts{
//	    Name:   "my-sandbox",
//	    Engine: lib.EngineFirecracker,
//	    Firecracker: &lib.FirecrackerConfig{
//	        RootFS:      "/path/to/rootfs.ext4",
//	        KernelImage: "/path/to/vmlinux",
//	    },
//	    Resources: lib.Resources{VCPUs: 2, MemoryMB: 1024, DiskGB: 10},
//	})
//
//	// Start, exec, stop.
//	client.StartSandbox(ctx, "my-sandbox", nil)
//	client.Exec(ctx, "my-sandbox", []string{"echo", "hello"}, nil)
//	client.StopSandbox(ctx, "my-sandbox")
//	client.RemoveSandbox(ctx, "my-sandbox", false)
//
// # Engines
//
// The SDK supports two engine types:
//
//   - [EngineFirecracker]: Real Firecracker microVMs. Requires KVM, kernel and
//     rootfs images, and appropriate capabilities (CAP_NET_ADMIN).
//   - [EngineFake]: In-memory fake engine for unit testing. No real infrastructure
//     needed. Set [Config].Engine to [EngineFake] to use it.
//
// # Error Handling
//
// All methods return errors that can be inspected with [errors.Is]:
//
//   - [ErrNotFound]: Resource does not exist.
//   - [ErrAlreadyExists]: Resource with the same name already exists.
//   - [ErrNotValid]: Invalid input or operation (e.g. stopping a non-running sandbox).
//
// # Testing
//
// Use [EngineFake] and a temporary database path to write tests without
// real infrastructure:
//
//	client, _ := lib.New(ctx, lib.Config{
//	    DBPath: filepath.Join(t.TempDir(), "test.db"),
//	    Engine: lib.EngineFake,
//	})
//	defer client.Close()
//
// # Thread Safety
//
// A [Client] is safe for concurrent use from multiple goroutines. The underlying
// storage uses SQLite with WAL mode, and engines are created per-operation.
package lib
