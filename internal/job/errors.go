package job

import (
	"strings"

	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"

	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/i18n"
)

// Code is a typed failure reason; every code has wording in every language
// (internal/i18n, `error.<code>`).
type Code string

// The failure codes a conversion can report per file.
const (
	CodeUnknownFormat Code = "unknown_format" // the source extension is not in the catalogue
	CodeNoTarget      Code = "no_target"      // params.target missing or unknown
	CodeNoRoute       Code = "no_route"       // no path between the formats
	CodeEngineMissing Code = "engine_missing" // a route exists but needs an engine this host lacks
	CodeEngineFailed  Code = "engine_failed"  // the engine exited non-zero or produced nothing
	CodeTooLarge      Code = "too_large"      // the input is beyond what a pure-Go step can hold
	CodeReadFailed    Code = "read_failed"    // the host refused or failed reading the input
	CodeWriteFailed   Code = "write_failed"   // the host refused or failed writing the output
	CodeConvertFailed Code = "convert_failed" // a pure-Go step failed (corrupt input, unsupported variant)
	CodeBadOptions    Code = "bad_options"    // a knob is out of range
	CodeCancelled     Code = "cancelled"      // the job ran out of time budget
)

// Codes lists every failure code (each has an `error.<code>` message).
var Codes = []Code{CodeUnknownFormat, CodeNoTarget, CodeNoRoute, CodeEngineMissing, CodeEngineFailed,
	CodeTooLarge, CodeReadFailed, CodeWriteFailed, CodeConvertFailed, CodeBadOptions, CodeCancelled}

// Error is one file's failure with a code and a detail line.
type Error struct {
	Code Code
	// Detail is the technical line for the plugin's LOG (engine stderr tail,
	// decoder message …). It is English and it is not for a person: see Text.
	Detail string
	File   string
	// Engines are the engines this failure is about — the one that failed,
	// or the ones the route would need — by id. A person reads them by their
	// product name ("ImageMagick"), which is the same in every language.
	Engines []string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return string(e.Code) + ": " + e.Detail
	}
	return string(e.Code)
}

// Text renders the failure for people in every language the converter
// ships (internal/i18n, key `error.<code>`), with the file name and, when
// the failure is an engine's, that engine's name.
//
// ⚠⚠ Never the Detail. It used to follow the reason in brackets, and it is
// whatever the engine or a Go decoder said, in English: a Turkish tray read
// "foto.png: dönüşüm başarısız oldu (png: invalid format: not a PNG file)"
// and "dönüştürücü başarısız oldu (ffmpeg exit 1: Conversion failed!)" —
// a sentence the reader cannot act on, half in a language they did not pick
// (filex's own rule, lesson #292: no raw server text on a person's screen).
// The detail goes to the plugin's log (Run and runMerged write Error()),
// where the administrator who can act on it reads it.
func (e *Error) Text() wire.Text {
	return i18n.Each(func(lang string) string {
		s := i18n.S(lang, "error."+string(e.Code))
		if len(e.Engines) > 0 && (e.Code == CodeEngineMissing || e.Code == CodeEngineFailed) {
			s = i18n.S(lang, "error.with_detail", "message", s, "detail", strings.Join(graph.EngineNames(e.Engines), ", "))
		}
		if e.File != "" {
			s = i18n.S(lang, "error.with_file", "file", e.File, "message", s)
		}
		return s
	})
}
