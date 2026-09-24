// Command setsha writes a module's sha256 into filex-app.json's
// `wasm.sha256`, keeping the file's formatting:
//
//	go run ./tools/setsha plugin.wasm
//
// It is the release-preparation step (scripts/release-prepare.sh): the
// module's bytes do not depend on this value (see tools/manifest-embed),
// so the hash committed at the tag is the hash of what the tag builds.
package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"regexp"
)

var shaLine = regexp.MustCompile(`("sha256"\s*:\s*")[0-9a-f]{64}(")`)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: setsha <plugin.wasm>")
		os.Exit(2)
	}
	mod, err := os.ReadFile(os.Args[1])
	if err != nil {
		fail(err)
	}
	sum := sha256.Sum256(mod)
	hexsum := hex.EncodeToString(sum[:])
	src, err := os.ReadFile("filex-app.json")
	if err != nil {
		fail(err)
	}
	if !shaLine.Match(src) {
		fail(fmt.Errorf("filex-app.json has no wasm.sha256 line to replace"))
	}
	out := shaLine.ReplaceAll(src, []byte("${1}"+hexsum+"${2}"))
	if err := os.WriteFile("filex-app.json", out, 0o644); err != nil {
		fail(err)
	}
	fmt.Println(hexsum)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "setsha:", err)
	os.Exit(1)
}
