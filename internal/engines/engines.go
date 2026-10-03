// Package engines builds the argument vectors for the host's heavy engines
// (ffmpeg, ImageMagick, the office engine, Ghostscript, poppler, rsvg). It never
// runs anything: the job layer hands an Invocation to pluginkit.EngineRun.
//
// Two rules from the host shape everything here:
//
//   - Arguments are bare tokens. No path separator, no "..", no "@list", no
//     "file:"/"http:"-style scheme, and none of the engine flags that would
//     open the sandbox (ffmpeg -safe / -protocol_whitelist, Ghostscript
//     -dNOSAFER, LibreOffice --infilter, …). CheckArg mirrors the host's
//     rule so a bad builder fails in the unit tests, not on the server.
//   - Inputs and outputs are just file names inside the engine's private
//     run directory: the host copies the input under the name we choose,
//     and every new file in that directory comes back as an output ref.
//
// A "/" is never available, so ffmpeg filter expressions use decimals and
// products (fps=0.2, scale=trunc(iw*0.5)*2) and Ghostscript presets are
// spelled out parameter by parameter.
package engines

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/options"
)

// Invocation is one engine run, ready for the host.
type Invocation struct {
	Engine  string
	Args    []string
	Input   string   // the input's name in the run directory
	Outputs []string // expected output names; empty = collect every new file
	// Multi says several files may come back (pages); the job keeps them all.
	Multi bool
	// TimeoutS is the wall clock the step asks for (0 = host default).
	TimeoutS int
}

// ErrUnsupported is answered for an edge no builder knows.
var ErrUnsupported = errors.New("engines: no argv builder for this edge")

// ErrBadArg is answered when a builder produced a token the host refuses.
var ErrBadArg = errors.New("engines: argument would be refused by the host")

// MaxOutputs is the most files one engine run may return (the host's cap).
const MaxOutputs = 64

// Build makes the invocation for one graph edge. inName is the input's
// name in the run directory (use InputName), outStem the bare stem the
// output should carry (letters, digits, "-" and "_" only; see OutputName).
func Build(e graph.Edge, inName, outStem string, opts options.Options) (Invocation, error) {
	var inv Invocation
	var err error
	switch e.Engine {
	case graph.FFmpeg:
		inv, err = ffmpeg(e, inName, outStem, opts)
	case graph.ImageMagick:
		inv, err = magick(e, inName, outStem, opts)
	case graph.Office:
		inv, err = soffice(e, inName, outStem, opts)
	case graph.Ghostscript:
		inv, err = ghostscript(e, inName, outStem, opts)
	case graph.Poppler:
		inv, err = poppler(e, inName, outStem, opts)
	case graph.RSVG:
		inv, err = rsvg(e, inName, outStem, opts)
	default:
		return Invocation{}, fmt.Errorf("%w: %s", ErrUnsupported, e.Key())
	}
	if err != nil {
		return Invocation{}, err
	}
	inv.Engine = e.Engine
	inv.Input = inName
	inv.Multi = e.Multi
	for _, a := range inv.Args {
		if cerr := CheckArg(a); cerr != nil {
			return Invocation{}, fmt.Errorf("%w: %v", ErrBadArg, cerr)
		}
	}
	return inv, nil
}

// InputName is the name an input gets inside the run directory: a fixed
// stem plus the format's primary extension, so every engine sniffs the
// type it expects and no user-supplied name reaches an argv.
func InputName(format string) string {
	if f, ok := formats.ByID(format); ok {
		return "in." + f.Primary()
	}
	return "in.bin"
}

// OutputName is the produced file's name in the run directory.
func OutputName(stem, format string) string {
	if f, ok := formats.ByID(format); ok {
		return stem + "." + f.Primary()
	}
	return stem + ".bin"
}

// CheckArg mirrors the host's refusal rule for engine arguments.
func CheckArg(a string) error {
	if len(a) > 4096 {
		return errors.New("argument too long")
	}
	if a == "" {
		return errors.New("empty argument")
	}
	if strings.ContainsAny(a, "/\\\x00") || strings.Contains(a, "..") {
		return fmt.Errorf("path separator or '..' in %q", a)
	}
	if strings.HasPrefix(a, "@") {
		return fmt.Errorf("file list in %q", a)
	}
	lower := strings.ToLower(a)
	for _, bad := range []string{"file:", "http:", "https:", "ftp:", "pipe:", "tcp:", "udp:", "rtsp:", "rtmp:", "concat:", "subfile:", "data:", "-safe", "-protocol_whitelist", "-dnosafer", "-dnosafer=", "--infilter", "-shell", "%pipe%", "-sdevice=pipe"} {
		if strings.HasPrefix(lower, bad) || strings.Contains(lower, "="+bad) {
			return fmt.Errorf("refused token %q", a)
		}
	}
	return nil
}

func itoa(n int) string { return strconv.Itoa(n) }

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func ext(format string) string {
	if f, ok := formats.ByID(format); ok {
		return f.Primary()
	}
	return "bin"
}

func category(id string) formats.Category { return formats.CategoryOf(id) }

// ── ffmpeg ──

// videoCodecs names the video and audio codec per container. x264 and VP9
// take -crf; the older codecs (WMV2, MPEG-2, Theora) take a quality scale
// derived from the CRF knob instead.
var videoCodecs = map[string][]string{
	"mp4":  {"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart"},
	"mov":  {"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac", "-movflags", "+faststart"},
	"mkv":  {"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac"},
	"avi":  {"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "libmp3lame"},
	"webm": {"-c:v", "libvpx-vp9", "-b:v", "0", "-row-mt", "1", "-c:a", "libopus"},
	"flv":  {"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac"},
	"ts":   {"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac"},
	"3gp":  {"-c:v", "libx264", "-pix_fmt", "yuv420p", "-c:a", "aac"},
	"wmv":  {"-c:v", "wmv2", "-pix_fmt", "yuv420p", "-c:a", "wmav2"},
	"mpg":  {"-c:v", "mpeg2video", "-pix_fmt", "yuv420p", "-c:a", "mp2"},
	"ogv":  {"-c:v", "libtheora", "-pix_fmt", "yuv420p", "-c:a", "libvorbis"},
}

// x264Like says the container's codec takes -crf and -preset.
var x264Like = map[string]bool{"mp4": true, "mov": true, "mkv": true, "avi": true, "flv": true, "ts": true, "3gp": true}

var audioCodecs = map[string]func(bitrate string) []string{
	"mp3":  func(b string) []string { return []string{"-c:a", "libmp3lame", "-b:a", b + "k"} },
	"aac":  func(b string) []string { return []string{"-c:a", "aac", "-b:a", b + "k"} },
	"m4a":  func(b string) []string { return []string{"-c:a", "aac", "-b:a", b + "k"} },
	"ogg":  func(b string) []string { return []string{"-c:a", "libvorbis", "-b:a", b + "k"} },
	"opus": func(b string) []string { return []string{"-c:a", "libopus", "-b:a", b + "k"} },
	"wma":  func(b string) []string { return []string{"-c:a", "wmav2", "-b:a", b + "k"} },
	"ac3":  func(b string) []string { return []string{"-c:a", "ac3", "-b:a", b + "k"} },
	"flac": func(string) []string { return []string{"-c:a", "flac"} },
	"wav":  func(string) []string { return []string{"-c:a", "pcm_s16le"} },
	"aiff": func(string) []string { return []string{"-c:a", "pcm_s16be"} },
}

var animated = map[string]bool{"gif": true, "webp": true, "apng": true}

// videoQuality answers the encoder quality flags for a container from the
// CRF knob.
func videoQuality(to string, crf int, preset string) []string {
	switch {
	case x264Like[to]:
		return []string{"-crf", itoa(crf), "-preset", preset}
	case to == "webm":
		return []string{"-crf", itoa(crf)}
	case to == "ogv":
		// theora: 0 (worst) … 10 (best)
		return []string{"-q:v", itoa(clamp(10-crf/5, 0, 10))}
	default:
		// mpeg-2 / wmv2 quantiser: 1 (best) … 31 (worst)
		return []string{"-q:v", itoa(clamp(crf*31/51, 1, 31))}
	}
}

// evenScale keeps dimensions even (yuv420p needs it) without a "/".
const evenScale = "scale=trunc(iw*0.5)*2:trunc(ih*0.5)*2"

func heightScale(h int) string {
	return "scale=-2:'min(ih," + itoa(h) + ")'"
}

func ffmpeg(e graph.Edge, in, stem string, o options.Options) (Invocation, error) {
	out := OutputName(stem, e.To)
	args := []string{"-hide_banner", "-nostdin", "-y"}
	fromCat, toCat := category(e.From), category(e.To)
	crf := clamp(o.Int(options.CRF), 0, 51)
	preset := o.String(options.Preset)
	switch {
	// still image → video
	case fromCat == formats.Image && !animated[e.From] && toCat == formats.Video:
		dur := clamp(o.Int(options.Duration), 1, 600)
		args = append(args, "-loop", "1", "-framerate", "25", "-i", in, "-t", itoa(dur))
		args = append(args, videoCodecs[e.To]...)
		args = append(args, videoQuality(e.To, crf, preset)...)
		args = append(args, "-an", "-vf", evenScale)

	// animated image → video
	case animated[e.From] && toCat == formats.Video:
		args = append(args, "-i", in)
		args = append(args, videoCodecs[e.To]...)
		args = append(args, videoQuality(e.To, crf, preset)...)
		args = append(args, "-an", "-vf", evenScale)

	// video → video
	case fromCat == formats.Video && toCat == formats.Video:
		args = append(args, "-i", in)
		if preset == "copy" {
			args = append(args, "-c", "copy")
			if e.To == "mp4" || e.To == "mov" {
				args = append(args, "-movflags", "+faststart")
			}
			break
		}
		args = append(args, videoCodecs[e.To]...)
		args = append(args, videoQuality(e.To, crf, preset)...)
		if h := o.Int(options.MaxHeight); h > 0 {
			args = append(args, "-vf", heightScale(h))
		}

	// video or animated image → animated image
	case animated[e.To] && (fromCat == formats.Video || animated[e.From]):
		h := o.Int(options.MaxHeight)
		if h <= 0 {
			h = 480
		}
		base := "fps=12," + heightScale(h) + ":flags=lanczos"
		switch e.To {
		case "gif":
			args = append(args, "-i", in, "-vf", base+",split[a][b];[a]palettegen[p];[b][p]paletteuse", "-loop", "0", "-an")
		case "webp":
			args = append(args, "-i", in, "-vf", base, "-c:v", "libwebp", "-lossless", "0", "-q:v", "75", "-loop", "0", "-an")
		case "apng":
			args = append(args, "-i", in, "-vf", base, "-f", "apng", "-plays", "0", "-an")
		}

	// video → frames
	case fromCat == formats.Video && (e.To == "png" || e.To == "jpg"):
		at := clamp(o.Int(options.At), 0, 359999)
		every := clamp(o.Int(options.Every), 0, 3600)
		var vf []string
		if h := o.Int(options.MaxHeight); h > 0 {
			vf = append(vf, heightScale(h))
		}
		if every > 0 {
			args = append(args, "-i", in)
			vf = append([]string{"fps=" + strconv.FormatFloat(1/float64(every), 'f', 6, 64)}, vf...)
			args = append(args, "-vf", strings.Join(vf, ","), "-frames:v", itoa(MaxOutputs))
		} else {
			args = append(args, "-ss", itoa(at), "-i", in)
			if len(vf) > 0 {
				args = append(args, "-vf", strings.Join(vf, ","))
			}
			args = append(args, "-frames:v", "1")
		}
		if e.To == "jpg" {
			args = append(args, "-q:v", itoa(jpegScale(o.Int(options.Quality))))
		}
		args = append(args, stem+"-%03d."+ext(e.To))
		return Invocation{Args: args, TimeoutS: 600}, nil

	// audio → picture
	case fromCat == formats.Audio && (e.To == "png" || e.To == "jpg"):
		filter := "showwavespic=s=1600x480:colors=0x2563eb:split_channels=1"
		if o.String(options.Picture) == "spectrogram" {
			filter = "showspectrumpic=s=1600x480:legend=1"
		}
		args = append(args, "-i", in, "-filter_complex", "[0:a]"+filter+"[v]", "-map", "[v]", "-frames:v", "1")
		if e.To == "jpg" {
			args = append(args, "-q:v", itoa(jpegScale(o.Int(options.Quality))))
		}

	// audio → video (a drawn waveform or spectrum with the sound)
	case fromCat == formats.Audio && toCat == formats.Video:
		filter := "showwaves=s=1280x720:mode=cline:rate=25:colors=0x2563eb"
		if o.String(options.Picture) == "spectrogram" {
			filter = "showspectrum=s=1280x720:slide=scroll:legend=0"
		}
		args = append(args, "-i", in, "-filter_complex", "[0:a]"+filter+",format=yuv420p[v]", "-map", "[v]", "-map", "0:a")
		args = append(args, videoCodecs[e.To]...)
		args = append(args, videoQuality(e.To, crf, "medium")...)
		args = append(args, "-shortest")

	// anything with sound → audio
	case toCat == formats.Audio:
		args = append(args, "-i", in)
		if fromCat == formats.Video {
			args = append(args, "-vn")
		}
		bitrate := o.String(options.Bitrate)
		if _, err := strconv.Atoi(bitrate); err != nil {
			bitrate = "192"
		}
		args = append(args, audioCodecs[e.To](bitrate)...)
		if e.To == "m4a" {
			args = append(args, "-f", "mp4")
		}

	// subtitles
	case fromCat == formats.Subtitle && toCat == formats.Subtitle:
		args = append(args, "-i", in)

	default:
		return Invocation{}, fmt.Errorf("%w: %s", ErrUnsupported, e.Key())
	}
	args = append(args, out)
	return Invocation{Args: args, Outputs: []string{out}, TimeoutS: 840}, nil
}

// jpegScale maps the 1–100 quality knob onto ffmpeg's 2 (best) … 31 scale.
func jpegScale(q int) int {
	q = clamp(q, 1, 100)
	return clamp(31-(q*29)/100, 2, 31)
}

// ── ImageMagick ──

func magick(e graph.Edge, in, stem string, o options.Options) (Invocation, error) {
	out := OutputName(stem, e.To)
	var args []string
	switch e.From {
	case "pdf", "ps", "eps", "svg":
		args = append(args, "-density", itoa(clamp(o.Int(options.DPI), 36, 600)))
		if e.From == "svg" {
			args = append(args, "-background", "none")
		}
	}
	src := in
	switch e.From {
	case "pdf":
		if pr, err := options.ParsePages(o.String(options.Pages)); err == nil && pr.First > 0 {
			last := pr.Last
			if last == 0 {
				last = 9999
			}
			src = in + "[" + itoa(pr.First-1) + "-" + itoa(last-1) + "]"
		}
	case "ico", "dds":
		src = in + "[0]"
	}
	args = append(args, src)
	if e.From == "heic" || e.From == "psd" {
		args = append(args, "-auto-orient")
	}
	if e.From == "psd" || e.From == "xcf" {
		args = append(args, "-flatten")
	}
	if e.From == "exr" {
		args = append(args, "-colorspace", "sRGB")
	}
	switch e.To {
	case "jpg", "webp", "avif":
		args = append(args, "-quality", itoa(clamp(o.Int(options.Quality), 1, 100)))
		if e.To == "jpg" {
			args = append(args, "-background", "white", "-alpha", "remove", "-alpha", "off")
		}
	case "ico":
		args = append(args, "-define", "icon:auto-resize=256,128,64,48,32,16")
	case "pdf":
		args = append(args, "-compress", "jpeg", "-quality", "92")
	case "dds":
		args = append(args, "-define", "dds:compression=dxt5")
	}
	if e.Multi {
		// page files come back as <stem>-0.png, <stem>-1.png, …
		args = append(args, "-scene", "1", stem+"-%d."+ext(e.To))
		return Invocation{Args: args, TimeoutS: 600}, nil
	}
	args = append(args, out)
	return Invocation{Args: args, Outputs: []string{out}, TimeoutS: 300}, nil
}

// ── The office engine ──
//
// Since filex 0.50 the office engine is the connected ONLYOFFICE Document
// Server, and the host reads this same soffice command line as a conversion
// (pkg/pluginkit/officecmd): the target extension, PDF/A from the PDF
// option, the CSV separator and character set from the CSV options; the
// filter names themselves are not used. The line is kept as LibreOffice
// wrote it, so one shape serves both and nothing here depends on which
// program is behind the engine.

// sofficeFilter names the export filter per target. Writer/Calc/Impress
// each have their own filter for the same target extension, so the choice
// depends on the source family.
func sofficeFilter(from, to string, pdfa bool) string {
	family := "writer"
	switch from {
	case "xlsx", "ods", "csv":
		family = "calc"
	case "pptx", "odp":
		family = "impress"
	}
	switch to {
	case "pdf":
		f := "pdf:" + family + "_pdf_Export"
		if pdfa {
			f += `:{"SelectPdfVersion":{"type":"long","value":"2"}}`
		}
		return f
	case "docx":
		return "docx:MS Word 2007 XML"
	case "odt":
		return "odt"
	case "rtf":
		return "rtf:Rich Text Format"
	case "txt":
		return "txt:Text (encoded):UTF8"
	case "html":
		switch family {
		case "calc":
			return "html:HTML (StarCalc)"
		default:
			return "html:HTML (StarWriter)"
		}
	case "epub":
		return "epub:EPUB"
	case "xlsx":
		return "xlsx:Calc MS Excel 2007 XML"
	case "ods":
		return "ods"
	case "csv":
		return "csv:Text - txt - csv (StarCalc):44,34,76,1,,0,false,true,false,false,false,-1"
	case "pptx":
		return "pptx:Impress MS PowerPoint 2007 XML"
	case "odp":
		return "odp"
	}
	return to
}

func soffice(e graph.Edge, in, stem string, o options.Options) (Invocation, error) {
	filter := sofficeFilter(e.From, e.To, o.Bool(options.PDFA))
	args := []string{"--convert-to", filter, in}
	// soffice names the output after the input stem: in.docx → in.pdf
	produced := strings.TrimSuffix(in, "."+ext(e.From)) + "." + ext(e.To)
	inv := Invocation{Args: args, Outputs: []string{produced}, TimeoutS: 600}
	_ = stem
	return inv, nil
}

// ── Ghostscript ──

// pdfPresets spells Ghostscript's -dPDFSETTINGS presets out parameter by
// parameter: the preset names are PostScript names ("/ebook") and a "/" is
// a token the host refuses.
var pdfPresets = map[string][]string{
	"screen":   {"-dDownsampleColorImages=true", "-dColorImageResolution=72", "-dDownsampleGrayImages=true", "-dGrayImageResolution=72", "-dDownsampleMonoImages=true", "-dMonoImageResolution=300", "-dCompatibilityLevel=1.5"},
	"ebook":    {"-dDownsampleColorImages=true", "-dColorImageResolution=150", "-dDownsampleGrayImages=true", "-dGrayImageResolution=150", "-dDownsampleMonoImages=true", "-dMonoImageResolution=300", "-dCompatibilityLevel=1.5"},
	"printer":  {"-dDownsampleColorImages=true", "-dColorImageResolution=300", "-dDownsampleGrayImages=true", "-dGrayImageResolution=300", "-dDownsampleMonoImages=true", "-dMonoImageResolution=1200", "-dCompatibilityLevel=1.5"},
	"prepress": {"-dDownsampleColorImages=false", "-dDownsampleGrayImages=false", "-dDownsampleMonoImages=false", "-dCompatibilityLevel=1.5", "-dPreserveAnnots=true"},
}

func gsPages(args []string, o options.Options) []string {
	if pr, err := options.ParsePages(o.String(options.Pages)); err == nil && pr.First > 0 {
		args = append(args, "-dFirstPage="+itoa(pr.First))
		if pr.Last > 0 {
			args = append(args, "-dLastPage="+itoa(pr.Last))
		}
	}
	return args
}

func ghostscript(e graph.Edge, in, stem string, o options.Options) (Invocation, error) {
	out := OutputName(stem, e.To)
	args := []string{"-q"}
	switch e.To {
	case "pdf":
		args = append(args, "-sDEVICE=pdfwrite")
		if e.From == "pdf" {
			preset := o.String(options.Preset)
			p, ok := pdfPresets[preset]
			if !ok {
				p = pdfPresets["ebook"]
			}
			args = append(args, p...)
			args = append(args, "-dEmbedAllFonts=true", "-dSubsetFonts=true", "-dCompressFonts=true", "-dDetectDuplicateImages=true")
		}
		if e.From == "eps" {
			args = append(args, "-dEPSCrop")
		}
		if o.Bool(options.PDFA) {
			args = append(args, "-dPDFA=2", "-dPDFACompatibilityPolicy=1", "-sColorConversionStrategy=RGB")
		}
		args = append(args, "-sOutputFile="+out, in)
		return Invocation{Args: args, Outputs: []string{out}, TimeoutS: 600}, nil
	case "ps":
		args = append(args, "-sDEVICE=ps2write", "-sOutputFile="+out, in)
		return Invocation{Args: args, Outputs: []string{out}, TimeoutS: 600}, nil
	case "eps":
		args = append(args, "-sDEVICE=eps2write")
		page := 1
		if pr, err := options.ParsePages(o.String(options.Pages)); err == nil && pr.First > 0 {
			page = pr.First
		}
		args = append(args, "-dFirstPage="+itoa(page), "-dLastPage="+itoa(page), "-sOutputFile="+out, in)
		return Invocation{Args: args, Outputs: []string{out}, TimeoutS: 600}, nil
	case "txt":
		args = append(args, "-sDEVICE=txtwrite", "-sOutputFile="+out, in)
		return Invocation{Args: args, Outputs: []string{out}, TimeoutS: 300}, nil
	case "png", "jpg", "tiff":
		switch e.To {
		case "png":
			args = append(args, "-sDEVICE=png16m")
		case "jpg":
			args = append(args, "-sDEVICE=jpeg", "-dJPEGQ="+itoa(clamp(o.Int(options.Quality), 1, 100)))
		case "tiff":
			args = append(args, "-sDEVICE=tiff24nc")
		}
		args = append(args, "-r"+itoa(clamp(o.Int(options.DPI), 36, 600)), "-dTextAlphaBits=4", "-dGraphicsAlphaBits=4")
		if e.From == "eps" {
			args = append(args, "-dEPSCrop")
		}
		if e.From == "pdf" {
			args = gsPages(args, o)
		}
		if e.Multi {
			args = append(args, "-sOutputFile="+stem+"-%d."+ext(e.To), in)
			return Invocation{Args: args, TimeoutS: 600}, nil
		}
		args = append(args, "-sOutputFile="+out, in)
		return Invocation{Args: args, Outputs: []string{out}, TimeoutS: 600}, nil
	}
	return Invocation{}, fmt.Errorf("%w: %s", ErrUnsupported, e.Key())
}

// ── poppler ──

func poppler(e graph.Edge, in, stem string, o options.Options) (Invocation, error) {
	switch e.To {
	case "png", "jpg", "tiff":
		args := []string{"pdftoppm", "-r", itoa(clamp(o.Int(options.DPI), 36, 600))}
		switch e.To {
		case "png":
			args = append(args, "-png")
		case "jpg":
			args = append(args, "-jpeg", "-jpegopt", "quality="+itoa(clamp(o.Int(options.Quality), 1, 100)))
		case "tiff":
			args = append(args, "-tiff")
		}
		if pr, err := options.ParsePages(o.String(options.Pages)); err == nil && pr.First > 0 {
			args = append(args, "-f", itoa(pr.First))
			if pr.Last > 0 {
				args = append(args, "-l", itoa(pr.Last))
			}
		}
		// pdftoppm writes <stem>-<page>.png (zero-padded to the page count)
		args = append(args, in, stem)
		return Invocation{Args: args, TimeoutS: 600}, nil
	case "txt":
		out := OutputName(stem, "txt")
		return Invocation{Args: []string{"pdftotext", "-layout", "-enc", "UTF-8", in, out}, Outputs: []string{out}, TimeoutS: 300}, nil
	case "svg", "eps":
		out := OutputName(stem, e.To)
		page := 1
		if pr, err := options.ParsePages(o.String(options.Pages)); err == nil && pr.First > 0 {
			page = pr.First
		}
		return Invocation{Args: []string{"pdftocairo", "-" + e.To, "-f", itoa(page), "-l", itoa(page), in, out}, Outputs: []string{out}, TimeoutS: 300}, nil
	case "ps":
		out := OutputName(stem, "ps")
		return Invocation{Args: []string{"pdftocairo", "-ps", in, out}, Outputs: []string{out}, TimeoutS: 300}, nil
	}
	return Invocation{}, fmt.Errorf("%w: %s", ErrUnsupported, e.Key())
}

// ── rsvg ──

func rsvg(e graph.Edge, in, stem string, o options.Options) (Invocation, error) {
	out := OutputName(stem, e.To)
	args := []string{"--format=" + e.To}
	if e.To == "png" {
		dpi := itoa(clamp(o.Int(options.DPI), 36, 600))
		args = append(args, "--dpi-x="+dpi, "--dpi-y="+dpi)
	}
	args = append(args, "--output="+out, in)
	return Invocation{Args: args, Outputs: []string{out}, TimeoutS: 300}, nil
}

// ── slideshow (merge mode) ──

// Slideshow builds the ffmpeg run that turns frame-001.png … frame-NNN.png
// (placed in the run directory by the job layer) into one video or GIF,
// each frame shown for the Duration knob's seconds.
func Slideshow(target string, frames int, stem string, o options.Options) Invocation {
	out := OutputName(stem, target)
	dur := clamp(o.Int(options.Duration), 1, 600)
	rate := strconv.FormatFloat(1/float64(dur), 'f', 6, 64)
	args := []string{"-hide_banner", "-nostdin", "-y", "-framerate", rate, "-i", "frame-%03d.png"}
	if target == "gif" {
		h := o.Int(options.MaxHeight)
		if h <= 0 {
			h = 480
		}
		args = append(args, "-vf", heightScale(h)+":flags=lanczos,split[a][b];[a]palettegen[p];[b][p]paletteuse", "-loop", "0")
	} else {
		args = append(args, videoCodecs[target]...)
		args = append(args, videoQuality(target, clamp(o.Int(options.CRF), 0, 51), "medium")...)
		args = append(args, "-r", "25", "-vf", evenScale)
	}
	args = append(args, out)
	_ = frames
	return Invocation{Engine: graph.FFmpeg, Args: args, Outputs: []string{out}, TimeoutS: 840}
}
