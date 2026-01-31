#!/bin/bash
set -o errexit
set -o nounset

# Download latest release
ARCH=$(uname -m)
VERSION="v1.14.1"
OUT_DIR="./bin"
curl -L -o firecracker.tgz https://github.com/firecracker-microvm/firecracker/releases/download/${VERSION}/firecracker-${VERSION}-${ARCH}.tgz

# Extract and install
tar -xzf firecracker.tgz
mkdir -p ${OUT_DIR}
mv release-${VERSION}-${ARCH}/firecracker-${VERSION}-${ARCH} ${OUT_DIR}/firecracker
mv release-${VERSION}-${ARCH}/jailer-${VERSION}-${ARCH} ${OUT_DIR}/jailer
chmod +x ${OUT_DIR}/firecracker ${OUT_DIR}/jailer

# Cleanup
rm -rf firecracker.tgz release-${VERSION}-${ARCH}

# Verify
${OUT_DIR}/firecracker --version