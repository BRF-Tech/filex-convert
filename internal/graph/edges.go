package graph

import (
	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/options"
)

// The edge table. Costs: 1 is the ordinary hop; an engine that is a
// fallback for something pure Go (or a better engine) already does gets a
// higher cost so the planner prefers the cheaper path and RouteExcluding
// still finds the alternative when the first one fails. Pure-Go document
// edges that the office engine renders with better fidelity cost 1.6, so
// the office engine wins when ONLYOFFICE is connected and the sandbox still
// delivers when it is not.
var edges = buildEdges()

// GoImages are the raster ids the sandbox codes on its own (mirrors
// purego.GoImages; the tests check they agree).
var GoImages = []string{"png", "jpg", "gif", "bmp", "tiff", "webp", "qoi", "pnm", "tga", "ico"}

// ArchiveWritable are the archive ids the sandbox can write; every other
// archive id it can only read.
var ArchiveWritable = []string{"zip", "tar", "tgz", "tzst", "txz"}

// IsPack says whether an edge is the "put this file into an archive" edge
// (documented once in MATRIX.md instead of per cell).
func IsPack(e Edge) bool {
	return formats.CategoryOf(e.To) == formats.Archive && formats.CategoryOf(e.From) != formats.Archive
}

func setOf(ids ...string) map[string]bool {
	m := map[string]bool{}
	for _, id := range ids {
		m[id] = true
	}
	return m
}

func buildEdges() []Edge {
	var es []Edge
	add := func(list ...Edge) { es = append(es, list...) }

	// pairwise adds from×to (from != to) with the same attributes; lossyTo
	// marks targets whose encoding loses information (they take opts).
	pairwise := func(from, to []string, engine string, cost float64, lossyTo map[string]bool, opts []string, variant string) {
		for _, f := range from {
			for _, t := range to {
				if f == t {
					continue
				}
				e := Edge{From: f, To: t, Engine: engine, Cost: cost, Lossy: lossyTo[t], Variant: variant}
				if lossyTo[t] {
					e.Options = opts
				}
				add(e)
			}
		}
	}

	// ── pure Go: images ──
	lossyImg := setOf("jpg", "gif", "ico")
	pairwise(GoImages, GoImages, PureGo, 1, lossyImg, nil, "")
	for i := range es {
		switch es[i].To {
		case "jpg":
			es[i].Options = []string{options.Quality}
		case "ico":
			es[i].Options = []string{options.IconSizes}
		}
	}
	for _, r := range GoImages {
		add(
			Edge{From: r, To: "pdf", Engine: PureGo, Cost: 1},
			Edge{From: r, To: "svg", Engine: PureGo, Cost: 1.2, Terminal: true},
			Edge{From: r, To: "txt", Engine: PureGo, Cost: 1.2, Lossy: true, Terminal: true},
		)
	}

	// ── pure Go: archives ──
	archiveReadable := []string{"zip", "tar", "tgz", "tzst", "txz", "tbz2", "rar"}
	pairwise(archiveReadable, ArchiveWritable, PureGo, 1, nil, nil, "")
	for _, f := range formats.All {
		if f.Category == formats.Archive {
			continue
		}
		for _, to := range ArchiveWritable {
			add(Edge{From: f.ID, To: to, Engine: PureGo, Cost: 2.5, Initial: true, Terminal: true})
		}
	}

	// ── pure Go: data ──
	tree := []string{"json", "yaml", "xml", "toml"}
	pairwise(tree, tree, PureGo, 1, nil, nil, "")
	rowsLossy := setOf("csv", "tsv")
	pairwise([]string{"csv", "tsv"}, []string{"json", "yaml", "csv", "tsv"}, PureGo, 1, nil, nil, "")
	pairwise([]string{"json", "yaml"}, []string{"csv", "tsv"}, PureGo, 1, rowsLossy, nil, "")
	pairwise([]string{"csv", "tsv"}, []string{"md", "html", "xlsx"}, PureGo, 1, nil, nil, "")
	pairwise([]string{"json", "yaml"}, []string{"xlsx"}, PureGo, 1.2, nil, nil, "")
	pairwise([]string{"xlsx", "ods"}, []string{"csv", "tsv", "json", "yaml", "md", "html"}, PureGo, 1.2, setOf("csv", "tsv", "json", "yaml", "md", "html"), nil, "")
	add(Edge{From: "ods", To: "xlsx", Engine: PureGo, Cost: 1.4, Lossy: true})
	pairwise([]string{"xml", "toml"}, []string{"csv"}, PureGo, 1.2, rowsLossy, nil, "")

	// ── pure Go: documents ──
	docReaders := []string{"md", "html", "txt", "docx", "odt", "rtf", "pptx", "epub"}
	docWriters := []string{"md", "html", "txt", "pdf", "docx", "epub"}
	officeSrc := setOf("docx", "odt", "rtf", "pptx", "epub")
	for _, f := range docReaders {
		for _, t := range docWriters {
			if f == t {
				continue
			}
			if f == "md" && t == "html" || f == "txt" && t == "html" || f == "html" && t == "txt" || f == "txt" && t == "md" {
				continue
			}
			e := Edge{From: f, To: t, Engine: PureGo, Cost: 1}
			switch {
			case officeSrc[f]:
				e.Cost = 1.6
				e.Lossy = t != "pdf" && t != "docx"
			case f == "html" && (t == "pdf" || t == "docx"):
				e.Cost = 1.2
			}
			if t == "txt" || t == "md" && f != "txt" {
				e.Lossy = true
			}
			add(e)
		}
	}

	add(
		Edge{From: "md", To: "html", Engine: PureGo, Cost: 1},
		Edge{From: "txt", To: "html", Engine: PureGo, Cost: 1},
		Edge{From: "html", To: "txt", Engine: PureGo, Cost: 1, Lossy: true},
		Edge{From: "txt", To: "md", Engine: PureGo, Cost: 1},
	)

	// ── pure Go: subtitles, fonts ──
	add(
		Edge{From: "srt", To: "vtt", Engine: PureGo, Cost: 1},
		Edge{From: "vtt", To: "srt", Engine: PureGo, Cost: 1, Lossy: true},
		Edge{From: "srt", To: "txt", Engine: PureGo, Cost: 1, Lossy: true, Terminal: true},
		Edge{From: "vtt", To: "txt", Engine: PureGo, Cost: 1, Lossy: true, Terminal: true},
		Edge{From: "ttf", To: "woff", Engine: PureGo, Cost: 1},
		Edge{From: "otf", To: "woff", Engine: PureGo, Cost: 1},
		Edge{From: "woff", To: "ttf", Engine: PureGo, Cost: 1},
		Edge{From: "woff", To: "otf", Engine: PureGo, Cost: 1},
		Edge{From: "ttf", To: "otf", Engine: PureGo, Cost: 1},
	)

	// ── ffmpeg ──
	videos := formats.IDsIn(formats.Video)
	audios := formats.IDsIn(formats.Audio)
	videoOpts := []string{options.Preset, options.CRF, options.MaxHeight}
	pairwise(videos, videos, FFmpeg, 1, setOf(videos...), videoOpts, "video")
	animated := []string{"gif", "webp", "apng"}
	for _, v := range videos {
		for _, a := range animated {
			add(Edge{From: v, To: a, Engine: FFmpeg, Cost: 1, Lossy: true, Options: []string{options.MaxHeight}, Terminal: true})
		}
		add(
			Edge{From: v, To: "png", Engine: FFmpeg, Cost: 1, Lossy: true, Options: []string{options.At, options.Every, options.MaxHeight}, Multi: true, Terminal: true},
			Edge{From: v, To: "jpg", Engine: FFmpeg, Cost: 1, Lossy: true, Options: []string{options.At, options.Every, options.MaxHeight, options.Quality}, Multi: true, Terminal: true},
		)
		for _, a := range audios {
			add(Edge{From: v, To: a, Engine: FFmpeg, Cost: 1, Lossy: true, Options: []string{options.Bitrate}})
		}
	}
	for _, a := range animated {
		for _, v := range []string{"mp4", "webm", "mkv", "mov"} {
			add(Edge{From: a, To: v, Engine: FFmpeg, Cost: 1, Lossy: true, Options: []string{options.CRF}, Initial: true})
		}
		for _, b := range animated {
			if a != b {
				add(Edge{From: a, To: b, Engine: FFmpeg, Cost: 1.3, Lossy: true, Options: []string{options.MaxHeight}, Initial: true})
			}
		}
	}
	lossyAudio := setOf("mp3", "ogg", "aac", "m4a", "opus", "wma", "ac3")
	pairwise(audios, audios, FFmpeg, 1, lossyAudio, []string{options.Bitrate}, "")
	for _, a := range audios {
		add(
			Edge{From: a, To: "png", Engine: FFmpeg, Cost: 1, Lossy: true, Options: []string{options.Picture}, Terminal: true},
			Edge{From: a, To: "jpg", Engine: FFmpeg, Cost: 1, Lossy: true, Options: []string{options.Picture, options.Quality}, Terminal: true},
		)
		for _, v := range []string{"mp4", "webm", "mkv"} {
			add(Edge{From: a, To: v, Engine: FFmpeg, Cost: 1.2, Lossy: true, Options: []string{options.Picture, options.CRF}, Terminal: true})
		}
	}
	ffImages := []string{"png", "jpg", "bmp", "tiff", "pnm", "tga"}
	for _, i := range ffImages {
		for _, v := range []string{"mp4", "webm", "mkv", "mov"} {
			add(Edge{From: i, To: v, Engine: FFmpeg, Cost: 1.2, Lossy: true, Options: []string{options.Duration, options.CRF}, Initial: true, Terminal: true})
		}
	}
	subs := []string{"srt", "vtt", "ass"}
	pairwise(subs, subs, FFmpeg, 1.3, setOf("srt", "vtt", "ass"), nil, "")

	// ── ImageMagick ──
	rasters := []string{"png", "jpg", "gif", "bmp", "tiff", "webp"}
	magickOnly := []string{"heic", "psd", "avif", "xcf", "dds", "exr"}
	lossyRaster := setOf("jpg", "gif", "webp", "avif")
	qual := []string{options.Quality}
	pairwise(magickOnly, []string{"png", "jpg", "webp", "tiff", "bmp", "gif"}, ImageMagick, 1, lossyRaster, qual, "")
	pairwise(rasters, rasters, ImageMagick, 1.5, lossyRaster, qual, "")
	for _, r := range rasters {
		add(
			Edge{From: r, To: "avif", Engine: ImageMagick, Cost: 1, Lossy: true, Options: qual},
			Edge{From: r, To: "pdf", Engine: ImageMagick, Cost: 1.5},
			Edge{From: r, To: "ico", Engine: ImageMagick, Cost: 1.5, Lossy: true},
			Edge{From: r, To: "psd", Engine: ImageMagick, Cost: 1},
			Edge{From: r, To: "dds", Engine: ImageMagick, Cost: 1, Lossy: true},
			Edge{From: r, To: "exr", Engine: ImageMagick, Cost: 1},
		)
	}
	add(
		Edge{From: "ico", To: "png", Engine: ImageMagick, Cost: 1.5},
		Edge{From: "svg", To: "png", Engine: ImageMagick, Cost: 1.3, Options: []string{options.DPI}},
		Edge{From: "svg", To: "jpg", Engine: ImageMagick, Cost: 1.3, Lossy: true, Options: []string{options.DPI, options.Quality}},
		Edge{From: "svg", To: "webp", Engine: ImageMagick, Cost: 1.3, Lossy: true, Options: []string{options.DPI, options.Quality}},
		Edge{From: "pdf", To: "png", Engine: ImageMagick, Cost: 1.5, Lossy: true, Options: []string{options.DPI, options.Pages}, Multi: true},
		Edge{From: "pdf", To: "jpg", Engine: ImageMagick, Cost: 1.5, Lossy: true, Options: []string{options.DPI, options.Pages, options.Quality}, Multi: true},
		Edge{From: "eps", To: "png", Engine: ImageMagick, Cost: 1.5, Lossy: true, Options: []string{options.DPI}},
		Edge{From: "ps", To: "png", Engine: ImageMagick, Cost: 1.5, Lossy: true, Options: []string{options.DPI}, Multi: true},
	)

	// ── The office engine (ONLYOFFICE, filex 0.50) ──
	//
	// Every edge here was measured on ONLYOFFICE Document Server 9.4 (filex
	// 0.50, 2026-10-02). What it does not make is not an edge: no HTML from a
	// spreadsheet (the conversion API answers -7) - the sandbox's own route
	// makes that one, so the target stays on offer without the engine.
	writers := []string{"docx", "doc", "odt", "rtf"}
	writerTargets := []string{"pdf", "docx", "odt", "rtf", "html", "txt", "epub"}
	lossyWriter := setOf("rtf", "txt", "html", "doc")
	pdfa := []string{options.PDFA}
	for _, f := range writers {
		for _, t := range writerTargets {
			if f == t {
				continue
			}
			e := Edge{From: f, To: t, Engine: Office, Cost: 1, Lossy: lossyWriter[t]}
			if t == "pdf" {
				e.Options = pdfa
			}
			add(e)
		}
	}
	for _, f := range []string{"html", "txt", "epub"} {
		for _, t := range []string{"pdf", "docx", "odt"} {
			e := Edge{From: f, To: t, Engine: Office, Cost: 1}
			if f == "epub" {
				e.Cost = 1.2
			}
			if t == "pdf" {
				e.Options = pdfa
			}
			add(e)
		}
	}
	calcs := []string{"xlsx", "ods"}
	calcTargets := []string{"pdf", "xlsx", "ods", "csv"}
	lossyCalc := setOf("csv")
	for _, f := range calcs {
		for _, t := range calcTargets {
			if f == t {
				continue
			}
			e := Edge{From: f, To: t, Engine: Office, Cost: 1, Lossy: lossyCalc[t]}
			if t == "pdf" {
				e.Options = pdfa
			}
			add(e)
		}
	}
	add(
		Edge{From: "csv", To: "xlsx", Engine: Office, Cost: 1.2},
		Edge{From: "csv", To: "ods", Engine: Office, Cost: 1},
		Edge{From: "csv", To: "pdf", Engine: Office, Cost: 1, Options: pdfa},
	)
	impress := []string{"pptx", "odp"}
	for _, f := range impress {
		for _, t := range []string{"pdf", "pptx", "odp"} {
			if f == t {
				continue
			}
			e := Edge{From: f, To: t, Engine: Office, Cost: 1}
			if t == "pdf" {
				e.Options = pdfa
			}
			add(e)
		}
	}

	// ── Ghostscript ──
	add(
		Edge{From: "pdf", To: "pdf", Engine: Ghostscript, Cost: 1, Lossy: true, Options: []string{options.Preset, options.PDFA}, Variant: "pdf"},
		Edge{From: "ps", To: "pdf", Engine: Ghostscript, Cost: 1},
		Edge{From: "eps", To: "pdf", Engine: Ghostscript, Cost: 1},
		Edge{From: "pdf", To: "ps", Engine: Ghostscript, Cost: 1},
		Edge{From: "pdf", To: "eps", Engine: Ghostscript, Cost: 1, Options: []string{options.Pages}},
		Edge{From: "pdf", To: "png", Engine: Ghostscript, Cost: 1.3, Lossy: true, Options: []string{options.DPI, options.Pages}, Multi: true},
		Edge{From: "pdf", To: "jpg", Engine: Ghostscript, Cost: 1.3, Lossy: true, Options: []string{options.DPI, options.Pages, options.Quality}, Multi: true},
		Edge{From: "pdf", To: "tiff", Engine: Ghostscript, Cost: 1.3, Lossy: true, Options: []string{options.DPI, options.Pages}, Multi: true},
		Edge{From: "pdf", To: "txt", Engine: Ghostscript, Cost: 1.4, Lossy: true, Terminal: true, Initial: true},
		Edge{From: "ps", To: "png", Engine: Ghostscript, Cost: 1.2, Lossy: true, Options: []string{options.DPI}, Multi: true},
		Edge{From: "ps", To: "jpg", Engine: Ghostscript, Cost: 1.2, Lossy: true, Options: []string{options.DPI, options.Quality}, Multi: true},
		Edge{From: "eps", To: "png", Engine: Ghostscript, Cost: 1.2, Lossy: true, Options: []string{options.DPI}},
		Edge{From: "eps", To: "jpg", Engine: Ghostscript, Cost: 1.2, Lossy: true, Options: []string{options.DPI, options.Quality}},
	)

	// ── poppler ──
	add(
		Edge{From: "pdf", To: "png", Engine: Poppler, Cost: 1, Lossy: true, Options: []string{options.DPI, options.Pages}, Multi: true},
		Edge{From: "pdf", To: "jpg", Engine: Poppler, Cost: 1, Lossy: true, Options: []string{options.DPI, options.Pages, options.Quality}, Multi: true},
		Edge{From: "pdf", To: "tiff", Engine: Poppler, Cost: 1, Lossy: true, Options: []string{options.DPI, options.Pages}, Multi: true},
		Edge{From: "pdf", To: "txt", Engine: Poppler, Cost: 1, Lossy: true, Terminal: true, Initial: true},
		Edge{From: "pdf", To: "svg", Engine: Poppler, Cost: 1, Lossy: true, Options: []string{options.Pages}, Terminal: true, Initial: true},
		Edge{From: "pdf", To: "eps", Engine: Poppler, Cost: 1.2, Options: []string{options.Pages}},
		Edge{From: "pdf", To: "ps", Engine: Poppler, Cost: 1.2},
	)

	// ── rsvg ──
	add(
		Edge{From: "svg", To: "png", Engine: RSVG, Cost: 1, Options: []string{options.DPI}},
		Edge{From: "svg", To: "pdf", Engine: RSVG, Cost: 1},
		Edge{From: "svg", To: "ps", Engine: RSVG, Cost: 1},
		Edge{From: "svg", To: "eps", Engine: RSVG, Cost: 1},
	)

	return es
}
