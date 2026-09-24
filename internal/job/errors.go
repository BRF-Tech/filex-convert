package job

import (
	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"

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
	Code   Code
	Detail string // engine stderr tail, decoder message, … (English, technical)
	File   string
}

func (e *Error) Error() string {
	if e.Detail != "" {
		return string(e.Code) + ": " + e.Detail
	}
	return string(e.Code)
}

// Text renders the failure for people in every language the converter
// ships (internal/i18n, key `error.<code>`), with the file name and a short
// detail when there is one.
func (e *Error) Text() wire.Text {
	return i18n.Each(func(lang string) string {
		s := i18n.S(lang, "error."+string(e.Code))
		if e.Detail != "" && (e.Code == CodeEngineMissing || e.Code == CodeConvertFailed || e.Code == CodeEngineFailed) {
			s = i18n.S(lang, "error.with_detail", "message", s, "detail", clip(e.Detail, 120))
		}
		if e.File != "" {
			s = i18n.S(lang, "error.with_file", "file", e.File, "message", s)
		}
		return s
	})
}

func clip(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
