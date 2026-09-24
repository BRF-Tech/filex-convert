#!/usr/bin/env bash
# Rough per-dependency wasm cost: builds a minimal reactor importing one
# library at a time and prints the size delta against an empty module.
# Development aid only; run from the repository root inside a Go toolchain.
# PROBES="name:import ..." overrides the list.
set -euo pipefail
export GOTOOLCHAIN=local
cd "$(dirname "$0")/.."
tmp=$(mktemp -d)
trap 'rm -rf "$tmp"' EXIT
probe() {
  name="$1"; shift
  mkdir -p "cmd/probe-$name"
  {
    echo 'package main'
    echo 'import ('
    for imp in "$@"; do echo "  _ \"$imp\""; done
    echo ')'
    echo 'func main() {}'
  } > "cmd/probe-$name/main.go"
  GOOS=wasip1 GOARCH=wasm go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o "$tmp/$name.wasm" "./cmd/probe-$name"
  rm -rf "cmd/probe-$name"
  stat -c %s "$tmp/$name.wasm"
}
base=$(probe base)
printf '%-14s %10d\n' base "$base"
default_probes="pdk:github.com/extism/go-pdk
json:encoding/json
xml:encoding/xml
regexp:regexp
yaml:gopkg.in/yaml.v3
goldmark:github.com/yuin/goldmark github.com/yuin/goldmark/extension
zstd:github.com/klauspost/compress/zstd
zip:archive/zip archive/tar compress/gzip compress/bzip2
xz:github.com/ulikunitz/xz
images:image/png image/jpeg image/gif golang.org/x/image/bmp golang.org/x/image/tiff golang.org/x/image/webp golang.org/x/image/draw
webpenc:github.com/HugoSmits86/nativewebp
sfnt:golang.org/x/image/font/sfnt golang.org/x/image/font/gofont/goregular golang.org/x/image/font/gofont/gobold golang.org/x/image/font/gofont/gomono
rar:github.com/nwaples/rardecode/v2
sevenzip:github.com/bodgit/sevenzip
toml:github.com/BurntSushi/toml
csv:encoding/csv"
printf '%s\n' "${PROBES:-$default_probes}" | while IFS= read -r spec; do
  [ -n "$spec" ] || continue
  name="${spec%%:*}"; imports="${spec#*:}"
  sz=$(probe "$name" $imports)
  printf '%-14s %10d  (+%d)\n' "$name" "$sz" $(( sz - base ))
done
