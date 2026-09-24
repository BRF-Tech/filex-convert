package graph

import (
	"errors"
	"testing"

	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/options"
)

func TestEdgesConsistent(t *testing.T) {
	engines := map[string]bool{}
	for _, e := range Engines {
		engines[e] = true
	}
	seen := map[string]bool{}
	for _, e := range Edges() {
		if _, ok := formats.ByID(e.From); !ok {
			t.Errorf("edge %s: unknown from", e.Key())
		}
		if _, ok := formats.ByID(e.To); !ok {
			t.Errorf("edge %s: unknown to", e.Key())
		}
		if e.Engine != PureGo && !engines[e.Engine] {
			t.Errorf("edge %s: unknown engine", e.Key())
		}
		if e.Cost <= 0 {
			t.Errorf("edge %s: cost %v", e.Key(), e.Cost)
		}
		if seen[e.Key()] {
			t.Errorf("duplicate edge %s", e.Key())
		}
		seen[e.Key()] = true
		for _, k := range e.Options {
			if _, ok := options.Defs[k]; !ok {
				t.Errorf("edge %s: unknown option %q", e.Key(), k)
			}
		}
		if e.From == e.To && e.Engine == PureGo {
			t.Errorf("edge %s: pure-Go self edge makes no sense", e.Key())
		}
	}
	if n := len(Edges()); n < 120 {
		t.Errorf("only %d edges, expected at least 120", n)
	}
}

func TestEveryFormatHasAnEdge(t *testing.T) {
	touched := map[string]bool{}
	for _, e := range Edges() {
		touched[e.From] = true
		touched[e.To] = true
	}
	for _, id := range formats.IDs() {
		if !touched[id] {
			t.Errorf("format %s appears in no edge", id)
		}
	}
}

func TestDirectRoute(t *testing.T) {
	r, err := Route("png", "jpg", nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 || r[0].Engine != PureGo || !r[0].Lossy {
		t.Fatalf("png→jpg = %v", r)
	}
	if got := r.Cost(); got != LossyFactor {
		t.Errorf("cost %v, want %v", got, LossyFactor)
	}
	if opts := r.Options(); len(opts) != 1 || opts[0] != options.Quality {
		t.Errorf("options %v", opts)
	}
}

func TestPreferPureGoOverEngine(t *testing.T) {
	r, err := Route("png", "tiff", AllEngines())
	if err != nil {
		t.Fatal(err)
	}
	if r[0].Engine != PureGo {
		t.Errorf("png→tiff should prefer pure Go, got %s", r)
	}
	r, err = Route("svg", "png", AllEngines())
	if err != nil {
		t.Fatal(err)
	}
	if r[0].Engine != RSVG {
		t.Errorf("svg→png should prefer rsvg, got %s", r)
	}
	r, err = Route("pdf", "png", AllEngines())
	if err != nil {
		t.Fatal(err)
	}
	if r[0].Engine != Poppler {
		t.Errorf("pdf→png should prefer poppler, got %s", r)
	}
}

func TestTwoHopRoute(t *testing.T) {
	// heic → png needs ImageMagick; then png → bmp is pure Go. No direct edge
	// heic → gif exists, so heic → gif goes heic → png → gif (2 hops).
	r, err := Route("heic", "qoi", AllEngines())
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || r[0].Engine != ImageMagick || r[1].Engine != PureGo {
		t.Errorf("heic→qoi = %s", r)
	}
	// docx → png: docx → pdf (soffice) → png (poppler)
	r, err = Route("docx", "png", AllEngines())
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || r[0].To != "pdf" || r[1].Engine != Poppler {
		t.Errorf("docx→png = %s", r)
	}
}

func TestThreeHopRoute(t *testing.T) {
	// md → pdf (go) → png (poppler): the pure-Go PDF writer makes it two steps.
	r, err := Route("md", "png", AllEngines())
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || r[0].To != "pdf" || r[0].Engine != PureGo || r[1].To != "png" {
		t.Errorf("md→png = %s", r)
	}
	// md → ico: md → pdf → png → ico is three steps and allowed.
	r, err = Route("md", "ico", AllEngines())
	if err != nil || len(r) != 3 {
		t.Errorf("md→ico = %s %v", r, err)
	}
	// heic → mp4 would be heic → png → mp4 (still video): the image → video
	// edge is Initial, so it is refused, as is any four-step chain.
	if _, err := Route("heic", "mp4", AllEngines()); !errors.Is(err, ErrNoRoute) {
		t.Errorf("heic→mp4 should be refused, got %v", err)
	}
}

func TestLossyCostSteersRoute(t *testing.T) {
	// heic → tiff: heic → png → tiff (both lossless, 2) beats heic → jpg → tiff (1.4+1).
	r, err := Route("heic", "tiff", map[string]bool{ImageMagick: true})
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 1 || r[0].Engine != ImageMagick {
		t.Errorf("heic→tiff = %s (direct ImageMagick edge expected)", r)
	}
	// xcf → qoi has no direct edge: through png (lossless) rather than jpg.
	r, err = Route("xcf", "qoi", AllEngines())
	if err != nil {
		t.Fatal(err)
	}
	if len(r) != 2 || r[0].To == "jpg" || r[0].To == "gif" {
		t.Errorf("xcf→qoi = %s (should pass through a lossless format)", r)
	}
}

func TestExcludedEdge(t *testing.T) {
	r, _ := Route("png", "jpg", AllEngines())
	r2, err := RouteExcluding("png", "jpg", AllEngines(), r)
	if err != nil {
		t.Fatal(err)
	}
	if len(r2) != 1 || r2[0].Engine != ImageMagick {
		t.Errorf("after excluding the pure edge, png→jpg = %s", r2)
	}
	// With no ImageMagick either, the planner detours through another
	// pure-Go format rather than reuse the excluded edge.
	r3, err := RouteExcluding("png", "jpg", nil, r)
	if err != nil {
		t.Fatal(err)
	}
	if len(r3) != 2 || r3[0].To == "jpg" {
		t.Errorf("detour = %s", r3)
	}
	for _, e := range r3 {
		if e.Key() == r[0].Key() {
			t.Errorf("excluded edge reused: %s", r3)
		}
	}
}

func TestTerminalEdges(t *testing.T) {
	// A video is offered as GIF and as frames, but never as, say, an ICO
	// "through" the GIF or through a frame.
	if _, err := Route("mp4", "gif", AllEngines()); err != nil {
		t.Errorf("mp4→gif: %v", err)
	}
	r, err := Route("mp4", "png", AllEngines())
	if err != nil || len(r) != 1 || !r[0].Multi {
		t.Errorf("mp4→png should be the direct frames edge, got %s %v", r, err)
	}
	if _, err := Route("mp4", "ico", AllEngines()); !errors.Is(err, ErrNoRoute) {
		t.Errorf("mp4→ico must not route through a frame, got %v", err)
	}
	if _, err := Route("pdf", "docx", AllEngines()); !errors.Is(err, ErrNoRoute) {
		t.Errorf("pdf→docx must not route through txt, got %v", err)
	}
	// the ASCII art of a PNG is offered, but nothing is built on top of it
	if _, err := Route("png", "md", AllEngines()); !errors.Is(err, ErrNoRoute) {
		t.Errorf("png→md must not route through the ASCII text, got %v", err)
	}
}

func TestInitialEdges(t *testing.T) {
	// An animated GIF becomes a video; a PNG becomes a still video directly,
	// but a HEIC does not (heic → png → mp4 would start the still edge late).
	if _, err := Route("gif", "mp4", AllEngines()); err != nil {
		t.Errorf("gif→mp4: %v", err)
	}
	r, err := Route("png", "mp4", AllEngines())
	if err != nil || len(r) != 1 || !r[0].Initial {
		t.Errorf("png→mp4 = %s %v", r, err)
	}
	if _, err := Route("heic", "mp4", AllEngines()); !errors.Is(err, ErrNoRoute) {
		t.Errorf("heic→mp4 must not chain into the still-video edge, got %v", err)
	}
	// The text of a PDF is offered; the text of a DOCX comes from the DOCX
	// itself, never from docx → pdf → txt.
	r, err = Route("docx", "txt", nil)
	if err != nil || len(r) != 1 || r[0].Engine != PureGo {
		t.Errorf("docx→txt = %s %v", r, err)
	}
	// packing is only ever the first and last step
	if _, err := Route("docx", "zip", nil); err != nil {
		t.Errorf("docx→zip: %v", err)
	}
	r, err = Route("heic", "zip", nil)
	if err != nil || len(r) != 1 || !IsPack(r[0]) {
		t.Errorf("heic→zip should be the direct pack edge, got %s %v", r, err)
	}
}

func TestNoRouteAndMissingEngines(t *testing.T) {
	_, err := Route("heic", "png", nil)
	var nre *NoRouteError
	if !errors.As(err, &nre) {
		t.Fatalf("want NoRouteError, got %v", err)
	}
	if len(nre.MissingEngines) != 1 || nre.MissingEngines[0] != ImageMagick {
		t.Errorf("missing engines = %v", nre.MissingEngines)
	}
	// docx → pdf works without LibreOffice (pure Go) but prefers it
	r, err := Route("docx", "pdf", nil)
	if err != nil || len(r) != 1 || r[0].Engine != PureGo {
		t.Errorf("docx→pdf without engines = %s %v", r, err)
	}
	r, err = Route("docx", "pdf", AllEngines())
	if err != nil || len(r) != 1 || r[0].Engine != LibreOffice {
		t.Errorf("docx→pdf with engines = %s %v", r, err)
	}
	if _, err := Route("mp3", "docx", AllEngines()); !errors.Is(err, ErrNoRoute) {
		t.Errorf("mp3→docx should have no route, got %v", err)
	}
	if _, err := Route("nope", "pdf", nil); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("unknown format: %v", err)
	}
	if _, err := Route("pdf", "nope", nil); !errors.Is(err, ErrUnknownFormat) {
		t.Errorf("unknown format: %v", err)
	}
}

func TestSelfEdge(t *testing.T) {
	r, err := Route("pdf", "pdf", AllEngines())
	if err != nil || len(r) != 1 || r[0].Engine != Ghostscript {
		t.Fatalf("pdf→pdf = %v %v", r, err)
	}
	if r.Variant() != "pdf" {
		t.Errorf("variant %q", r.Variant())
	}
	if _, err := Route("png", "png", AllEngines()); !errors.Is(err, ErrNoRoute) {
		t.Errorf("png→png should have no route, got %v", err)
	}
}

func TestTargetsIntersection(t *testing.T) {
	ts := Targets([]string{"png", "jpg"}, nil)
	has := map[string]bool{}
	for _, id := range ts {
		has[id] = true
	}
	for _, want := range []string{"gif", "bmp", "tiff", "webp", "qoi", "pdf", "svg", "txt", "zip", "txz"} {
		if !has[want] {
			t.Errorf("png+jpg should reach %s: %v", want, ts)
		}
	}
	// png itself is a target only for jpg (and vice versa): not in the intersection.
	if has["png"] || has["jpg"] {
		t.Errorf("png or jpg in the intersection: %v", ts)
	}
	for _, id := range ts {
		if formats.CategoryOf(id) == formats.Video || formats.CategoryOf(id) == formats.Audio || id == "avif" || id == "docx" {
			t.Errorf("unexpected pure-Go target %s for png+jpg", id)
		}
	}
	if len(Targets(nil, nil)) != 0 {
		t.Error("no sources → no targets")
	}
	// mp3 + docx share only the archives without engines
	ts = Targets([]string{"mp3", "docx"}, nil)
	for _, id := range ts {
		if formats.CategoryOf(id) != formats.Archive {
			t.Errorf("mp3+docx should share only archives without engines, got %v", ts)
		}
	}
	ts = Targets([]string{"docx", "xlsx"}, AllEngines())
	found := false
	for _, id := range ts {
		if id == "pdf" {
			found = true
		}
	}
	if !found {
		t.Errorf("docx+xlsx should both reach pdf: %v", ts)
	}
}

func TestPackEdgesAndImages(t *testing.T) {
	n := 0
	for _, e := range Edges() {
		if IsPack(e) {
			n++
			if !e.Initial || !e.Terminal || e.Engine != PureGo {
				t.Errorf("pack edge %s must be pure Go, Initial and Terminal", e.Key())
			}
		}
	}
	if n < 250 {
		t.Errorf("only %d pack edges", n)
	}
	for _, id := range GoImages {
		if formats.CategoryOf(id) != formats.Image {
			t.Errorf("%s is not an image", id)
		}
	}
}

func TestRouteString(t *testing.T) {
	r, _ := Route("docx", "png", AllEngines())
	if s := r.String(); s != "docx → pdf (libreoffice) → png (poppler)" {
		t.Errorf("String() = %q", s)
	}
}
