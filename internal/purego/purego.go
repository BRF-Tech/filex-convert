// Package purego holds the conversions that run inside the sandbox with no
// engine: raster images through Go's image codecs, archives through
// archive/zip, archive/tar, compress/gzip and zstd, tabular data between
// csv/tsv/json/yaml, and text between txt/md/html.
//
// Every converter is `func(in []byte, opts options.Options) ([]byte, error)`
// and is looked up by (from, to) format id. Inputs are whole byte slices:
// the graph only offers pure-Go edges for the formats where that is
// reasonable, and the job layer caps what it reads (MaxInput).
package purego

import (
	"errors"
	"fmt"

	"github.com/brf-tech/filex-convert/internal/options"
)

// MaxInput is the largest input a pure-Go converter accepts (64 MiB): the
// decoded form of an image or archive lives in wasm memory alongside it.
const MaxInput = 64 << 20

// Func converts one input into one output.
type Func func(in []byte, opts options.Options) ([]byte, error)

// ErrUnsupported is answered for a (from, to) pair with no pure-Go converter.
var ErrUnsupported = errors.New("purego: no converter for this pair")

// ErrTooLarge is answered when the input exceeds MaxInput.
var ErrTooLarge = errors.New("purego: input too large for an in-sandbox conversion")

var registry = map[string]Func{}

func key(from, to string) string { return from + ">" + to }

func register(from, to string, fn Func) {
	registry[key(from, to)] = fn
}

// Lookup answers the converter for a pair.
func Lookup(from, to string) (Func, bool) {
	fn, ok := registry[key(from, to)]
	return fn, ok
}

// Convert runs the converter for a pair.
func Convert(from, to string, in []byte, opts options.Options) ([]byte, error) {
	fn, ok := Lookup(from, to)
	if !ok {
		return nil, fmt.Errorf("%w: %s → %s", ErrUnsupported, from, to)
	}
	if len(in) > MaxInput {
		return nil, ErrTooLarge
	}
	return fn(in, opts)
}

// Pairs lists every registered (from, to) pair, for the tests that check the
// graph and the registry agree.
func Pairs() [][2]string {
	var out [][2]string
	for k := range registry {
		for i := 0; i < len(k); i++ {
			if k[i] == '>' {
				out = append(out, [2]string{k[:i], k[i+1:]})
				break
			}
		}
	}
	return out
}
