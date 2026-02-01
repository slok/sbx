#!/bin/bash
# setup-firecracker-images.sh
# Downloads Firecracker kernel and rootfs images for testing
#
# Usage: ./scripts/setup-firecracker-images.sh [--target-dir DIR]

set -euo pipefail

# Default target directory
TARGET_DIR="${HOME}/.sbx/images"

# Firecracker CI artifacts (these are the official test images)
# See: https://github.com/firecracker-microvm/firecracker/blob/main/docs/getting-started.md
FC_VERSION="v1.10.1"
CI_VERSION="v1.10"
ARCH=$(uname -m)

# URLs for Firecracker CI artifacts
KERNEL_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/${CI_VERSION}/${ARCH}/vmlinux-6.1.102"
ROOTFS_URL="https://s3.amazonaws.com/spec.ccfc.min/firecracker-ci/${CI_VERSION}/${ARCH}/ubuntu-22.04.ext4"
FIRECRACKER_URL="https://github.com/firecracker-microvm/firecracker/releases/download/${FC_VERSION}/firecracker-${FC_VERSION}-${ARCH}.tgz"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

log_info() {
    echo -e "${GREEN}[INFO]${NC} $1"
}

log_warn() {
    echo -e "${YELLOW}[WARN]${NC} $1"
}

log_error() {
    echo -e "${RED}[ERROR]${NC} $1"
}

# Parse arguments
while [[ $# -gt 0 ]]; do
    case $1 in
        --target-dir)
            TARGET_DIR="$2"
            shift 2
            ;;
        --help|-h)
            echo "Usage: $0 [--target-dir DIR]"
            echo ""
            echo "Downloads Firecracker kernel and rootfs images for testing."
            echo ""
            echo "Options:"
            echo "  --target-dir DIR  Target directory for images (default: ~/.sbx/images)"
            echo ""
            exit 0
            ;;
        *)
            log_error "Unknown option: $1"
            exit 1
            ;;
    esac
done

# Check architecture
if [[ "$ARCH" != "x86_64" && "$ARCH" != "aarch64" ]]; then
    log_error "Unsupported architecture: $ARCH. Firecracker only supports x86_64 and aarch64."
    exit 1
fi

log_info "Architecture: $ARCH"
log_info "Target directory: $TARGET_DIR"

# Create target directory
mkdir -p "$TARGET_DIR"
mkdir -p ./bin

# Download kernel
KERNEL_PATH="$TARGET_DIR/vmlinux"
if [[ -f "$KERNEL_PATH" ]]; then
    log_info "Kernel already exists at $KERNEL_PATH"
else
    log_info "Downloading kernel..."
    curl -fSL --progress-bar -o "$KERNEL_PATH" "$KERNEL_URL"
    chmod 644 "$KERNEL_PATH"
    log_info "Kernel downloaded to $KERNEL_PATH"
fi

# Download rootfs
ROOTFS_PATH="$TARGET_DIR/ubuntu-22.04.ext4"
if [[ -f "$ROOTFS_PATH" ]]; then
    log_info "Rootfs already exists at $ROOTFS_PATH"
else
    log_info "Downloading rootfs..."
    curl -fSL --progress-bar -o "$ROOTFS_PATH" "$ROOTFS_URL"
    chmod 644 "$ROOTFS_PATH"
    log_info "Rootfs downloaded to $ROOTFS_PATH"
fi

# Download Firecracker binary
FC_BIN_PATH="./bin/firecracker"
if [[ -f "$FC_BIN_PATH" ]]; then
    log_info "Firecracker binary already exists at $FC_BIN_PATH"
else
    log_info "Downloading Firecracker ${FC_VERSION}..."
    TMP_DIR=$(mktemp -d)
    trap "rm -rf $TMP_DIR" EXIT
    
    curl -fSL --progress-bar -o "$TMP_DIR/firecracker.tgz" "$FIRECRACKER_URL"
    tar -xzf "$TMP_DIR/firecracker.tgz" -C "$TMP_DIR"
    
    # Find and copy the firecracker binary
    FC_EXTRACTED="$TMP_DIR/release-${FC_VERSION}-${ARCH}/firecracker-${FC_VERSION}-${ARCH}"
    if [[ -f "$FC_EXTRACTED" ]]; then
        cp "$FC_EXTRACTED" "$FC_BIN_PATH"
        chmod +x "$FC_BIN_PATH"
        log_info "Firecracker binary installed to $FC_BIN_PATH"
    else
        log_error "Could not find firecracker binary in archive"
        exit 1
    fi
fi

# Verify files
log_info ""
log_info "=== Verification ==="
log_info ""

verify_file() {
    local path=$1
    local name=$2
    if [[ -f "$path" ]]; then
        local size=$(du -h "$path" | cut -f1)
        echo -e "  ${GREEN}OK${NC} $name ($size)"
    else
        echo -e "  ${RED}MISSING${NC} $name"
    fi
}

verify_file "$KERNEL_PATH" "Kernel"
verify_file "$ROOTFS_PATH" "Rootfs"
verify_file "$FC_BIN_PATH" "Firecracker binary"

# Check KVM
log_info ""
log_info "=== System Checks ==="
log_info ""

if [[ -w /dev/kvm ]]; then
    echo -e "  ${GREEN}OK${NC} /dev/kvm is accessible"
else
    echo -e "  ${YELLOW}WARN${NC} /dev/kvm is not accessible (need root or kvm group)"
fi

if command -v iptables &> /dev/null; then
    echo -e "  ${GREEN}OK${NC} iptables is available"
else
    echo -e "  ${YELLOW}WARN${NC} iptables not found"
fi

if command -v debugfs &> /dev/null; then
    echo -e "  ${GREEN}OK${NC} debugfs is available (e2fsprogs)"
else
    echo -e "  ${YELLOW}WARN${NC} debugfs not found (install e2fsprogs for SSH key injection)"
fi

log_info ""
log_info "=== Example sandbox.yaml ==="
log_info ""
cat << 'EOF'
name: my-sandbox
engine:
  firecracker:
    kernel: ~/.sbx/images/vmlinux
    rootfs: ~/.sbx/images/ubuntu-22.04.ext4
resources:
  vcpus: 2
  memory_mb: 1024
  disk_gb: 10
EOF

log_info ""
log_info "Setup complete! You can now create Firecracker sandboxes."
