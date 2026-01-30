#!/usr/bin/env sh

set -o errexit
set -o nounset

echo "Running integration tests..."
go test -v ./test/integration/...
