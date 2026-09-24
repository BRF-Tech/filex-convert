// Package app assembles the plugin: the manifest, the action and the
// screens, over one host.
//
// The host is a parameter, not a package-level call, and that is the whole
// point. pluginkit's host functions only answer inside wasm — off-wasm they
// return ErrNotWasm — so a plugin that calls them directly can only be
// tested by building the module and installing it. Taking `job.Host` here
// means `cmd/plugin` passes the real SDK host and the test suite passes
// plugintest's fake one, and the SAME assembly is measured either way:
// the manifest, the registered ids and the screens a person will see.
package app

import (
	"github.com/brf-tech/filex/backend/pkg/pluginkit"
	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"

	convert "github.com/brf-tech/filex-convert"
	"github.com/brf-tech/filex-convert/internal/job"
	"github.com/brf-tech/filex-convert/internal/view"
)

// Plugin is what pluginkit.Run registers, over the given host.
func Plugin(h job.Host) *pluginkit.Plugin {
	return &pluginkit.Plugin{
		Manifest: convert.Manifest(),
		Actions: map[string]pluginkit.ActionFunc{
			convert.ActionID: func(in *wire.ActionRunInput) (*wire.ActionRunOutput, error) {
				return job.Run(h, in)
			},
		},
		Views: map[string]pluginkit.ViewFunc{
			convert.ViewID: view.Handle,
		},
	}
}
