// Command render is a development aid: it runs one pure-Go conversion on
// the host (no filex, no wasm) so the output can be opened and eyeballed.
//
//	go run ./tools/render <from-id> <to-id> <input> <output>
package main

import (
	"fmt"
	"os"

	"github.com/brf-tech/filex-convert/internal/options"
	"github.com/brf-tech/filex-convert/internal/purego"
)

func main() {
	if len(os.Args) != 5 {
		fmt.Fprintln(os.Stderr, "usage: render <from> <to> <input> <output>")
		os.Exit(2)
	}
	in, err := os.ReadFile(os.Args[3])
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	out, err := purego.Convert(os.Args[1], os.Args[2], in, options.Options{"source_name": os.Args[3]})
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	if err := os.WriteFile(os.Args[4], out, 0o644); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	fmt.Printf("%s → %s: %d bytes\n", os.Args[1], os.Args[2], len(out))
}
