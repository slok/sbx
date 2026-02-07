#!/usr/bin/env sh

set -o errexit
set -o nounset

echo "Running integration tests..."

if [ -d ./test/integration ]; then
    go test -v -count=1 -timeout 600s ./test/integration/...
else
    echo "No integration tests directory found, skipping."
fi
