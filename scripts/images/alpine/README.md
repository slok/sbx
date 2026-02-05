# Alpine RootFS Builder

This directory contains SBX-owned scripts for building Alpine rootfs images for Firecracker.

Static files copied into the image live under `scripts/images/alpine/files/`.

## Build

```bash
./scripts/images/alpine/build-rootfs.sh
```

Defaults:

- profile: `balanced`
- branch: `v3.23`
- output image: `~/.sbx/images/alpine-agentic.ext4`

## Profiles

- `minimal`: lightweight interactive/dev shell
- `balanced`: recommended default for agentic workloads
- `heavy`: broad polyglot toolchain (includes `nodejs/npm`)

## Requirements

- Linux host
- `sudo` privileges (mount/chroot/permissions)
- `git` (script clones `alpine-make-rootfs` automatically if not installed)
