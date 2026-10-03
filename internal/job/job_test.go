package job

import (
	"archive/zip"
	"bytes"
	"errors"
	"image"
	"image/color"
	"image/png"
	"io"
	"strconv"
	"strings"
	"testing"

	"github.com/brf-tech/filex/backend/pkg/pluginkit"
	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"

	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/i18n"
	"github.com/brf-tech/filex-convert/internal/purego"
)

// fakeHost is an in-memory Host: inputs are refs with bytes, outputs get
// out:N refs, engines are scripted per call.
type fakeHost struct {
	files    map[string][]byte
	sizes    map[string]int64
	nextOut  int
	nextEng  int
	engines  []string // engines invoked, in order
	requests []pluginkit.EngineRequest
	engineFn func(n int, req pluginkit.EngineRequest) (*pluginkit.EngineResult, error)
	progress []string
	logs     []string
}

func newFake() *fakeHost {
	return &fakeHost{files: map[string][]byte{}, sizes: map[string]int64{}}
}

func (f *fakeHost) input(ref, data string) { f.files[ref] = []byte(data) }

func (f *fakeHost) ReadInput(ref string) ([]byte, error) {
	b, ok := f.files[ref]
	if !ok {
		return nil, &pluginkit.HostError{Code: wire.ErrNotFound, Message: "no such ref " + ref}
	}
	return b, nil
}

func (f *fakeHost) InputSize(ref string) int64 {
	if s, ok := f.sizes[ref]; ok {
		return s
	}
	if b, ok := f.files[ref]; ok {
		return int64(len(b))
	}
	return -1
}

func (f *fakeHost) WriteOutput(name string, data []byte) (wire.OutputRef, error) {
	ref := "out:" + strconv.Itoa(f.nextOut)
	f.nextOut++
	f.files[ref] = data
	return wire.OutputRef{Ref: ref, Name: name}, nil
}

// artefact registers an engine-produced file and answers its ref.
func (f *fakeHost) artefact(name string, data []byte) wire.OutputRef {
	ref := "eng:" + strconv.Itoa(f.nextEng)
	f.nextEng++
	f.files[ref] = data
	return wire.OutputRef{Ref: ref, Name: name}
}

func (f *fakeHost) EngineRun(req pluginkit.EngineRequest) (*pluginkit.EngineResult, error) {
	n := len(f.engines)
	f.engines = append(f.engines, req.Engine)
	f.requests = append(f.requests, req)
	for name, ref := range req.Inputs {
		if _, ok := f.files[ref]; !ok {
			return nil, &pluginkit.HostError{Code: wire.ErrNotFound, Message: "input " + name + " ref " + ref}
		}
	}
	if f.engineFn != nil {
		return f.engineFn(n, req)
	}
	// default: produce what was asked for, or one page file
	res := &pluginkit.EngineResult{}
	if len(req.Outputs) > 0 {
		for _, o := range req.Outputs {
			res.Outputs = append(res.Outputs, f.artefact(o, []byte("engine:"+o)))
		}
	} else {
		res.Outputs = append(res.Outputs, f.artefact("out-1.png", pngFixture()))
	}
	return res, nil
}

func (f *fakeHost) Progress(done, total int64, message string) {
	f.progress = append(f.progress, strconv.FormatInt(done, 10)+"/"+strconv.FormatInt(total, 10)+" "+message)
}

func (f *fakeHost) Log(level, msg string) { f.logs = append(f.logs, level+": "+msg) }

func pngFixture() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i < 16; i++ {
		img.Set(i%4, i/4, color.NRGBA{R: uint8(i * 16), G: 20, B: 200, A: 255})
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func run(t *testing.T, h *fakeHost, target string, engines map[string]bool, params map[string]any, names ...string) *wire.ActionRunOutput {
	t.Helper()
	in := &wire.ActionRunInput{JobID: "j1", ActionID: "convert", Params: map[string]any{ParamTarget: target}, Engines: engines}
	for k, v := range params {
		in.Params[k] = v
	}
	for i, n := range names {
		ref := "in:" + strconv.Itoa(i)
		in.Inputs = append(in.Inputs, wire.FileRef{Ref: ref, Name: n, Size: int64(len(h.files[ref]))})
	}
	out, err := Run(h, in)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func TestPureSingle(t *testing.T) {
	h := newFake()
	h.files["in:0"] = pngFixture()
	out := run(t, h, "jpg", nil, nil, "photo.PNG")
	if !out.OK || len(out.Outputs) != 1 || out.Outputs[0].Name != "photo.jpg" {
		t.Fatalf("out %+v", out)
	}
	if !bytes.HasPrefix(h.files[out.Outputs[0].Ref], []byte{0xff, 0xd8}) {
		t.Error("output is not a JPEG")
	}
	if out.Message["en"] != "converted to JPEG" || out.Message["tr"] != "JPEG biçimine dönüştürüldü" {
		t.Errorf("message %v", out.Message)
	}
	if len(h.engines) != 0 {
		t.Error("no engine should run")
	}
	if len(h.progress) != 2 || !strings.Contains(h.progress[0], "photo.PNG → JPEG") {
		t.Errorf("progress %v", h.progress)
	}
}

func TestPartialFailure(t *testing.T) {
	h := newFake()
	h.files["in:0"] = pngFixture()
	h.input("in:1", "not an archive")
	h.files["in:2"] = pngFixture()
	out := run(t, h, "bmp", nil, nil, "a.png", "b.xyz", "c.png")
	if !out.OK || len(out.Outputs) != 2 {
		t.Fatalf("out %+v", out)
	}
	if out.Outputs[0].Name != "a.bmp" || out.Outputs[1].Name != "c.bmp" {
		t.Errorf("names %v", out.Outputs)
	}
	en, tr := out.Message["en"], out.Message["tr"]
	if !strings.HasPrefix(en, "2 of 3 converted to BMP - failed: b.xyz: unknown file type") {
		t.Errorf("en message %q", en)
	}
	if !strings.Contains(tr, "2 / 3 dosya BMP biçimine dönüştürüldü - başarısız: b.xyz: bilinmeyen dosya türü") {
		t.Errorf("tr message %q", tr)
	}
}

func TestAllFail(t *testing.T) {
	h := newFake()
	h.input("in:0", "garbage")
	out := run(t, h, "jpg", nil, nil, "broken.png")
	if out.OK || len(out.Outputs) != 0 {
		t.Fatalf("out %+v", out)
	}
	if !strings.HasPrefix(out.Message["en"], "nothing converted - broken.png: conversion failed") {
		t.Errorf("message %v", out.Message)
	}
	if !strings.HasPrefix(out.Message["tr"], "hiçbir dosya dönüştürülemedi - broken.png: dönüşüm başarısız oldu") {
		t.Errorf("message %v", out.Message)
	}
}

func TestEngineStep(t *testing.T) {
	h := newFake()
	h.input("in:0", "docx bytes")
	out := run(t, h, "pdf", map[string]bool{graph.Office: true}, nil, "Quarterly Report.docx")
	if !out.OK || len(out.Outputs) != 1 {
		t.Fatalf("out %+v", out)
	}
	if out.Outputs[0].Name != "Quarterly Report.pdf" {
		t.Errorf("name %q", out.Outputs[0].Name)
	}
	if len(h.requests) != 1 || h.requests[0].Engine != graph.Office {
		t.Fatalf("requests %+v", h.requests)
	}
	req := h.requests[0]
	if req.Inputs["in.docx"] != "in:0" || req.Outputs[0] != "in.pdf" || req.TimeoutS == 0 {
		t.Errorf("request %+v", req)
	}
	if string(h.files[out.Outputs[0].Ref]) != "engine:in.pdf" {
		t.Error("output ref does not point at the artefact")
	}
}

func TestEngineMissing(t *testing.T) {
	h := newFake()
	h.input("in:0", "heic bytes")
	out := run(t, h, "png", map[string]bool{graph.ImageMagick: false}, nil, "r.heic")
	if out.OK {
		t.Fatal("should fail")
	}
	if !strings.Contains(out.Message["en"], "needs an engine this server does not have (ImageMagick)") {
		t.Errorf("en %q", out.Message["en"])
	}
	if !strings.Contains(out.Message["tr"], "bu sunucuda olmayan bir motor gerekiyor (ImageMagick)") {
		t.Errorf("tr %q", out.Message["tr"])
	}
	if len(h.engines) != 0 {
		t.Error("no engine call expected")
	}
}

func TestEngineUnavailableAtRuntime(t *testing.T) {
	h := newFake()
	h.input("in:0", "svg")
	h.engineFn = func(int, pluginkit.EngineRequest) (*pluginkit.EngineResult, error) {
		return nil, &pluginkit.HostError{Code: wire.ErrUnavailable, Message: "rsvg is not installed"}
	}
	out := run(t, h, "pdf", map[string]bool{graph.RSVG: true, graph.ImageMagick: true}, nil, "logo.svg")
	if out.OK || !strings.Contains(out.Message["en"], "needs an engine") {
		t.Errorf("out %+v", out)
	}
}

func TestRetryOnAlternateEdge(t *testing.T) {
	h := newFake()
	h.input("in:0", "%PDF-1.4")
	h.engineFn = func(n int, req pluginkit.EngineRequest) (*pluginkit.EngineResult, error) {
		if req.Engine == graph.Poppler {
			return &pluginkit.EngineResult{Exit: 1, StderrTail: "Syntax Error: bad xref\nCommand Line Error: x"}, nil
		}
		return &pluginkit.EngineResult{Outputs: []wire.OutputRef{
			h.artefact("out-2.png", []byte("p2")),
			h.artefact("out-1.png", []byte("p1")),
			h.artefact("out-10.png", []byte("p10")),
		}}, nil
	}
	out := run(t, h, "png", graph.AllEngines(), map[string]any{"pages": "1-10"}, "doc.pdf")
	if !out.OK {
		t.Fatalf("out %+v", out)
	}
	if strings.Join(h.engines, ",") != "poppler,ghostscript" {
		t.Errorf("engines %v", h.engines)
	}
	names := []string{}
	for _, o := range out.Outputs {
		names = append(names, o.Name)
	}
	if strings.Join(names, ",") != "doc-1.png,doc-2.png,doc-10.png" {
		t.Errorf("names %v", names)
	}
	found := false
	for _, l := range h.logs {
		if strings.Contains(l, "retrying via pdf → png (ghostscript)") {
			found = true
		}
	}
	if !found {
		t.Errorf("logs %v", h.logs)
	}
}

func TestRetryExhausted(t *testing.T) {
	h := newFake()
	h.input("in:0", "%PDF-1.4")
	h.engineFn = func(int, pluginkit.EngineRequest) (*pluginkit.EngineResult, error) {
		return &pluginkit.EngineResult{Exit: 2, StderrTail: "boom"}, nil
	}
	out := run(t, h, "png", graph.AllEngines(), nil, "doc.pdf")
	if out.OK {
		t.Fatal("should fail")
	}
	if len(h.engines) != 2 {
		t.Errorf("exactly one retry expected, got %v", h.engines)
	}
	// The person reads which engine failed, by its name; the exit code and
	// the engine's own words are the log's (Error.Text says why).
	if !strings.Contains(out.Message["en"], "the converter failed (Poppler)") || strings.Contains(out.Message["en"], "boom") {
		t.Errorf("en %q", out.Message["en"])
	}
	if !strings.Contains(out.Message["tr"], "dönüştürücü başarısız oldu (Poppler)") {
		t.Errorf("tr %q", out.Message["tr"])
	}
	logged := false
	for _, l := range h.logs {
		logged = logged || strings.Contains(l, "poppler exit 2: boom")
	}
	if !logged {
		t.Errorf("the engine's own words belong in the log: %v", h.logs)
	}
}

func TestSameFormatNaming(t *testing.T) {
	h := newFake()
	h.input("in:0", "%PDF-1.4")
	out := run(t, h, "pdf", graph.AllEngines(), nil, "big.pdf")
	if !out.OK || out.Outputs[0].Name != "big.compressed.pdf" {
		t.Fatalf("out %+v", out)
	}
	h = newFake()
	h.input("in:0", "%PDF-1.4")
	out = run(t, h, "pdf", graph.AllEngines(), map[string]any{"pdfa": true}, "big.pdf")
	if !out.OK || out.Outputs[0].Name != "big.pdfa.pdf" {
		t.Fatalf("out %+v", out)
	}
	args := strings.Join(h.requests[0].Args, " ")
	if !strings.Contains(args, "-dPDFA=2") {
		t.Errorf("pdfa not passed: %s", args)
	}
}

func TestMultiHopEngineThenPure(t *testing.T) {
	h := newFake()
	h.input("in:0", "heic bytes")
	h.engineFn = func(int, pluginkit.EngineRequest) (*pluginkit.EngineResult, error) {
		return &pluginkit.EngineResult{Outputs: []wire.OutputRef{h.artefact("out.png", pngFixture())}}, nil
	}
	out := run(t, h, "qoi", graph.AllEngines(), nil, "IMG_0001.heic")
	if !out.OK || len(out.Outputs) != 1 || out.Outputs[0].Name != "IMG_0001.qoi" {
		t.Fatalf("out %+v", out)
	}
	if !bytes.HasPrefix(h.files[out.Outputs[0].Ref], []byte("qoif")) {
		t.Error("final output is not a QOI")
	}
	if len(h.engines) != 1 || h.engines[0] != graph.ImageMagick {
		t.Errorf("engines %v", h.engines)
	}
}

func TestPureThenEngine(t *testing.T) {
	// json → csv (pure) → ods (office engine): the pure output ref is the engine input.
	h := newFake()
	h.input("in:0", `[{"a":"1","b":"2"}]`)
	out := run(t, h, "ods", graph.AllEngines(), nil, "rows.json")
	if !out.OK || out.Outputs[0].Name != "rows.ods" {
		t.Fatalf("out %+v", out)
	}
	req := h.requests[0]
	ref := req.Inputs["in.xlsx"]
	if !strings.HasPrefix(ref, "out:") {
		t.Errorf("engine input should be the pure step's output ref, got %q (%v)", ref, req.Inputs)
	}
	if !bytes.HasPrefix(h.files[ref], []byte("PK")) {
		t.Error("intermediate xlsx missing")
	}
}

func TestBadOptions(t *testing.T) {
	h := newFake()
	h.files["in:0"] = pngFixture()
	out := run(t, h, "jpg", nil, map[string]any{"quality": 500}, "a.png")
	if out.OK || !strings.Contains(out.Message["en"], "an option is out of range") {
		t.Errorf("out %+v", out)
	}
	if !strings.Contains(out.Message["tr"], "bir seçenek aralık dışında") {
		t.Errorf("tr %q", out.Message["tr"])
	}
}

func TestTooLarge(t *testing.T) {
	h := newFake()
	h.files["in:0"] = pngFixture()
	h.sizes["in:0"] = purego.MaxInput + 1
	out := run(t, h, "jpg", nil, nil, "huge.png")
	if out.OK || !strings.Contains(out.Message["tr"], "dosya bu dönüşüm için çok büyük") {
		t.Errorf("out %+v", out)
	}
}

func TestNoTargetOrInputs(t *testing.T) {
	h := newFake()
	out, err := Run(h, &wire.ActionRunInput{Params: map[string]any{}})
	if err != nil || out.OK || out.Message["tr"] != "hedef biçim seçilmedi" {
		t.Errorf("out %+v err %v", out, err)
	}
	out, _ = Run(h, &wire.ActionRunInput{Params: map[string]any{ParamTarget: "pdf"}})
	if out.OK || out.Message["en"] != "nothing to convert" {
		t.Errorf("out %+v", out)
	}
}

func TestReadFailure(t *testing.T) {
	h := newFake()
	out := run(t, h, "jpg", nil, nil, "ghost.png")
	if out.OK || !strings.Contains(out.Message["en"], "could not read the file") {
		t.Errorf("out %+v", out)
	}
}

func TestErrorTextAndUnwrap(t *testing.T) {
	e := &Error{Code: CodeEngineFailed, File: "x.avi", Detail: strings.Repeat("y", 200), Engines: []string{graph.FFmpeg}}
	if got := e.Text()["en"]; got != "x.avi: the converter failed (FFmpeg)" {
		t.Errorf("a person reads the reason and the engine's name, not the detail: %q", got)
	}
	if got := e.Text()["tr"]; got != "x.avi: dönüştürücü başarısız oldu (FFmpeg)" {
		t.Errorf("tr %q", got)
	}
	if e.Error() != "engine_failed: "+strings.Repeat("y", 200) {
		t.Errorf("Error() %q", e.Error())
	}
	var target *Error
	if !errors.As(error(e), &target) {
		t.Error("errors.As")
	}
	if (&Error{Code: CodeNoRoute}).Error() != "no_route" {
		t.Error("bare code")
	}
}

// filex 0.50: the office engine is the ONLYOFFICE connected to filex, so a
// job that needed it and found none says what is missing - a server, not a
// program - in every language, and never names LibreOffice.
//
// Red before 0.2.0: "needs an engine this server does not have (LibreOffice)".
func TestErrorText_TheOfficeEngineIsNotConnected(t *testing.T) {
	e := &Error{Code: CodeEngineMissing, File: "teklif.docx", Detail: "office: engine office is not configured on this host", Engines: []string{graph.Office}}
	text := e.Text()
	if got := text["en"]; got != "teklif.docx: office documents need ONLYOFFICE, which is not connected to this server" {
		t.Errorf("en %q", got)
	}
	if got := text["tr"]; got != "teklif.docx: ofis belgeleri için ONLYOFFICE gerekiyor ve bu sunucuya bağlı değil" {
		t.Errorf("tr %q", got)
	}
	for _, lang := range i18n.Langs {
		if !strings.Contains(text[lang], "ONLYOFFICE") || strings.Contains(text[lang], "LibreOffice") {
			t.Errorf("%s: %q", lang, text[lang])
		}
	}
	// Another engine missing beside it keeps the generic sentence.
	both := &Error{Code: CodeEngineMissing, Engines: []string{graph.FFmpeg, graph.Office}}
	if got := both.Text()["en"]; !strings.Contains(got, "FFmpeg, ONLYOFFICE") {
		t.Errorf("both %q", got)
	}
}

func TestPageNo(t *testing.T) {
	for name, want := range map[string]int{"out-1.png": 1, "out-007.jpg": 7, "out.png": 0, "x-12-3.png": 3} {
		if got := pageNo(name); got != want {
			t.Errorf("pageNo(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestMergeImagesToPDF(t *testing.T) {
	h := newFake()
	h.files["in:0"] = pngFixture()
	h.files["in:1"] = pngFixture()
	out := run(t, h, "pdf", nil, map[string]any{"merge": true}, "a.png", "b.png")
	if !out.OK || len(out.Outputs) != 1 || out.Outputs[0].Name != "a.pdf" {
		t.Fatalf("out %+v", out)
	}
	pdf := h.files[out.Outputs[0].Ref]
	if !bytes.HasPrefix(pdf, []byte("%PDF-")) || !bytes.Contains(pdf, []byte("/Count 2")) {
		t.Error("merged PDF should have two pages")
	}
	if out.Message["en"] != "2 files combined into one PDF" {
		t.Errorf("message %v", out.Message)
	}
}

func TestMergeFilesToZip(t *testing.T) {
	h := newFake()
	h.input("in:0", "hello")
	h.input("in:1", "# md")
	out := run(t, h, "zip", nil, map[string]any{"merge": true}, "notes.txt", "readme.md")
	if !out.OK || len(out.Outputs) != 1 || out.Outputs[0].Name != "notes.zip" {
		t.Fatalf("out %+v", out)
	}
	z := h.files[out.Outputs[0].Ref]
	if !bytes.Contains(z, []byte("notes.txt")) || !bytes.Contains(z, []byte("readme.md")) {
		t.Error("zip should hold both members")
	}
}

func TestMergeImagesToGIFViaFFmpeg(t *testing.T) {
	h := newFake()
	h.files["in:0"] = pngFixture()
	h.files["in:1"] = pngFixture()
	h.engineFn = func(_ int, req pluginkit.EngineRequest) (*pluginkit.EngineResult, error) {
		if len(req.Inputs) != 2 || req.Inputs["frame-001.png"] == "" || req.Inputs["frame-002.png"] == "" {
			t.Errorf("frames not handed over: %v", req.Inputs)
		}
		if !strings.Contains(strings.Join(req.Args, " "), "-framerate 0.200000 -i frame-%03d.png") {
			t.Errorf("args %v", req.Args)
		}
		return &pluginkit.EngineResult{Outputs: []wire.OutputRef{h.artefact("out.gif", []byte("GIF89a"))}}, nil
	}
	out := run(t, h, "gif", map[string]bool{graph.FFmpeg: true}, map[string]any{"merge": true}, "a.png", "b.png")
	if !out.OK || len(out.Outputs) != 1 || out.Outputs[0].Name != "a.gif" {
		t.Fatalf("out %+v", out)
	}
	out = run(t, h, "gif", nil, map[string]any{"merge": true}, "a.png", "b.png")
	if out.OK || !strings.Contains(out.Message["en"], "needs an engine") {
		t.Errorf("without ffmpeg: %+v", out)
	}
}

func TestPackNamesMemberAfterSource(t *testing.T) {
	h := newFake()
	h.input("in:0", "hello")
	out := run(t, h, "tar", nil, nil, "Notlar 2026.txt")
	if !out.OK || out.Outputs[0].Name != "Notlar 2026.tar" {
		t.Fatalf("out %+v", out)
	}
	if !bytes.Contains(h.files[out.Outputs[0].Ref], []byte("Notlar 2026.txt")) {
		t.Error("tar member should carry the source name")
	}
}

// The job's own words name the format in the reader's language: a label
// that DESCRIBES a format ("Plain text (.txt)") is English, and it used to
// be pasted into the Turkish message and the Turkish tray line as is.
func TestTheJobNamesTheFormatInEachLanguage(t *testing.T) {
	msg := summary(&Result{Done: 1}, "txt", 1)
	if msg["en"] != "converted to Plain text (.txt)" {
		t.Errorf("en %q", msg["en"])
	}
	if msg["tr"] != "Düz metin (.txt) biçimine dönüştürüldü" {
		t.Errorf("tr %q", msg["tr"])
	}
	if got := mergedSummary(2, "ico"); got["tr"] != "2 dosya tek Simge (.ico) dosyasında birleştirildi" {
		t.Errorf("merged tr %q", got["tr"])
	}
	// The tray line is ONE string, so it names the format by a name that
	// reads the same for everybody (TestTheTrayLineReadsTheSameInEveryLanguage).
	if got := progressLine("a.md", "txt"); got != "a.md → TXT" {
		t.Errorf("tray %q", got)
	}
}

// Spanish, German and French too (owner's decision, v0.43.0): the summary,
// a partial failure with its reasons and the merge line are
// each written in the reader's language, the format named in it.
//
// ⚠ The host still collapses every locale but Turkish to `en` before it
// calls an app (filex feat/043-srvtext); these build the messages directly.
func TestTheJobSpeaksSpanishGermanAndFrench(t *testing.T) {
	done := summary(&Result{Done: 1}, "txt", 1)
	partial := summary(&Result{Done: 1, Failed: 1, Errors: []*Error{{Code: CodeEngineMissing, Detail: "imagemagick", File: "a.heic", Engines: []string{graph.ImageMagick}}}}, "png", 2)
	merged := mergedSummary(3, "ico")
	for lang, want := range map[string][3]string{
		"es": {"convertido a Texto sin formato (.txt)",
			"1 de 2 convertidos a PNG - con errores: a.heic: requiere un motor que este servidor no tiene (ImageMagick)",
			"3 archivos combinados en un solo Icono (.ico)"},
		"de": {"in Reiner Text (.txt) konvertiert",
			"1 von 2 in PNG konvertiert - fehlgeschlagen: a.heic: benötigt eine Engine, die auf diesem Server fehlt (ImageMagick)",
			"3 Dateien zu einer Datei im Format Symbol (.ico) zusammengeführt"},
		"fr": {"converti en Texte brut (.txt)",
			"1 sur 2 convertis en PNG - échecs : a.heic : nécessite un moteur absent de ce serveur (ImageMagick)",
			"3 fichiers combinés en un seul fichier Icône (.ico)"},
	} {
		if done[lang] != want[0] {
			t.Errorf("%s summary %q, want %q", lang, done[lang], want[0])
		}
		if partial[lang] != want[1] {
			t.Errorf("%s partial %q, want %q", lang, partial[lang], want[1])
		}
		if merged[lang] != want[2] {
			t.Errorf("%s merged %q, want %q", lang, merged[lang], want[2])
		}
	}
}

// The job hands the reader's locale to the converter: a document that
// declares no language is written in the language of the person who ran
// the conversion (v0.43.0; before, every EPUB said Turkish).
//
// ⚠ Until filex feat/043-srvtext merges, the host sends `en` for every
// locale but Turkish, so a German user's undeclared text is written `en`.
func TestTheJobPassesItsLocaleToTheDocument(t *testing.T) {
	for locale, want := range map[string]string{"de": "<dc:language>de</dc:language>", "tr": "<dc:language>tr</dc:language>", "": "<dc:language>und</dc:language>"} {
		h := newFake()
		h.files["in:0"] = []byte("Some text.")
		in := &wire.ActionRunInput{JobID: "j1", ActionID: "convert", Locale: locale, Params: map[string]any{ParamTarget: "epub"},
			Inputs: []wire.FileRef{{Ref: "in:0", Name: "notes.txt", Size: 10}}}
		out, err := Run(h, in)
		if err != nil || !out.OK || len(out.Outputs) != 1 {
			t.Fatalf("locale %q: %v %+v", locale, err, out)
		}
		epub := h.files[out.Outputs[0].Ref]
		zr, err := zip.NewReader(bytes.NewReader(epub), int64(len(epub)))
		if err != nil {
			t.Fatal(err)
		}
		found := false
		for _, f := range zr.File {
			if f.Name == "OEBPS/content.opf" {
				rc, _ := f.Open()
				opf, _ := io.ReadAll(rc)
				rc.Close()
				found = true
				if !bytes.Contains(opf, []byte(want)) {
					t.Errorf("locale %q: OPF lacks %s:\n%s", locale, want, opf)
				}
			}
		}
		if !found {
			t.Fatalf("locale %q: no OPF", locale)
		}
	}
}
