#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
# The compiler go.mod pins (`toolchain goX.Y.Z`), as in scripts/build.sh.
GOTOOLCHAIN="$(sed -n 's/^toolchain //p' go.mod)"
export GOTOOLCHAIN="${GOTOOLCHAIN:?go.mod has no toolchain line}"
gofmt -l . | grep -v '^$' && { echo "gofmt: files above need formatting"; exit 1; } || true
go vet "$@"
go test -count=1 "$@"
