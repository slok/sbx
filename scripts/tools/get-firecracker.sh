#!/bin/bash
set -o errexit
set -o nounset

# Download latest release
ARCH=$(uname -m)
VERSION="v1.6.0"
curl -L -o firecracker.tgz https://github.com/firecracker-microvm/firecracker/releases/download/${VERSION}/firecracker-${VERSION}-${ARCH}.tgz

# Extract and install
tar -xzf firecracker.tgz
sudo mv release-${VERSION}-${ARCH}/firecracker-${VERSION}-${ARCH} /usr/local/bin/firecracker
sudo mv release-${VERSION}-${ARCH}/jailer-${VERSION}-${ARCH} /usr/local/bin/jailer
chmod +x /usr/local/bin/firecracker /usr/local/bin/jailer

# Cleanup
rm -rf firecracker.tgz release-${VERSION}-${ARCH}

# Verify
firecracker --version