# Image Management

SBX uses a release-based image system. Pre-built VM images (kernel + rootfs) are published as GitHub Releases in a companion repository, and the `sbx image` commands manage downloading and using them locally.

## Architecture

### sbx-images Repository

The [slok/sbx-images](https://github.com/slok/sbx-images) repository builds and publishes versioned image bundles. Each release contains:

- **Kernel binary** (`vmlinux-x86_64`) - Linux kernel built for Firecracker
- **Root filesystem** (`rootfs-x86_64.ext4`) - Alpine Linux ext4 image
- **Manifest** (`manifest.json`) - Metadata describing all artifacts

Releases are tagged manually (e.g. `v0.1.0`, `v0.1.0-rc.1`) and built via GitHub Actions.

### Manifest Format

```json
{
  "schema_version": 1,
  "version": "v0.1.0",
  "artifacts": {
    "x86_64": {
      "kernel": { "file": "vmlinux-x86_64", "version": "6.1.155", "source": "firecracker-ci/v1.15", "size_bytes": 44279576 },
      "rootfs": { "file": "rootfs-x86_64.ext4", "distro": "alpine", "distro_version": "3.23", "profile": "balanced", "size_bytes": 679034880 }
    }
  },
  "firecracker": { "version": "v1.14.1", "source": "github.com/firecracker-microvm/firecracker" },
  "build": { "date": "2026-02-08T09:54:17Z", "commit": "adc9bc1" }
}
```

The `schema_version` field identifies the manifest format. The sbx client validates it and will error with a clear message if the version is unsupported (e.g., after a breaking manifest format change). Manifests without `schema_version` (pre-versioning) are treated as schema version 1.

### Local Storage Layout

Downloaded images are stored at `~/.sbx/images/<version>/`:

```
~/.sbx/images/
  v0.1.0/
    vmlinux-x86_64       # Kernel binary
    rootfs-x86_64.ext4   # Root filesystem
    firecracker           # Firecracker binary (from upstream)
```

### Firecracker Binary

The Firecracker binary is **not** bundled in `sbx-images`. During `sbx image pull`, it is downloaded from the official [firecracker-microvm/firecracker](https://github.com/firecracker-microvm/firecracker) GitHub releases based on the version specified in the manifest.

## Commands

### List available releases

```bash
sbx image list
sbx image list --format json
```

Shows all releases from the GitHub repository, marking which ones are installed locally.

### Pull an image

```bash
sbx image pull v0.1.0
sbx image pull v0.1.0 --force   # Re-download even if installed
```

Downloads kernel, rootfs, and Firecracker binary for the specified version.

### Remove an image

```bash
sbx image rm v0.1.0
```

Deletes the local image directory for the specified version.

### Inspect an image

```bash
sbx image inspect v0.1.0
sbx image inspect v0.1.0 --format json
```

Fetches and displays the manifest for the specified version.

### Create sandbox from image

```bash
sbx create --name my-vm --engine firecracker --from-image v0.1.0
```

Uses the kernel, rootfs, and Firecracker binary from a pulled image. The image must be pulled first (`sbx image pull`).

The `--from-image` flag conflicts with `--firecracker-root-fs` and `--firecracker-kernel`.

`--from-image` works with both pulled releases and snapshot images created via `sbx snapshot`. See [commands.md](commands.md) for the full CLI reference.

## Global Flags

All image subcommands support:

- `--repo <owner/name>` - GitHub repository (default: `slok/sbx-images`)
- `--images-dir <path>` - Local storage directory (default: `~/.sbx/images`)

## Related

- [Commands Reference](commands.md) — Full CLI reference for image commands
- [Security](security.md) — Egress filtering and security model
