#!/usr/bin/env bash
# Maintainer step before tagging a release:
#   1. regenerate the embedded manifest (drops the wasm block),
#   2. build plugin.wasm reproducibly (-trimpath, pinned Go),
#   3. write its sha256 into filex-app.json,
#   4. regenerate MATRIX.md,
# then review, commit ("release: vX.Y.Z"), tag vX.Y.Z and push. The release
# workflow rebuilds the module and refuses to publish unless its hash equals
# the committed one.
set -euo pipefail
export GOTOOLCHAIN=local
cd "$(dirname "$0")/.."
go generate ./...
bash scripts/build.sh
go run ./tools/setsha plugin.wasm
go run ./tools/matrix > MATRIX.md
go test -count=1 ./...
echo "filex-app.json now carries the sha256 of plugin.wasm; commit and tag v$(sed -n 's/^  "version": "\(.*\)",$/\1/p' filex-app.json)"
