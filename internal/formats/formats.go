// Package formats is the static catalogue of file formats filex-convert
// knows: an id, the extensions that name it, a MIME type, a category and a
// label. Nothing here calls a host function or an engine; it is a table.
package formats

import (
	"sort"
	"strings"

	"github.com/brf-tech/filex-convert/internal/i18n"
)

// Category groups formats for the target picker.
type Category string

// The categories, in the order the picker shows them.
const (
	Image    Category = "image"
	Video    Category = "video"
	Audio    Category = "audio"
	Document Category = "document"
	Archive  Category = "archive"
	Data     Category = "data"
	Text     Category = "text"
	Subtitle Category = "subtitle"
	Font     Category = "font"
)

// Categories lists every category in display order.
var Categories = []Category{Image, Video, Audio, Document, Archive, Data, Text, Subtitle, Font}

// CategoryLabel is the category's name in every language the converter
// ships (internal/i18n, key `category.<c>`).
func CategoryLabel(c Category) map[string]string { return i18n.M("category." + string(c)) }

// Format is one catalogue entry.
type Format struct {
	ID       string   // stable id, also the primary extension in most cases
	Ext      []string // extensions naming this format, primary first, lower-case, no dot; "tar.gz" style compound extensions allowed
	Mime     string   // the MIME type an output of this format is served as
	Category Category
	// Label is the short human label ("PNG", "Word (.docx)"), in English.
	//
	// ⚠ A label that is a NAME ("PNG", "Word (.docx)") reads the same in
	// every language. A label that DESCRIBES ("Plain text (.txt)", "Icon
	// (.ico)") is English, and on another language's screen it is exactly
	// the half-translated button the v0.43.0 sweep found on a Turkish
	// wizard: those have a `format.<id>` key in internal/i18n, in every
	// language. TestEveryDescriptiveLabelIsTranslated keeps a new English
	// word from slipping back in.
	Label string
}

// Primary is the extension an output file gets.
func (f Format) Primary() string { return f.Ext[0] }

// Name is the label in the given language: the translated one when the label
// describes the format, else the label, which is a name and reads the same
// everywhere.
func (f Format) Name(locale string) string {
	if key := "format." + f.ID; i18n.Has(key) {
		return i18n.S(locale, key)
	}
	return f.Label
}

// Names is the label in every language.
func (f Format) Names() map[string]string {
	return i18n.Each(func(lang string) string { return f.Name(lang) })
}

// Neutral is the label that reads the same in EVERY language: the label
// when it is a name ("PDF", "Matroska (.mkv)"), the extension in capitals
// when the label describes ("TXT" for "Plain text (.txt)"). It is for the one
// place the converter can hand filex a single string only — a job's progress
// line (pluginkit.Progress) — where no language can be picked right for
// everybody who may read it.
func (f Format) Neutral() string {
	if i18n.Has("format." + f.ID) {
		return strings.ToUpper(f.Primary())
	}
	return f.Label
}

// All is the catalogue. Keep it sorted by category, then by id; the tests
// check every id and extension is unique.
var All = []Format{
	// ── image ──
	{ID: "png", Ext: []string{"png"}, Mime: "image/png", Category: Image, Label: "PNG"},
	{ID: "jpg", Ext: []string{"jpg", "jpeg"}, Mime: "image/jpeg", Category: Image, Label: "JPEG"},
	{ID: "gif", Ext: []string{"gif"}, Mime: "image/gif", Category: Image, Label: "GIF"},
	{ID: "bmp", Ext: []string{"bmp"}, Mime: "image/bmp", Category: Image, Label: "BMP"},
	{ID: "tiff", Ext: []string{"tiff", "tif"}, Mime: "image/tiff", Category: Image, Label: "TIFF"},
	{ID: "webp", Ext: []string{"webp"}, Mime: "image/webp", Category: Image, Label: "WebP"},
	{ID: "apng", Ext: []string{"apng"}, Mime: "image/apng", Category: Image, Label: "Animated PNG (.apng)"},
	{ID: "avif", Ext: []string{"avif"}, Mime: "image/avif", Category: Image, Label: "AVIF"},
	{ID: "heic", Ext: []string{"heic", "heif"}, Mime: "image/heic", Category: Image, Label: "HEIC"},
	{ID: "psd", Ext: []string{"psd"}, Mime: "image/vnd.adobe.photoshop", Category: Image, Label: "Photoshop (.psd)"},
	{ID: "xcf", Ext: []string{"xcf"}, Mime: "image/x-xcf", Category: Image, Label: "GIMP (.xcf)"},
	{ID: "ico", Ext: []string{"ico"}, Mime: "image/x-icon", Category: Image, Label: "Icon (.ico)"},
	{ID: "svg", Ext: []string{"svg"}, Mime: "image/svg+xml", Category: Image, Label: "SVG"},
	{ID: "qoi", Ext: []string{"qoi"}, Mime: "image/qoi", Category: Image, Label: "QOI"},
	{ID: "pnm", Ext: []string{"ppm", "pnm", "pgm", "pbm"}, Mime: "image/x-portable-pixmap", Category: Image, Label: "Netpbm (.ppm/.pgm/.pbm)"},
	{ID: "tga", Ext: []string{"tga"}, Mime: "image/x-tga", Category: Image, Label: "Targa (.tga)"},
	{ID: "dds", Ext: []string{"dds"}, Mime: "image/vnd-ms.dds", Category: Image, Label: "DirectDraw Surface (.dds)"},
	{ID: "exr", Ext: []string{"exr"}, Mime: "image/x-exr", Category: Image, Label: "OpenEXR (.exr)"},

	// ── video ──
	{ID: "mp4", Ext: []string{"mp4", "m4v"}, Mime: "video/mp4", Category: Video, Label: "MP4"},
	{ID: "mkv", Ext: []string{"mkv"}, Mime: "video/x-matroska", Category: Video, Label: "Matroska (.mkv)"},
	{ID: "webm", Ext: []string{"webm"}, Mime: "video/webm", Category: Video, Label: "WebM"},
	{ID: "mov", Ext: []string{"mov"}, Mime: "video/quicktime", Category: Video, Label: "QuickTime (.mov)"},
	{ID: "avi", Ext: []string{"avi"}, Mime: "video/x-msvideo", Category: Video, Label: "AVI"},
	{ID: "flv", Ext: []string{"flv"}, Mime: "video/x-flv", Category: Video, Label: "Flash Video (.flv)"},
	{ID: "wmv", Ext: []string{"wmv"}, Mime: "video/x-ms-wmv", Category: Video, Label: "Windows Media (.wmv)"},
	{ID: "mpg", Ext: []string{"mpg", "mpeg"}, Mime: "video/mpeg", Category: Video, Label: "MPEG-2 (.mpg)"},
	{ID: "ts", Ext: []string{"ts", "m2ts"}, Mime: "video/mp2t", Category: Video, Label: "MPEG-TS (.ts)"},
	{ID: "3gp", Ext: []string{"3gp"}, Mime: "video/3gpp", Category: Video, Label: "3GP"},
	{ID: "ogv", Ext: []string{"ogv"}, Mime: "video/ogg", Category: Video, Label: "Ogg Theora (.ogv)"},

	// ── audio ──
	{ID: "mp3", Ext: []string{"mp3"}, Mime: "audio/mpeg", Category: Audio, Label: "MP3"},
	{ID: "wav", Ext: []string{"wav"}, Mime: "audio/wav", Category: Audio, Label: "WAV"},
	{ID: "flac", Ext: []string{"flac"}, Mime: "audio/flac", Category: Audio, Label: "FLAC"},
	{ID: "ogg", Ext: []string{"ogg", "oga"}, Mime: "audio/ogg", Category: Audio, Label: "Ogg Vorbis"},
	{ID: "aac", Ext: []string{"aac"}, Mime: "audio/aac", Category: Audio, Label: "AAC"},
	{ID: "m4a", Ext: []string{"m4a"}, Mime: "audio/mp4", Category: Audio, Label: "M4A (AAC)"},
	{ID: "opus", Ext: []string{"opus"}, Mime: "audio/opus", Category: Audio, Label: "Opus"},
	{ID: "wma", Ext: []string{"wma"}, Mime: "audio/x-ms-wma", Category: Audio, Label: "Windows Media Audio (.wma)"},
	{ID: "aiff", Ext: []string{"aiff", "aif"}, Mime: "audio/aiff", Category: Audio, Label: "AIFF"},
	{ID: "ac3", Ext: []string{"ac3"}, Mime: "audio/ac3", Category: Audio, Label: "Dolby AC-3"},

	// ── document ──
	{ID: "pdf", Ext: []string{"pdf"}, Mime: "application/pdf", Category: Document, Label: "PDF"},
	{ID: "docx", Ext: []string{"docx"}, Mime: "application/vnd.openxmlformats-officedocument.wordprocessingml.document", Category: Document, Label: "Word (.docx)"},
	{ID: "doc", Ext: []string{"doc"}, Mime: "application/msword", Category: Document, Label: "Word 97 (.doc)"},
	{ID: "odt", Ext: []string{"odt"}, Mime: "application/vnd.oasis.opendocument.text", Category: Document, Label: "OpenDocument Text (.odt)"},
	{ID: "rtf", Ext: []string{"rtf"}, Mime: "application/rtf", Category: Document, Label: "Rich Text (.rtf)"},
	{ID: "xlsx", Ext: []string{"xlsx"}, Mime: "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet", Category: Document, Label: "Excel (.xlsx)"},
	{ID: "ods", Ext: []string{"ods"}, Mime: "application/vnd.oasis.opendocument.spreadsheet", Category: Document, Label: "OpenDocument Spreadsheet (.ods)"},
	{ID: "pptx", Ext: []string{"pptx"}, Mime: "application/vnd.openxmlformats-officedocument.presentationml.presentation", Category: Document, Label: "PowerPoint (.pptx)"},
	{ID: "odp", Ext: []string{"odp"}, Mime: "application/vnd.oasis.opendocument.presentation", Category: Document, Label: "OpenDocument Presentation (.odp)"},
	{ID: "epub", Ext: []string{"epub"}, Mime: "application/epub+zip", Category: Document, Label: "EPUB"},
	{ID: "ps", Ext: []string{"ps"}, Mime: "application/postscript", Category: Document, Label: "PostScript (.ps)"},
	{ID: "eps", Ext: []string{"eps"}, Mime: "application/postscript", Category: Document, Label: "EPS"},

	// ── archive ──
	{ID: "zip", Ext: []string{"zip"}, Mime: "application/zip", Category: Archive, Label: "ZIP"},
	{ID: "tar", Ext: []string{"tar"}, Mime: "application/x-tar", Category: Archive, Label: "TAR"},
	{ID: "tgz", Ext: []string{"tar.gz", "tgz"}, Mime: "application/gzip", Category: Archive, Label: "TAR + gzip (.tar.gz)"},
	{ID: "tzst", Ext: []string{"tar.zst", "tzst"}, Mime: "application/zstd", Category: Archive, Label: "TAR + zstd (.tar.zst)"},
	{ID: "txz", Ext: []string{"tar.xz", "txz"}, Mime: "application/x-xz", Category: Archive, Label: "TAR + xz (.tar.xz)"},
	{ID: "tbz2", Ext: []string{"tar.bz2", "tbz2", "tbz"}, Mime: "application/x-bzip2", Category: Archive, Label: "TAR + bzip2 (.tar.bz2)"},
	{ID: "rar", Ext: []string{"rar"}, Mime: "application/vnd.rar", Category: Archive, Label: "RAR"},

	// ── data ──
	{ID: "csv", Ext: []string{"csv"}, Mime: "text/csv", Category: Data, Label: "CSV"},
	{ID: "tsv", Ext: []string{"tsv"}, Mime: "text/tab-separated-values", Category: Data, Label: "TSV"},
	{ID: "json", Ext: []string{"json"}, Mime: "application/json", Category: Data, Label: "JSON"},
	{ID: "yaml", Ext: []string{"yaml", "yml"}, Mime: "application/yaml", Category: Data, Label: "YAML"},
	{ID: "xml", Ext: []string{"xml"}, Mime: "application/xml", Category: Data, Label: "XML"},
	{ID: "toml", Ext: []string{"toml"}, Mime: "application/toml", Category: Data, Label: "TOML"},

	// ── text ──
	{ID: "txt", Ext: []string{"txt", "text"}, Mime: "text/plain", Category: Text, Label: "Plain text (.txt)"},
	{ID: "md", Ext: []string{"md", "markdown"}, Mime: "text/markdown", Category: Text, Label: "Markdown"},
	{ID: "html", Ext: []string{"html", "htm", "xhtml"}, Mime: "text/html", Category: Text, Label: "HTML"},

	// ── subtitle ──
	{ID: "srt", Ext: []string{"srt"}, Mime: "application/x-subrip", Category: Subtitle, Label: "SubRip (.srt)"},
	{ID: "vtt", Ext: []string{"vtt"}, Mime: "text/vtt", Category: Subtitle, Label: "WebVTT (.vtt)"},
	{ID: "ass", Ext: []string{"ass", "ssa"}, Mime: "text/x-ssa", Category: Subtitle, Label: "SubStation Alpha (.ass)"},

	// ── font ──
	{ID: "ttf", Ext: []string{"ttf"}, Mime: "font/ttf", Category: Font, Label: "TrueType (.ttf)"},
	{ID: "otf", Ext: []string{"otf"}, Mime: "font/otf", Category: Font, Label: "OpenType (.otf)"},
	{ID: "woff", Ext: []string{"woff"}, Mime: "font/woff", Category: Font, Label: "WOFF"},
}

var (
	byID  = map[string]*Format{}
	byExt = map[string]*Format{}
	// compound extensions ("tar.gz") checked before the last-dot extension
	compound []string
)

func init() {
	for i := range All {
		f := &All[i]
		byID[f.ID] = f
		for _, e := range f.Ext {
			byExt[e] = f
			if strings.Contains(e, ".") {
				compound = append(compound, e)
			}
		}
	}
	// longest compound extension first so "tar.gz" wins over "gz" if ever added
	sort.Slice(compound, func(i, j int) bool { return len(compound[i]) > len(compound[j]) })
}

// ByID looks a format up by id.
func ByID(id string) (*Format, bool) {
	f, ok := byID[id]
	return f, ok
}

// ByExt looks a format up by extension (any case, with or without the dot).
func ByExt(ext string) (*Format, bool) {
	ext = strings.ToLower(strings.TrimPrefix(ext, "."))
	f, ok := byExt[ext]
	return f, ok
}

// Detect finds the format of a file name from its extension and returns the
// stem (the name without that extension). Compound extensions such as
// ".tar.gz" are recognised as a whole. Unknown extensions answer ok=false
// with the stem set to the name minus its last extension.
func Detect(name string) (f *Format, stem string, ok bool) {
	base := name
	if i := strings.LastIndexAny(base, `/\`); i >= 0 {
		base = base[i+1:]
	}
	lower := strings.ToLower(base)
	for _, ce := range compound {
		if strings.HasSuffix(lower, "."+ce) && len(lower) > len(ce)+1 {
			return byExt[ce], base[:len(base)-len(ce)-1], true
		}
	}
	dot := strings.LastIndex(base, ".")
	if dot <= 0 || dot == len(base)-1 {
		return nil, base, false
	}
	f, ok = byExt[lower[dot+1:]]
	return f, base[:dot], ok
}

// InCategory lists the formats of one category in catalogue order.
func InCategory(c Category) []*Format {
	var out []*Format
	for i := range All {
		if All[i].Category == c {
			out = append(out, &All[i])
		}
	}
	return out
}

// IDs lists every format id in catalogue order.
func IDs() []string {
	out := make([]string, 0, len(All))
	for i := range All {
		out = append(out, All[i].ID)
	}
	return out
}

// CategoryOf answers a format id's category ("" when unknown).
func CategoryOf(id string) Category {
	if f, ok := byID[id]; ok {
		return f.Category
	}
	return ""
}

// IDsIn lists the ids of one category in catalogue order.
func IDsIn(c Category) []string {
	var out []string
	for _, f := range InCategory(c) {
		out = append(out, f.ID)
	}
	return out
}
