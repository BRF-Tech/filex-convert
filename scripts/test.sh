#!/usr/bin/env bash
set -euo pipefail
export GOTOOLCHAIN=local
cd "$(dirname "$0")/.."
gofmt -l . | grep -v '^$' && { echo "gofmt: files above need formatting"; exit 1; } || true
go vet ./...
go test ./... "$@"
