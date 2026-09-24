// Package convert is the root of the filex-convert app plugin. It holds the
// one source of truth for the manifest: filex-app.json.
//
// The wasm module embeds manifest.embed.json, which `go generate` derives
// from filex-app.json by dropping the `wasm` block (the module cannot embed
// its own sha256 — see tools/manifest-embed). Everything `describe` must
// echo (name, version, manifest_version, permissions, actions, views) is
// byte-for-byte the file the administrator installs, and the tests fail
// when the two drift.
package convert

import (
	_ "embed"
	"encoding/json"

	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"
)

//go:generate go run ./tools/manifest-embed

//go:embed manifest.embed.json
var manifestJSON []byte

// ActionID is the single action the plugin registers.
const ActionID = "convert"

// ViewID is the options screen the action opens first.
const ViewID = "options"

// Manifest parses the embedded manifest. It panics on a malformed file:
// the build must not succeed with a manifest filex would refuse.
func Manifest() wire.Manifest {
	var m wire.Manifest
	if err := json.Unmarshal(manifestJSON, &m); err != nil {
		panic("filex-convert: manifest.embed.json is not valid: " + err.Error())
	}
	return m
}

// ManifestBytes returns the embedded manifest verbatim (for tests and tools).
func ManifestBytes() []byte { return manifestJSON }
