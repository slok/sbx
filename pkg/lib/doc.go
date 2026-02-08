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
// # File Operations
//
// Copy files between the host and a running sandbox:
//
//	client.CopyTo(ctx, "my-sandbox", "/local/file.txt", "/remote/file.txt")
//	client.CopyFrom(ctx, "my-sandbox", "/remote/file.txt", "/local/file.txt")
//
// # Port Forwarding
//
// Forward local ports to a running sandbox. The method blocks until context
// cancellation:
//
//	ctx, cancel := context.WithCancel(context.Background())
//	go func() {
//	    time.Sleep(10 * time.Second)
//	    cancel()
//	}()
//	client.Forward(ctx, "my-sandbox", []lib.PortMapping{
//	    {LocalPort: 8080, RemotePort: 80},
//	})
//
// # Snapshots
//
// Create point-in-time snapshots of stopped sandboxes and restore from them:
//
//	client.StopSandbox(ctx, "my-sandbox")
//	snap, _ := client.CreateSnapshot(ctx, "my-sandbox", nil)
//	client.CreateSandbox(ctx, lib.CreateSandboxOpts{
//	    Name:         "from-snapshot",
//	    Engine:       lib.EngineFirecracker,
//	    Firecracker:  &lib.FirecrackerConfig{KernelImage: "/path/to/vmlinux"},
//	    Resources:    lib.Resources{VCPUs: 1, MemoryMB: 512, DiskGB: 5},
//	    FromSnapshot: snap.Name,
//	})
//
// # Image Management
//
// List, pull, inspect, and remove image releases from the registry:
//
//	images, _ := client.ListImages(ctx)
//	result, _ := client.PullImage(ctx, "v0.1.0", nil)
//	manifest, _ := client.InspectImage(ctx, "v0.1.0")
//	client.RemoveImage(ctx, "v0.1.0")
//
// # Health Checks
//
// Run preflight checks to verify the engine environment:
//
//	results, _ := client.Doctor(ctx)
//	for _, r := range results {
//	    fmt.Printf("%s: %s (%s)\n", r.ID, r.Message, r.Status)
//	}
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
