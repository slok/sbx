#!/usr/bin/env sh

set -o errexit
set -o nounset

echo "Running integration tests..."

if [ -d ./test/integration ]; then
    # SDK lib integration tests need CAP_NET_ADMIN for TAP device creation
    # (they run the Firecracker engine in-process, unlike CLI tests which
    # shell out to the setcap'd sbx binary). Run them with sudo -E to
    # preserve environment variables (SBX_INTEGRATION, etc.) and PATH.
    if [ -d ./test/integration/lib ]; then
        echo "Running SDK lib integration tests (with sudo for CAP_NET_ADMIN)..."
        sudo -E "$(command -v go)" test -v -count=1 -timeout 600s ./test/integration/lib/...
    fi

    # CLI integration tests use the sbx binary (which has setcap) so they
    # don't need elevated privileges.
    if [ -d ./test/integration/sbx ]; then
        echo "Running CLI integration tests..."
        go test -v -count=1 -timeout 600s ./test/integration/sbx/...
    fi

    # Proxy integration tests only need the sbx binary (no VM infrastructure).
    # Gated by SBX_INTEGRATION_PROXY=true.
    if [ -d ./test/integration/proxy ]; then
        echo "Running proxy integration tests..."
        go test -v -count=1 -timeout 60s ./test/integration/proxy/...
    fi
else
    echo "No integration tests directory found, skipping."
fi
