package convert

import (
	"bytes"
	"encoding/json"
	"os"
	"regexp"
	"testing"

	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/i18n"
	"github.com/brf-tech/filex-convert/internal/job"
	"github.com/brf-tech/filex-convert/internal/view"
)

// The embedded manifest is filex-app.json minus its wasm block (one source
// of truth, derived by go generate), parses strictly, and agrees with the
// code: the action and view ids the plugin registers exist, and every
// engine the graph may use is granted.
func TestManifestIsTheFileOnDisk(t *testing.T) {
	disk, err := os.ReadFile("filex-app.json")
	if err != nil {
		t.Fatal(err)
	}
	var full, embedded map[string]any
	if err := json.Unmarshal(disk, &full); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(ManifestBytes(), &embedded); err != nil {
		t.Fatal(err)
	}
	if _, has := embedded["wasm"]; has {
		t.Error("the embedded manifest must not carry the wasm block")
	}
	delete(full, "wasm")
	a, _ := json.Marshal(full)
	b, _ := json.Marshal(embedded)
	if string(a) != string(b) {
		t.Fatal("manifest.embed.json is stale: run `go generate ./...`")
	}
	// strict decode: an unknown key would be refused by filex at install
	dec := json.NewDecoder(bytes.NewReader(disk))
	dec.DisallowUnknownFields()
	var strict struct {
		ManifestVersion   int            `json:"manifest_version"`
		Name              string         `json:"name"`
		Version           string         `json:"version"`
		Label             map[string]any `json:"label"`
		Description       map[string]any `json:"description"`
		Icon              string         `json:"icon"`
		Homepage          string         `json:"homepage"`
		MinFilex          string         `json:"min_filex"`
		Filex             string         `json:"filex"`
		Languages         []string       `json:"languages"`
		UILocales         map[string]any `json:"ui_locales"`
		Permissions       []string       `json:"permissions"`
		PermissionReasons map[string]any `json:"permission_reasons"`
		Settings          []any          `json:"settings"`
		Actions           []any          `json:"actions"`
		Views             []any          `json:"views"`
		PublicPages       []any          `json:"public_pages"`
		Limits            map[string]any `json:"limits"`
		Wasm              map[string]any `json:"wasm"`
	}
	if err := dec.Decode(&strict); err != nil {
		t.Fatalf("manifest has a key filex would refuse: %v", err)
	}
}

func TestManifestMatchesCode(t *testing.T) {
	m := Manifest()
	disk, err := os.ReadFile("filex-app.json")
	if err != nil {
		t.Fatal(err)
	}
	var onDisk = Manifest()
	if err := json.Unmarshal(disk, &onDisk); err != nil {
		t.Fatal(err)
	}
	m.Wasm = onDisk.Wasm
	if m.ManifestVersion != 1 || m.Name != "convert" {
		t.Errorf("name/version %q %d", m.Name, m.ManifestVersion)
	}
	if !regexp.MustCompile(`^\d+\.\d+\.\d+$`).MatchString(m.Version) {
		t.Errorf("version %q is not semver", m.Version)
	}
	if m.Label["en"] != "Convert" || m.Label["tr"] != "Dönüştür" || m.Label["es"] != "Convertir" ||
		m.Label["de"] != "Konvertieren" || m.Label["fr"] != "Convertir" {
		t.Errorf("label %v", m.Label)
	}
	perms := map[string]bool{}
	for _, p := range m.Permissions {
		perms[p] = true
		for _, lang := range i18n.Langs {
			if m.PermissionReasons[p][lang] == "" {
				t.Errorf("permission %s has no %s reason", p, lang)
			}
		}
	}
	if !perms["files:read"] || !perms["files:write"] {
		t.Error("files:read and files:write are required")
	}
	for _, e := range graph.Engines {
		if !perms["engines:"+e] {
			t.Errorf("graph uses engine %s but the manifest does not ask for engines:%s", e, e)
		}
	}
	if len(m.Permissions) != 2+len(graph.Engines) {
		t.Errorf("unexpected extra permissions: %v", m.Permissions)
	}
	for k := range m.PermissionReasons {
		if !perms[k] {
			t.Errorf("reason for a permission not asked for: %s", k)
		}
	}
	if len(m.Actions) != 1 || m.Actions[0].ID != ActionID || view.ActionID != ActionID {
		t.Errorf("actions %+v", m.Actions)
	}
	a := m.Actions[0]
	if a.View != ViewID || !a.Applies.Multi || a.Applies.Kind != "file" || len(a.Applies.Ext) != 0 {
		t.Errorf("action %+v", a)
	}
	if a.Output.Mode != "sibling" || a.Output.Name != "" {
		t.Errorf("output must be sibling with no name pattern (the plugin names its outputs): %+v", a.Output)
	}
	if a.Limits.TimeoutS != 900 {
		t.Errorf("timeout %d", a.Limits.TimeoutS)
	}
	if len(m.Views) != 1 || m.Views[0].ID != ViewID || m.Views[0].Placement != "modal" {
		t.Errorf("views %+v", m.Views)
	}
	if m.Wasm == nil || m.Wasm.URL != "https://github.com/BRF-Tech/filex-convert/releases/download/{tag}/plugin.wasm" {
		t.Errorf("wasm source %+v", m.Wasm)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(m.Wasm.SHA256) {
		t.Errorf("wasm.sha256 must be 64 hex chars (a placeholder until release): %q", m.Wasm.SHA256)
	}
	if job.ParamTarget != view.FieldTarget {
		t.Error("the view's target field and the job's target param must share a key")
	}
	if m.Limits.MemoryPages != 4096 {
		t.Errorf("memory pages %d (256 MiB expected for in-sandbox archives/images)", m.Limits.MemoryPages)
	}
}
