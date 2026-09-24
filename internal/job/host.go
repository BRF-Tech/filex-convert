package job

import (
	"github.com/brf-tech/filex/backend/pkg/pluginkit"
	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"
)

// Host is the slice of the plugin SDK a job needs. The real one wraps
// pluginkit (host functions that only answer inside filex); tests use a
// fake so the run loop, naming, retries and partial failures are covered
// without a wasm runtime.
type Host interface {
	// ReadInput reads a call-scoped ref (an input or an engine artefact).
	ReadInput(ref string) ([]byte, error)
	// InputSize answers the size the host reported for a ref (-1 unknown).
	InputSize(ref string) int64
	// WriteOutput creates an output file with the given bytes.
	WriteOutput(name string, data []byte) (wire.OutputRef, error)
	// EngineRun runs an engine and answers its artefacts.
	EngineRun(req pluginkit.EngineRequest) (*pluginkit.EngineResult, error)
	// Progress reports done/total with a message.
	Progress(done, total int64, message string)
	// Log writes a line to the plugin log.
	Log(level, msg string)
}

// SDKHost is the production Host over pluginkit.
type SDKHost struct{}

// ReadInput implements Host.
func (SDKHost) ReadInput(ref string) ([]byte, error) { return pluginkit.ReadInput(ref) }

// InputSize implements Host.
func (SDKHost) InputSize(ref string) int64 {
	in, err := pluginkit.OpenInput(ref)
	if err != nil {
		return -1
	}
	defer in.Close()
	return in.Size()
}

// WriteOutput implements Host.
func (SDKHost) WriteOutput(name string, data []byte) (wire.OutputRef, error) {
	return pluginkit.WriteOutput(name, data)
}

// EngineRun implements Host.
func (SDKHost) EngineRun(req pluginkit.EngineRequest) (*pluginkit.EngineResult, error) {
	return pluginkit.EngineRun(req)
}

// Progress implements Host.
func (SDKHost) Progress(done, total int64, message string) { pluginkit.Progress(done, total, message) }

// Log implements Host.
func (SDKHost) Log(level, msg string) { pluginkit.Log(level, msg) }
