// Package options defines the knobs a conversion can take (quality, crf,
// dpi, …): their keys, types, ranges and labels, and typed accessors over
// the loose `params` map a job or a screen event carries.
package options

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/brf-tech/filex-convert/internal/i18n"
)

// Keys of every knob. An edge in the graph names the keys it honours.
const (
	Quality   = "quality"    // 1..100, lossy image encoders
	CRF       = "crf"        // 0..51, x264/vp9 constant rate factor
	MaxHeight = "max_height" // 0 = keep; scale video/gif down to this height
	Preset    = "preset"     // engine-specific preset (video speed, pdf compression)
	Bitrate   = "bitrate"    // audio kbit/s
	DPI       = "dpi"        // raster density for pdf/svg rendering
	Pages     = "pages"      // page range "1-3,5" for pdf rendering
	PDFA      = "pdfa"       // bool: produce PDF/A
	Picture   = "picture"    // audio → image: waveform | spectrogram
	At        = "at"         // seconds: the moment a single video frame is taken
	Every     = "every"      // seconds between extracted frames (0 = one frame)
	Duration  = "duration"   // seconds a still image plays as a video
	IconSizes = "icon_sizes" // favicon size set for .ico
	Merge     = "merge"      // bool: combine the whole selection into one output
)

// Def describes one knob for the screen builder.
type Def struct {
	Key     string
	Type    string // int | select | string | bool
	Label   map[string]string
	Help    map[string]string
	Min     int
	Max     int
	Default any
	// Choices for select knobs, keyed by variant ("video" / "pdf" for preset).
	Choices map[string][]Choice
}

// Choice is one option of a select knob.
type Choice struct {
	Value string
	Label map[string]string
}

// Defs is the catalogue of knobs.
var Defs = map[string]Def{
	Quality: {Key: Quality, Type: "int", Min: 1, Max: 100, Default: 85,
		Label: i18n.M("option.quality.label"),
		Help:  i18n.M("option.quality.help")},
	CRF: {Key: CRF, Type: "int", Min: 0, Max: 51, Default: 23,
		Label: i18n.M("option.crf.label"),
		Help:  i18n.M("option.crf.help")},
	MaxHeight: {Key: MaxHeight, Type: "int", Min: 0, Max: 4320, Default: 0,
		Label: i18n.M("option.max_height.label"),
		Help:  i18n.M("option.max_height.help")},
	Preset: {Key: Preset, Type: "select", Default: "medium",
		Label: i18n.M("option.preset.label"),
		Help:  i18n.M("option.preset.help"),
		Choices: map[string][]Choice{
			"video": {
				{Value: "copy", Label: i18n.M("option.preset.video.copy")},
				{Value: "ultrafast", Label: i18n.M("option.preset.video.ultrafast")},
				{Value: "fast", Label: i18n.M("option.preset.video.fast")},
				{Value: "medium", Label: i18n.M("option.preset.video.medium")},
				{Value: "slow", Label: i18n.M("option.preset.video.slow")},
			},
			"pdf": {
				{Value: "screen", Label: i18n.M("option.preset.pdf.screen")},
				{Value: "ebook", Label: i18n.M("option.preset.pdf.ebook")},
				{Value: "printer", Label: i18n.M("option.preset.pdf.printer")},
				{Value: "prepress", Label: i18n.M("option.preset.pdf.prepress")},
			},
		}},
	Bitrate: {Key: Bitrate, Type: "select", Default: "192",
		Label: i18n.M("option.bitrate.label"),
		Help:  i18n.M("option.bitrate.help"),
		Choices: map[string][]Choice{
			"": {
				{Value: "96", Label: i18n.M("option.bitrate.rate", "rate", "96")},
				{Value: "128", Label: i18n.M("option.bitrate.rate", "rate", "128")},
				{Value: "192", Label: i18n.M("option.bitrate.default", "rate", "192")},
				{Value: "256", Label: i18n.M("option.bitrate.rate", "rate", "256")},
				{Value: "320", Label: i18n.M("option.bitrate.rate", "rate", "320")},
			},
		}},
	DPI: {Key: DPI, Type: "int", Min: 36, Max: 600, Default: 150,
		Label: i18n.M("option.dpi.label"),
		Help:  i18n.M("option.dpi.help")},
	Pages: {Key: Pages, Type: "string", Default: "",
		Label: i18n.M("option.pages.label"),
		Help:  i18n.M("option.pages.help")},
	PDFA: {Key: PDFA, Type: "bool", Default: false,
		Label: i18n.M("option.pdfa.label"),
		Help:  i18n.M("option.pdfa.help"),
		// A bool's two answers are WRITTEN OUT, and the field is drawn as
		// two buttons (`Style: "choice"`, set where the field is built).
		// "PDF/A (archival)" beside an empty tickbox asks the person to
		// guess what ticking it means.
		Choices: map[string][]Choice{
			"": {
				{Value: "true", Label: i18n.M("option.pdfa.yes")},
				{Value: "false", Label: i18n.M("option.pdfa.no")},
			},
		}},
	Picture: {Key: Picture, Type: "select", Default: "waveform",
		Label: i18n.M("option.picture.label"),
		Help:  i18n.M("option.picture.help"),
		Choices: map[string][]Choice{
			"": {
				{Value: "waveform", Label: i18n.M("option.picture.waveform")},
				{Value: "spectrogram", Label: i18n.M("option.picture.spectrogram")},
			},
		}},
	At: {Key: At, Type: "int", Min: 0, Max: 359999, Default: 0,
		Label: i18n.M("option.at.label"),
		Help:  i18n.M("option.at.help")},
	Every: {Key: Every, Type: "int", Min: 0, Max: 3600, Default: 0,
		Label: i18n.M("option.every.label"),
		Help:  i18n.M("option.every.help")},
	Duration: {Key: Duration, Type: "int", Min: 1, Max: 600, Default: 5,
		Label: i18n.M("option.duration.label"),
		Help:  i18n.M("option.duration.help")},
	IconSizes: {Key: IconSizes, Type: "select", Default: "favicon",
		Label: i18n.M("option.icon_sizes.label"),
		Help:  i18n.M("option.icon_sizes.help"),
		Choices: map[string][]Choice{
			"": {
				{Value: "favicon", Label: i18n.M("option.icon_sizes.favicon")},
				{Value: "app", Label: i18n.M("option.icon_sizes.app")},
				{Value: "single", Label: i18n.M("option.icon_sizes.single")},
			},
		}},
	Merge: {Key: Merge, Type: "bool", Default: false,
		Label: i18n.M("option.merge.label"),
		Help:  i18n.M("option.merge.help"),
		// "One file or one for each" is a decision, not a setting flipped
		// in passing: both answers are written out and drawn as buttons.
		Choices: map[string][]Choice{
			"": {
				{Value: "true", Label: i18n.M("option.merge.yes")},
				{Value: "false", Label: i18n.M("option.merge.no")},
			},
		}},
}

// Order is the display order of knobs on the options screen.
var Order = []string{Merge, Preset, Quality, CRF, MaxHeight, Bitrate, DPI, Pages, PDFA, Picture, At, Every, Duration, IconSizes}

// Options is the loose params map with typed accessors. Missing or malformed
// values answer the knob's default; Validate reports what is out of range.
type Options map[string]any

// Int reads an integer knob (accepts numbers and numeric strings).
func (o Options) Int(key string) int {
	def := 0
	if d, ok := Defs[key]; ok {
		if v, ok := d.Default.(int); ok {
			def = v
		}
	}
	v, ok := o[key]
	if !ok || v == nil {
		return def
	}
	switch x := v.(type) {
	case int:
		return x
	case int64:
		return int(x)
	case float64:
		if math.IsNaN(x) || math.IsInf(x, 0) {
			return def
		}
		return int(x)
	case string:
		if n, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
			return n
		}
	}
	return def
}

// String reads a string knob.
func (o Options) String(key string) string {
	def := ""
	if d, ok := Defs[key]; ok {
		if v, ok := d.Default.(string); ok {
			def = v
		}
	}
	v, ok := o[key]
	if !ok || v == nil {
		return def
	}
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) == "" {
			return def
		}
		return strings.TrimSpace(x)
	case float64:
		return strconv.Itoa(int(x))
	case int:
		return strconv.Itoa(x)
	}
	return def
}

// Bool reads a boolean knob (accepts bools and "true"/"1"/"on").
func (o Options) Bool(key string) bool {
	v, ok := o[key]
	if !ok || v == nil {
		if d, ok := Defs[key]; ok {
			if b, ok := d.Default.(bool); ok {
				return b
			}
		}
		return false
	}
	switch x := v.(type) {
	case bool:
		return x
	case string:
		s := strings.ToLower(strings.TrimSpace(x))
		return s == "true" || s == "1" || s == "on" || s == "yes"
	case float64:
		return x != 0
	case int:
		return x != 0
	}
	return false
}

// PageRange is a parsed `pages` knob: 1-based, inclusive; zero = open end.
type PageRange struct{ First, Last int }

// ParsePages accepts "", "3", "2-5", "2-" and answers the range (First=0 =
// all pages). Anything else is an error.
func ParsePages(s string) (PageRange, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return PageRange{}, nil
	}
	parts := strings.SplitN(s, "-", 2)
	first, err := strconv.Atoi(strings.TrimSpace(parts[0]))
	if err != nil || first < 1 {
		return PageRange{}, fmt.Errorf("pages: %q is not a page range", s)
	}
	if len(parts) == 1 {
		return PageRange{First: first, Last: first}, nil
	}
	if strings.TrimSpace(parts[1]) == "" {
		return PageRange{First: first}, nil
	}
	last, err := strconv.Atoi(strings.TrimSpace(parts[1]))
	if err != nil || last < first {
		return PageRange{}, fmt.Errorf("pages: %q is not a page range", s)
	}
	return PageRange{First: first, Last: last}, nil
}

// Validate checks the given keys against their definitions. variant selects
// the preset choice list ("video" or "pdf"). It returns per-key messages
// keyed by knob, en/tr.
func (o Options) Validate(keys []string, variant string) map[string]map[string]string {
	errs := map[string]map[string]string{}
	for _, k := range keys {
		d, ok := Defs[k]
		if !ok {
			continue
		}
		switch d.Type {
		case "int":
			raw, present := o[k]
			if !present || raw == nil || raw == "" {
				continue
			}
			v := o.Int(k)
			if s, isStr := raw.(string); isStr {
				if _, err := strconv.Atoi(strings.TrimSpace(s)); err != nil {
					errs[k] = i18n.M("validate.whole_number")
					continue
				}
			}
			if v < d.Min || v > d.Max {
				errs[k] = i18n.M("validate.between", "min", d.Min, "max", d.Max)
			}
		case "select":
			raw, present := o[k]
			if !present || raw == nil || raw == "" {
				continue
			}
			v := o.String(k)
			choices := d.Choices[variant]
			if choices == nil {
				choices = d.Choices[""]
			}
			found := false
			for _, c := range choices {
				if c.Value == v {
					found = true
					break
				}
			}
			if !found {
				errs[k] = i18n.M("validate.pick")
			}
		case "string":
			if k == Pages {
				if _, err := ParsePages(o.String(k)); err != nil {
					errs[k] = i18n.M("validate.pages")
				}
			}
		}
	}
	return errs
}

// Choices answers the choice list for a select knob in a variant, falling
// back to the unkeyed list.
func Choices(key, variant string) []Choice {
	d, ok := Defs[key]
	if !ok {
		return nil
	}
	if c := d.Choices[variant]; c != nil {
		return c
	}
	return d.Choices[""]
}

// DefaultFor answers a knob's default, with the preset default depending on
// the variant ("pdf" → ebook, "video" → medium).
func DefaultFor(key, variant string) any {
	if key == Preset && variant == "pdf" {
		return "ebook"
	}
	if d, ok := Defs[key]; ok {
		return d.Default
	}
	return nil
}
