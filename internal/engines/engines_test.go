package engines

import (
	"strings"
	"testing"

	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/options"
)

func edge(from, to, engine string) graph.Edge {
	for _, e := range graph.Direct(from, to) {
		if e.Engine == engine {
			return e
		}
	}
	panic("no edge " + from + ">" + to + "@" + engine)
}

// golden pins the exact argv per edge and option set. A change here is a
// behaviour change the CHANGELOG must mention.
var golden = []struct {
	name    string
	from    string
	to      string
	engine  string
	opts    options.Options
	want    string
	outputs []string
}{
	{"mp4→mkv defaults", "mp4", "mkv", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.mp4 -c:v libx264 -pix_fmt yuv420p -c:a aac -crf 23 -preset medium out.mkv", []string{"out.mkv"}},
	{"avi→mp4 fast 720p", "avi", "mp4", graph.FFmpeg, options.Options{"preset": "fast", "crf": 20, "max_height": 720},
		"-hide_banner -nostdin -y -i in.avi -c:v libx264 -pix_fmt yuv420p -c:a aac -movflags +faststart -crf 20 -preset fast -vf scale=-2:'min(ih,720)' out.mp4", []string{"out.mp4"}},
	{"mkv→mp4 remux", "mkv", "mp4", graph.FFmpeg, options.Options{"preset": "copy"},
		"-hide_banner -nostdin -y -i in.mkv -c copy -movflags +faststart out.mp4", []string{"out.mp4"}},
	{"mov→webm", "mov", "webm", graph.FFmpeg, options.Options{"crf": "31"},
		"-hide_banner -nostdin -y -i in.mov -c:v libvpx-vp9 -b:v 0 -row-mt 1 -c:a libopus -crf 31 out.webm", []string{"out.webm"}},
	{"mp4→gif", "mp4", "gif", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.mp4 -vf fps=12,scale=-2:'min(ih,480)':flags=lanczos,split[a][b];[a]palettegen[p];[b][p]paletteuse -loop 0 -an out.gif", []string{"out.gif"}},
	{"gif→mp4", "gif", "mp4", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.gif -c:v libx264 -pix_fmt yuv420p -c:a aac -movflags +faststart -crf 23 -preset medium -an -vf scale=trunc(iw*0.5)*2:trunc(ih*0.5)*2 out.mp4", []string{"out.mp4"}},
	{"mp4→webp animated", "mp4", "webp", graph.FFmpeg, options.Options{"max_height": 320},
		"-hide_banner -nostdin -y -i in.mp4 -vf fps=12,scale=-2:'min(ih,320)':flags=lanczos -c:v libwebp -lossless 0 -q:v 75 -loop 0 -an out.webp", []string{"out.webp"}},
	{"mkv→apng", "mkv", "apng", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.mkv -vf fps=12,scale=-2:'min(ih,480)':flags=lanczos -f apng -plays 0 -an out.apng", []string{"out.apng"}},
	{"mp4→wmv", "mp4", "wmv", graph.FFmpeg, options.Options{"crf": 34},
		"-hide_banner -nostdin -y -i in.mp4 -c:v wmv2 -pix_fmt yuv420p -c:a wmav2 -q:v 20 out.wmv", []string{"out.wmv"}},
	{"avi→ogv", "avi", "ogv", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.avi -c:v libtheora -pix_fmt yuv420p -c:a libvorbis -q:v 6 out.ogv", []string{"out.ogv"}},
	{"mov→mpg", "mov", "mpg", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.mov -c:v mpeg2video -pix_fmt yuv420p -c:a mp2 -q:v 13 out.mpg", []string{"out.mpg"}},
	{"mp4→png one frame at 12 s", "mp4", "png", graph.FFmpeg, options.Options{"at": 12},
		"-hide_banner -nostdin -y -ss 12 -i in.mp4 -frames:v 1 out-%03d.png", nil},
	{"mp4→jpg every 5 s, 360p", "mp4", "jpg", graph.FFmpeg, options.Options{"every": 5, "max_height": 360, "quality": 90},
		"-hide_banner -nostdin -y -i in.mp4 -vf fps=0.200000,scale=-2:'min(ih,360)' -frames:v 64 -q:v 5 out-%03d.jpg", nil},
	{"mp3→png waveform", "mp3", "png", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.mp3 -filter_complex [0:a]showwavespic=s=1600x480:colors=0x2563eb:split_channels=1[v] -map [v] -frames:v 1 out.png", []string{"out.png"}},
	{"mp3→jpg spectrogram", "mp3", "jpg", graph.FFmpeg, options.Options{"picture": "spectrogram", "quality": 100},
		"-hide_banner -nostdin -y -i in.mp3 -filter_complex [0:a]showspectrumpic=s=1600x480:legend=1[v] -map [v] -frames:v 1 -q:v 2 out.jpg", []string{"out.jpg"}},
	{"wav→mp4 waveform video", "wav", "mp4", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.wav -filter_complex [0:a]showwaves=s=1280x720:mode=cline:rate=25:colors=0x2563eb,format=yuv420p[v] -map [v] -map 0:a -c:v libx264 -pix_fmt yuv420p -c:a aac -movflags +faststart -crf 23 -preset medium -shortest out.mp4", []string{"out.mp4"}},
	{"png→mp4 still 8 s", "png", "mp4", graph.FFmpeg, options.Options{"duration": 8},
		"-hide_banner -nostdin -y -loop 1 -framerate 25 -i in.png -t 8 -c:v libx264 -pix_fmt yuv420p -c:a aac -movflags +faststart -crf 23 -preset medium -an -vf scale=trunc(iw*0.5)*2:trunc(ih*0.5)*2 out.mp4", []string{"out.mp4"}},
	{"flac→wma", "flac", "wma", graph.FFmpeg, options.Options{"bitrate": "128"},
		"-hide_banner -nostdin -y -i in.flac -c:a wmav2 -b:a 128k out.wma", []string{"out.wma"}},
	{"mp3→aiff", "mp3", "aiff", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.mp3 -c:a pcm_s16be out.aiff", []string{"out.aiff"}},
	{"srt→ass", "srt", "ass", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.srt out.ass", []string{"out.ass"}},
	{"mp4→mp3 extract", "mp4", "mp3", graph.FFmpeg, options.Options{"bitrate": "320"},
		"-hide_banner -nostdin -y -i in.mp4 -vn -c:a libmp3lame -b:a 320k out.mp3", []string{"out.mp3"}},
	{"wav→flac", "wav", "flac", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.wav -c:a flac out.flac", []string{"out.flac"}},
	{"flac→m4a", "flac", "m4a", graph.FFmpeg, nil,
		"-hide_banner -nostdin -y -i in.flac -c:a aac -b:a 192k -f mp4 out.m4a", []string{"out.m4a"}},
	{"mp3→opus bad bitrate falls back", "mp3", "opus", graph.FFmpeg, options.Options{"bitrate": "lots"},
		"-hide_banner -nostdin -y -i in.mp3 -c:a libopus -b:a 192k out.opus", []string{"out.opus"}},

	{"heic→jpg", "heic", "jpg", graph.ImageMagick, options.Options{"quality": 80},
		"in.heic -auto-orient -quality 80 -background white -alpha remove -alpha off out.jpg", []string{"out.jpg"}},
	{"psd→png", "psd", "png", graph.ImageMagick, nil,
		"in.psd -auto-orient -flatten out.png", []string{"out.png"}},
	{"png→webp", "png", "webp", graph.ImageMagick, nil,
		"in.png -quality 85 out.webp", []string{"out.webp"}},
	{"png→ico", "png", "ico", graph.ImageMagick, nil,
		"in.png -define icon:auto-resize=256,128,64,48,32,16 out.ico", []string{"out.ico"}},
	{"jpg→pdf", "jpg", "pdf", graph.ImageMagick, nil,
		"in.jpg -compress jpeg -quality 92 out.pdf", []string{"out.pdf"}},
	{"svg→png magick", "svg", "png", graph.ImageMagick, options.Options{"dpi": 300},
		"-density 300 -background none in.svg out.png", []string{"out.png"}},
	{"pdf→png magick pages 2-3", "pdf", "png", graph.ImageMagick, options.Options{"pages": "2-3"},
		"-density 150 in.pdf[1-2] -scene 1 out-%d.png", nil},
	{"xcf→png", "xcf", "png", graph.ImageMagick, nil,
		"in.xcf -flatten out.png", []string{"out.png"}},
	{"ico→png magick", "ico", "png", graph.ImageMagick, nil,
		"in.ico[0] out.png", []string{"out.png"}},
	{"exr→jpg", "exr", "jpg", graph.ImageMagick, nil,
		"in.exr -colorspace sRGB -quality 85 -background white -alpha remove -alpha off out.jpg", []string{"out.jpg"}},
	{"png→dds", "png", "dds", graph.ImageMagick, nil,
		"in.png -define dds:compression=dxt5 out.dds", []string{"out.dds"}},

	{"docx→pdf", "docx", "pdf", graph.Office, nil,
		"--convert-to pdf:writer_pdf_Export in.docx", []string{"in.pdf"}},
	{"docx→pdf pdfa", "docx", "pdf", graph.Office, options.Options{"pdfa": true},
		`--convert-to pdf:writer_pdf_Export:{"SelectPdfVersion":{"type":"long","value":"2"}} in.docx`, []string{"in.pdf"}},
	{"xlsx→pdf", "xlsx", "pdf", graph.Office, nil,
		"--convert-to pdf:calc_pdf_Export in.xlsx", []string{"in.pdf"}},
	{"pptx→pdf", "pptx", "pdf", graph.Office, nil,
		"--convert-to pdf:impress_pdf_Export in.pptx", []string{"in.pdf"}},
	{"odt→docx", "odt", "docx", graph.Office, nil,
		"--convert-to docx:MS Word 2007 XML in.odt", []string{"in.docx"}},
	{"xlsx→csv", "xlsx", "csv", graph.Office, nil,
		"--convert-to csv:Text - txt - csv (StarCalc):44,34,76,1,,0,false,true,false,false,false,-1 in.xlsx", []string{"in.csv"}},
	{"docx→txt", "docx", "txt", graph.Office, nil,
		"--convert-to txt:Text (encoded):UTF8 in.docx", []string{"in.txt"}},
	{"csv→xlsx", "csv", "xlsx", graph.Office, nil,
		"--convert-to xlsx:Calc MS Excel 2007 XML in.csv", []string{"in.xlsx"}},
	{"docx→epub", "docx", "epub", graph.Office, nil,
		"--convert-to epub:EPUB in.docx", []string{"in.epub"}},
	{"epub→pdf", "epub", "pdf", graph.Office, nil,
		"--convert-to pdf:writer_pdf_Export in.epub", []string{"in.pdf"}},

	{"pdf→pdf ebook", "pdf", "pdf", graph.Ghostscript, nil,
		"-q -sDEVICE=pdfwrite -dDownsampleColorImages=true -dColorImageResolution=150 -dDownsampleGrayImages=true -dGrayImageResolution=150 -dDownsampleMonoImages=true -dMonoImageResolution=300 -dCompatibilityLevel=1.5 -dEmbedAllFonts=true -dSubsetFonts=true -dCompressFonts=true -dDetectDuplicateImages=true -sOutputFile=out.pdf in.pdf", []string{"out.pdf"}},
	{"pdf→pdf screen pdfa", "pdf", "pdf", graph.Ghostscript, options.Options{"preset": "screen", "pdfa": "true"},
		"-q -sDEVICE=pdfwrite -dDownsampleColorImages=true -dColorImageResolution=72 -dDownsampleGrayImages=true -dGrayImageResolution=72 -dDownsampleMonoImages=true -dMonoImageResolution=300 -dCompatibilityLevel=1.5 -dEmbedAllFonts=true -dSubsetFonts=true -dCompressFonts=true -dDetectDuplicateImages=true -dPDFA=2 -dPDFACompatibilityPolicy=1 -sColorConversionStrategy=RGB -sOutputFile=out.pdf in.pdf", []string{"out.pdf"}},
	{"eps→pdf", "eps", "pdf", graph.Ghostscript, nil,
		"-q -sDEVICE=pdfwrite -dEPSCrop -sOutputFile=out.pdf in.eps", []string{"out.pdf"}},
	{"pdf→ps", "pdf", "ps", graph.Ghostscript, nil,
		"-q -sDEVICE=ps2write -sOutputFile=out.ps in.pdf", []string{"out.ps"}},
	{"pdf→jpg gs pages 3-", "pdf", "jpg", graph.Ghostscript, options.Options{"pages": "3-", "dpi": 96, "quality": 70},
		"-q -sDEVICE=jpeg -dJPEGQ=70 -r96 -dTextAlphaBits=4 -dGraphicsAlphaBits=4 -dFirstPage=3 -sOutputFile=out-%d.jpg in.pdf", nil},
	{"pdf→eps page 2", "pdf", "eps", graph.Ghostscript, options.Options{"pages": "2"},
		"-q -sDEVICE=eps2write -dFirstPage=2 -dLastPage=2 -sOutputFile=out.eps in.pdf", []string{"out.eps"}},
	{"pdf→tiff gs", "pdf", "tiff", graph.Ghostscript, nil,
		"-q -sDEVICE=tiff24nc -r150 -dTextAlphaBits=4 -dGraphicsAlphaBits=4 -sOutputFile=out-%d.tiff in.pdf", nil},
	{"eps→png gs", "eps", "png", graph.Ghostscript, options.Options{"dpi": 300},
		"-q -sDEVICE=png16m -r300 -dTextAlphaBits=4 -dGraphicsAlphaBits=4 -dEPSCrop -sOutputFile=out.png in.eps", []string{"out.png"}},
	{"pdf→txt gs", "pdf", "txt", graph.Ghostscript, nil,
		"-q -sDEVICE=txtwrite -sOutputFile=out.txt in.pdf", []string{"out.txt"}},

	{"pdf→png poppler", "pdf", "png", graph.Poppler, options.Options{"dpi": 200, "pages": "1-2"},
		"pdftoppm -r 200 -png -f 1 -l 2 in.pdf out", nil},
	{"pdf→jpg poppler", "pdf", "jpg", graph.Poppler, options.Options{"quality": 75},
		"pdftoppm -r 150 -jpeg -jpegopt quality=75 in.pdf out", nil},
	{"pdf→txt", "pdf", "txt", graph.Poppler, nil,
		"pdftotext -layout -enc UTF-8 in.pdf out.txt", []string{"out.txt"}},
	{"pdf→svg page 2", "pdf", "svg", graph.Poppler, options.Options{"pages": "2"},
		"pdftocairo -svg -f 2 -l 2 in.pdf out.svg", []string{"out.svg"}},
	{"pdf→tiff poppler", "pdf", "tiff", graph.Poppler, nil,
		"pdftoppm -r 150 -tiff in.pdf out", nil},
	{"pdf→eps poppler", "pdf", "eps", graph.Poppler, nil,
		"pdftocairo -eps -f 1 -l 1 in.pdf out.eps", []string{"out.eps"}},
	{"pdf→ps poppler", "pdf", "ps", graph.Poppler, nil,
		"pdftocairo -ps in.pdf out.ps", []string{"out.ps"}},

	{"svg→png rsvg", "svg", "png", graph.RSVG, nil,
		"--format=png --dpi-x=150 --dpi-y=150 --output=out.png in.svg", []string{"out.png"}},
	{"svg→pdf rsvg", "svg", "pdf", graph.RSVG, nil,
		"--format=pdf --output=out.pdf in.svg", []string{"out.pdf"}},
	{"svg→eps rsvg", "svg", "eps", graph.RSVG, nil,
		"--format=eps --output=out.eps in.svg", []string{"out.eps"}},
}

func TestSlideshow(t *testing.T) {
	inv := Slideshow("mp4", 3, "out", options.Options{"duration": 2})
	got := strings.Join(inv.Args, " ")
	want := "-hide_banner -nostdin -y -framerate 0.500000 -i frame-%03d.png -c:v libx264 -pix_fmt yuv420p -c:a aac -movflags +faststart -crf 23 -preset medium -r 25 -vf scale=trunc(iw*0.5)*2:trunc(ih*0.5)*2 out.mp4"
	if got != want || inv.Engine != graph.FFmpeg || inv.Outputs[0] != "out.mp4" {
		t.Errorf("slideshow argv got: %s want: %s", got, want)
	}
	inv = Slideshow("gif", 2, "out", nil)
	if !strings.Contains(strings.Join(inv.Args, " "), "palettegen") || inv.Outputs[0] != "out.gif" {
		t.Errorf("gif slideshow %v", inv.Args)
	}
	for _, a := range inv.Args {
		if err := CheckArg(a); err != nil {
			t.Error(err)
		}
	}
}

func TestGolden(t *testing.T) {
	for _, g := range golden {
		t.Run(g.name, func(t *testing.T) {
			e := edge(g.from, g.to, g.engine)
			inv, err := Build(e, InputName(g.from), "out", g.opts)
			if err != nil {
				t.Fatal(err)
			}
			if got := strings.Join(inv.Args, " "); got != g.want {
				t.Errorf("argv\n got: %s\nwant: %s", got, g.want)
			}
			if strings.Join(inv.Outputs, ",") != strings.Join(g.outputs, ",") {
				t.Errorf("outputs %v, want %v", inv.Outputs, g.outputs)
			}
			if inv.Engine != g.engine || inv.Input != InputName(g.from) {
				t.Errorf("engine/input %s %s", inv.Engine, inv.Input)
			}
			if inv.Multi != e.Multi {
				t.Errorf("multi %v", inv.Multi)
			}
		})
	}
}

// Every engine edge in the graph must build with default options and pass
// the host's argument rule; otherwise the route is a promise we cannot keep.
func TestEveryEngineEdgeBuilds(t *testing.T) {
	n := 0
	for _, e := range graph.Edges() {
		if e.Engine == graph.PureGo {
			continue
		}
		n++
		inv, err := Build(e, InputName(e.From), "out", nil)
		if err != nil {
			t.Errorf("%s: %v", e.Key(), err)
			continue
		}
		if len(inv.Args) == 0 {
			t.Errorf("%s: empty argv", e.Key())
		}
		// the input must be named in the argv and every arg is bare
		found := false
		for _, a := range inv.Args {
			if strings.Contains(a, inv.Input) {
				found = true
			}
			if err := CheckArg(a); err != nil {
				t.Errorf("%s: %v", e.Key(), err)
			}
		}
		if !found {
			t.Errorf("%s: argv does not name the input", e.Key())
		}
		if !e.Multi && len(inv.Outputs) != 1 {
			t.Errorf("%s: single-output edge should name its output, got %v", e.Key(), inv.Outputs)
		}
	}
	if n < 100 {
		t.Errorf("only %d engine edges", n)
	}
}

func TestCheckArg(t *testing.T) {
	bad := []string{"", "../x", "a/b", `a\b`, "@list.txt", "file:x", "-i=http://x", "-dNOSAFER", "--infilter=x", "-sDEVICE=pipe", "%pipe%x", "-sOutputFile=%pipe%x", "concat:a|b"}
	for _, a := range bad {
		if CheckArg(a) == nil {
			t.Errorf("%q should be refused", a)
		}
	}
	good := []string{"-i", "in.mp4", "scale=-2:'min(ih,720)'", "-sOutputFile=out.pdf", `pdf:writer_pdf_Export:{"SelectPdfVersion":{"type":"long","value":"2"}}`, "in.pdf[0-2]", "out-%d.png", "-crf", "23", "http-not-a-scheme"}
	for _, a := range good {
		if err := CheckArg(a); err != nil {
			t.Errorf("%q should pass: %v", a, err)
		}
	}
}

func TestUnsupportedEdge(t *testing.T) {
	if _, err := Build(graph.Edge{From: "png", To: "jpg", Engine: graph.PureGo}, "in.png", "out", nil); err == nil {
		t.Error("pure-Go edge has no engine builder")
	}
	if _, err := Build(graph.Edge{From: "png", To: "pdf", Engine: graph.FFmpeg}, "in.png", "out", nil); err == nil {
		t.Error("png→pdf through ffmpeg is unsupported")
	}
}

func TestNames(t *testing.T) {
	if InputName("tgz") != "in.tar.gz" || InputName("nope") != "in.bin" {
		t.Error("InputName")
	}
	if OutputName("out", "jpg") != "out.jpg" {
		t.Error("OutputName")
	}
}
