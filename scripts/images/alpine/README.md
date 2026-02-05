# Alpine RootFS Builder

This directory contains SBX-owned tooling to build Alpine rootfs images for Firecracker.

The goal is to keep VM boot/runtime behavior deterministic by baking stable OS-level setup into the image, while keeping sandbox-specific values dynamic at runtime.

## Layout

- `build-rootfs.sh`: main builder entrypoint.
- `profiles/*.txt`: package sets by usage profile.
- `files/`: static files copied into the image as part of provisioning.

## Build

```bash
./scripts/images/alpine/build-rootfs.sh
```

Defaults:

- profile: `balanced`
- branch: `v3.23`
- output image: `~/.sbx/images/alpine-agentic.ext4`
- post-build shrinking: enabled

Useful options:

```bash
./scripts/images/alpine/build-rootfs.sh --profile heavy
./scripts/images/alpine/build-rootfs.sh --image-name alpine-heavy.ext4
./scripts/images/alpine/build-rootfs.sh --no-shrink
```

## Profiles

- `minimal`: lightweight shell/dev baseline.
- `balanced`: recommended default for agentic workloads.
- `heavy`: broader polyglot tooling (includes `nodejs/npm`).

All profiles include `apk-tools` so package management is always available in the guest.

## Design Decisions

### 1) Bake stable rootfs prep into the image

We intentionally moved several Firecracker create-time patching steps into image build provisioning:

- DNS defaults (`/etc/resolv.conf`)
- init wrapper (`/usr/sbin/sbx-init`)
- session env hooks (`/etc/sbx/session-env.sh`, `/etc/profile.d/sbx-session-env.sh`, `/root/.ssh/rc`)

Why:

- Fewer runtime mutations on copied ext4 images.
- Faster/simpler sandbox create flow.
- Less `debugfs`-based patching logic to maintain.
- More reproducible behavior across environments.

What remains dynamic in runtime code:

- SSH public key injection (`authorized_keys`) because it depends on host key material.
- Disk sizing (`resize_rootfs`) because it depends on sandbox config.
- Session env values written on `sbx start` to `/etc/sbx/session-env.sh`.

### 2) Keep provisioning files as regular tracked files

Files under `files/` are normal versioned assets, not heredocs embedded in long shell blocks.

Why:

- Easier review and change history.
- Lower risk of quoting/escaping mistakes.
- Clear ownership of in-guest file contents.

### 3) Use sudo only where privileges are required

The script runs as user and elevates only for operations that need it (`mount`, `chroot`, protected writes).

Why:

- Avoids root-owned outputs/workspace artifacts.
- Keeps default output in the invoking user's `~/.sbx/images`.

### 4) Over-allocate then shrink image size

We size with configurable overhead for safe copy/provisioning, then shrink ext4 at the end (`e2fsck + resize2fs -M + truncate`).

Why:

- Prevents mid-build `No space left on device` failures.
- Produces a smaller final artifact.

## Static Files Installed Into the Image

- `etc/resolv.conf`: baseline DNS servers for network resolution.
- `usr/sbin/sbx-init`: mounts `devpts` then hands off to real init.
- `etc/sbx/session-env.sh`: managed env script target updated at `sbx start`.
- `etc/profile.d/sbx-session-env.sh`: loads session env for shell sessions.
- `root/.ssh/rc`: loads session env for ssh command sessions.
- `usr/local/bin/sbx-start-hooks`: optional hook runner for future start-time extensions.

## Requirements

- Linux host
- `sudo` privileges (mount/chroot/permissions)
- `git` (builder clones `alpine-make-rootfs` automatically if missing)
