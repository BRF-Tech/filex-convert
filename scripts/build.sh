#!/usr/bin/env bash
# Builds plugin.wasm with stock Go, prints its sha256 and enforces the size
# gate: warn above SOFT_MB (8), fail above HARD_MB (16).
#
#   bash scripts/build.sh            -> plugin.wasm + plugin.wasm.sha256
#   bash scripts/build.sh --stamp    -> also writes that sha256 into filex-app.json
#
# --stamp is the flag filex-sign's build.sh takes, so a helper that builds
# both apps says the same thing to each. It writes through tools/setsha — the
# stamping tool scripts/release-prepare.sh already uses — so this repository
# still has ONE way to put a hash into the manifest.
#
# Why a build needs it at all: filex-app.json carries the module's sha256 and
# filex checks it at install. On 2026-09-21 the manifest said a9950e0d... while
# the plugin.wasm beside it hashed to 4822fda7... — the module had been rebuilt
# without restamping, so a GitHub install at a tag would have been refused with
# sha256_mismatch, and a copy of the pair (filex's screenshot and e2e scenes
# install exactly this directory) carried a manifest that did not describe its
# own module.
#
# Unlike filex-sign, the manifest need not be blanked for the build: the module
# embeds manifest.embed.json, which `go generate` derives from filex-app.json
# WITHOUT the wasm block (tools/manifest-embed), so the stamped value cannot
# change the bytes it describes — and TestManifestIsTheFileOnDisk, which runs
# before every build here, fails the day the embedded manifest carries it.
#
# The test suite runs FIRST and the build stops when it fails. A wasm
# module with a broken screen or a missing translation is worse than no
# module: filex installs it, and the person finds out. SKIP_TESTS=1 is for
# bisecting a build problem, never for a release.
set -euo pipefail
export GOTOOLCHAIN=local
cd "$(dirname "$0")/.."
STAMP=0
case "${1:-}" in
  "") ;;
  --stamp) STAMP=1 ;;
  *) echo "usage: bash scripts/build.sh [--stamp]" >&2; exit 2 ;;
esac
OUT="${OUT:-plugin.wasm}"
SOFT_MB="${SOFT_MB:-8}"
HARD_MB="${HARD_MB:-16}"
if [ "${SKIP_TESTS:-0}" = "1" ]; then
  echo "tests skipped (SKIP_TESTS=1) — do not release this module" >&2
else
  echo "running the test suite before building"
  if ! go test ./...; then
    echo "build refused: the tests do not pass, so plugin.wasm is not produced" >&2
    exit 1
  fi
fi
# -buildvcs=false: without it Go stamps the git commit into the module, so the
# hash changes with every commit — including the release commit that CARRIES
# the hash. The release workflow rebuilds and compares, so a stamped build
# could never match what was committed.
# ⚠⚠ The guest SDK this links must be the one the host ships. While
# go.mod `replace`s it with a generated local copy, that copy can go stale
# without a sign: the module then compiles against a host contract the server
# does not have, and nothing is red until somebody uses the app. The guard
# lives in the copy itself and names the command that refreshes it; once
# go.mod points at the published module it exits 0 without a word.
if [ -f ../filex-sdk-dev/check-sdk.mjs ] && command -v node >/dev/null 2>&1; then
  node ../filex-sdk-dev/check-sdk.mjs || exit 1
fi

GOOS=wasip1 GOARCH=wasm go build -trimpath -buildvcs=false -ldflags="-s -w" -buildmode=c-shared -o "$OUT" ./cmd/plugin
size=$(stat -c %s "$OUT")
sum=$(sha256sum "$OUT" | cut -d' ' -f1)
echo "$sum  $OUT" > "$OUT.sha256"
mb=$(( size / 1048576 ))
printf 'plugin.wasm  %d bytes (%d.%02d MiB)\nsha256       %s\n' "$size" "$mb" $(( (size % 1048576) * 100 / 1048576 )) "$sum"
if [ "$size" -gt $(( HARD_MB * 1048576 )) ]; then
  echo "size gate: $OUT exceeds the hard limit of ${HARD_MB} MiB" >&2
  exit 1
fi
if [ "$size" -gt $(( SOFT_MB * 1048576 )) ]; then
  echo "size gate: warning, $OUT exceeds the soft target of ${SOFT_MB} MiB" >&2
fi
if [ "$STAMP" = "1" ]; then
  go run ./tools/setsha "$OUT" >/dev/null
  stamped=$(grep -o '"sha256": *"[0-9a-f]\{64\}"' filex-app.json | grep -o '[0-9a-f]\{64\}')
  if [ "$stamped" != "$sum" ]; then
    echo "stamp failed: filex-app.json says $stamped, the module is $sum" >&2
    exit 1
  fi
  echo "stamped wasm.sha256 into filex-app.json"
fi
