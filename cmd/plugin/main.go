// Command plugin is the filex-convert app plugin: a WebAssembly module
// filex loads and calls through the pluginkit ABI.
//
//	GOOS=wasip1 GOARCH=wasm go build -trimpath -ldflags="-s -w" -buildmode=c-shared -o plugin.wasm ./cmd/plugin
//
// Registration happens in init(): a wasip1 module built with
// -buildmode=c-shared is a reactor, filex calls the exports directly and
// main() never runs.
//
// The assembly itself lives in internal/app, over a host it is handed, so
// the test suite drives the SAME plugin against plugintest's fake filex —
// see internal/app/app_test.go.
package main

import (
	"github.com/brf-tech/filex/backend/pkg/pluginkit"

	"github.com/brf-tech/filex-convert/internal/app"
	"github.com/brf-tech/filex-convert/internal/job"
)

func main() {}

func init() {
	pluginkit.Run(app.Plugin(job.SDKHost{}))
}
