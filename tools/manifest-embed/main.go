// Command manifest-embed derives manifest.embed.json — the manifest the
// wasm module embeds and answers from `describe` — from filex-app.json by
// dropping the `wasm` block.
//
// Why a derived copy: the module's sha256 is written into filex-app.json at
// release time, and a manifest that embeds its own hash can never be built
// (the hash changes the bytes). `describe` does not need the block: filex
// compares name, version, manifest_version and permissions. Everything
// else is byte-for-byte the same file, and the test suite fails when the
// two drift.
//
//	go generate ./...    (or: go run ./tools/manifest-embed)
package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"regexp"
)

// wasmBlock matches the whole `"wasm": { … }` member (one level of braces
// inside, for the `{tag}` placeholder in the URL), including the comma that
// precedes it, so the rest of the file keeps its formatting.
var wasmBlock = regexp.MustCompile(`,\s*"wasm"\s*:\s*\{(?:[^{}]|\{[^{}]*\})*\}`)

func main() {
	src, err := os.ReadFile("filex-app.json")
	if err != nil {
		fail(err)
	}
	out := wasmBlock.ReplaceAll(src, nil)
	if bytes.Equal(out, src) {
		fail(fmt.Errorf("filex-app.json has no wasm block to strip"))
	}
	var check map[string]any
	if err := json.Unmarshal(out, &check); err != nil {
		fail(fmt.Errorf("stripped manifest is not valid JSON: %w", err))
	}
	if _, still := check["wasm"]; still {
		fail(fmt.Errorf("wasm block survived"))
	}
	if err := os.WriteFile("manifest.embed.json", out, 0o644); err != nil {
		fail(err)
	}
	fmt.Println("manifest.embed.json written")
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "manifest-embed:", err)
	os.Exit(1)
}
