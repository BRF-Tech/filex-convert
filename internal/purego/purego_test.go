package purego

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"io"
	"strings"
	"testing"

	"github.com/klauspost/compress/zstd"
	"gopkg.in/yaml.v3"

	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/options"
)

// ── registry ↔ graph ──

func TestRegistryMatchesGraph(t *testing.T) {
	// Every pure-Go edge in the graph has a converter, and every converter
	// is reachable through an edge (no dead code, no unroutable promise).
	edges := map[string]bool{}
	for _, e := range graph.Edges() {
		if e.Engine == graph.PureGo {
			edges[e.From+">"+e.To] = true
			if _, ok := Lookup(e.From, e.To); !ok {
				t.Errorf("graph edge %s→%s has no pure-Go converter", e.From, e.To)
			}
		}
	}
	for _, p := range Pairs() {
		if !edges[p[0]+">"+p[1]] {
			t.Errorf("converter %s→%s is not in the graph", p[0], p[1])
		}
	}
}

func TestUnsupportedAndTooLarge(t *testing.T) {
	if _, err := Convert("png", "mp4", nil, nil); err == nil {
		t.Error("png→mp4 should be unsupported")
	}
	big := make([]byte, MaxInput+1)
	if _, err := Convert("png", "jpg", big, nil); err != ErrTooLarge {
		t.Errorf("want ErrTooLarge, got %v", err)
	}
}

// ── images ──

func testImage() *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 12))
	for y := 0; y < 12; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.NRGBA{R: uint8(x * 16), G: uint8(y * 20), B: 128, A: 255})
		}
	}
	return img
}

func pngBytes(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestImageRoundTrips(t *testing.T) {
	src := pngBytes(t, testImage())
	targets := []string{"jpg", "gif", "bmp", "tiff"}
	for _, to := range targets {
		out, err := Convert("png", to, src, options.Options{options.Quality: 90})
		if err != nil {
			t.Fatalf("png→%s: %v", to, err)
		}
		if len(out) == 0 {
			t.Fatalf("png→%s: empty output", to)
		}
		back, err := Convert(to, "png", out, nil)
		if err != nil {
			t.Fatalf("%s→png: %v", to, err)
		}
		img, err := png.Decode(bytes.NewReader(back))
		if err != nil {
			t.Fatalf("%s→png: not a png: %v", to, err)
		}
		if img.Bounds().Dx() != 16 || img.Bounds().Dy() != 12 {
			t.Errorf("%s→png: bounds %v", to, img.Bounds())
		}
	}
	// lossless pairs keep pixels exactly
	for _, to := range []string{"bmp", "tiff"} {
		out, _ := Convert("png", to, src, nil)
		back, _ := Convert(to, "png", out, nil)
		img, _ := png.Decode(bytes.NewReader(back))
		want := testImage()
		for y := 0; y < 12; y++ {
			for x := 0; x < 16; x++ {
				r1, g1, b1, _ := img.At(x, y).RGBA()
				r2, g2, b2, _ := want.At(x, y).RGBA()
				if r1 != r2 || g1 != g2 || b1 != b2 {
					t.Fatalf("%s round trip changed pixel %d,%d", to, x, y)
				}
			}
		}
	}
}

func TestJPEGQualityMatters(t *testing.T) {
	src := pngBytes(t, testImage())
	hi, _ := Convert("png", "jpg", src, options.Options{options.Quality: 100})
	lo, _ := Convert("png", "jpg", src, options.Options{options.Quality: 5})
	if len(lo) >= len(hi) {
		t.Errorf("quality 5 (%d bytes) should be smaller than quality 100 (%d bytes)", len(lo), len(hi))
	}
}

func TestJPEGFlattensAlpha(t *testing.T) {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	// fully transparent: must come out white, not black
	out, err := Convert("png", "jpg", pngBytes(t, img), nil)
	if err != nil {
		t.Fatal(err)
	}
	back, _ := Convert("jpg", "png", out, nil)
	dec, _ := png.Decode(bytes.NewReader(back))
	r, g, b, _ := dec.At(1, 1).RGBA()
	if r < 0xf000 || g < 0xf000 || b < 0xf000 {
		t.Errorf("transparent pixel became %v %v %v, want white", r>>8, g>>8, b>>8)
	}
}

func TestBadImage(t *testing.T) {
	if _, err := Convert("png", "jpg", []byte("not a png"), nil); err == nil {
		t.Error("garbage should fail to decode")
	}
}

// ── archives ──

func makeZip(t *testing.T) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	if _, err := zw.Create("dir/"); err != nil {
		t.Fatal(err)
	}
	w, _ := zw.Create("dir/hello.txt")
	io.WriteString(w, "hello")
	w, _ = zw.Create("../escape.txt")
	io.WriteString(w, "nope")
	w, _ = zw.Create("/abs.txt")
	io.WriteString(w, "abs")
	zw.Close()
	return buf.Bytes()
}

func listTar(t *testing.T, r io.Reader) map[string]string {
	t.Helper()
	out := map[string]string{}
	tr := tar.NewReader(r)
	for {
		h, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(tr)
		out[h.Name] = string(b)
	}
	return out
}

func TestArchiveRoundTrips(t *testing.T) {
	src := makeZip(t)
	tarb, err := Convert("zip", "tar", src, nil)
	if err != nil {
		t.Fatal(err)
	}
	got := listTar(t, bytes.NewReader(tarb))
	if got["dir/hello.txt"] != "hello" {
		t.Errorf("tar entries %v", got)
	}
	if _, bad := got["../escape.txt"]; bad {
		t.Error("path traversal entry survived")
	}
	if got["abs.txt"] != "abs" {
		t.Errorf("absolute entry should be relativised: %v", got)
	}
	if _, ok := got["dir/"]; !ok {
		t.Errorf("directory entry lost: %v", got)
	}

	tgz, err := Convert("tar", "tgz", tarb, nil)
	if err != nil {
		t.Fatal(err)
	}
	gr, err := gzip.NewReader(bytes.NewReader(tgz))
	if err != nil {
		t.Fatal(err)
	}
	if listTar(t, gr)["dir/hello.txt"] != "hello" {
		t.Error("tgz content mismatch")
	}

	tzst, err := Convert("tgz", "tzst", tgz, nil)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zstd.NewReader(bytes.NewReader(tzst))
	if err != nil {
		t.Fatal(err)
	}
	if listTar(t, zr)["dir/hello.txt"] != "hello" {
		t.Error("tzst content mismatch")
	}
	zr.Close()

	zipb, err := Convert("tzst", "zip", tzst, nil)
	if err != nil {
		t.Fatal(err)
	}
	zrd, err := zip.NewReader(bytes.NewReader(zipb), int64(len(zipb)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zrd.File {
		names[f.Name] = true
	}
	if !names["dir/hello.txt"] || !names["dir/"] || !names["abs.txt"] {
		t.Errorf("zip names %v", names)
	}
}

func TestArchiveSkipsSymlinks(t *testing.T) {
	var buf bytes.Buffer
	tw := tar.NewWriter(&buf)
	tw.WriteHeader(&tar.Header{Name: "link", Typeflag: tar.TypeSymlink, Linkname: "/etc/passwd"})
	tw.WriteHeader(&tar.Header{Name: "f.txt", Typeflag: tar.TypeReg, Size: 2, Mode: 0o644})
	tw.Write([]byte("hi"))
	tw.Close()
	out, err := Convert("tar", "zip", buf.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(out), int64(len(out)))
	if len(zr.File) != 1 || zr.File[0].Name != "f.txt" {
		t.Errorf("zip should hold only f.txt, got %d entries", len(zr.File))
	}
}

func TestArchiveGarbage(t *testing.T) {
	if _, err := Convert("zip", "tar", []byte("garbage"), nil); err == nil {
		t.Error("garbage zip should fail")
	}
	if _, err := Convert("tgz", "zip", []byte("garbage"), nil); err == nil {
		t.Error("garbage tgz should fail")
	}
}

// ── data ──

const sampleCSV = "name,age,code\nAda,36,007\n\"Şule, Hanım\",29,true\n"

func TestCSVToJSONAndBack(t *testing.T) {
	out, err := Convert("csv", "json", []byte(sampleCSV), nil)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatalf("json: %v\n%s", err, out)
	}
	if len(rows) != 2 || rows[0]["name"] != "Ada" || rows[0]["code"] != "007" || rows[1]["name"] != "Şule, Hanım" {
		t.Errorf("rows %v", rows)
	}
	if rows[1]["code"] != "true" {
		t.Errorf("cells must stay strings, got %T", rows[1]["code"])
	}
	// column order must follow the header, not the alphabet
	if !strings.HasPrefix(strings.TrimSpace(string(out)), "[\n  {\n    \"name\"") {
		t.Errorf("column order lost:\n%s", out)
	}
	back, err := Convert("json", "csv", out, nil)
	if err != nil {
		t.Fatal(err)
	}
	// json→csv sorts columns (maps are unordered): age,code,name
	want := "age,code,name\n36,007,Ada\n29,true,\"Şule, Hanım\"\n"
	if string(back) != want {
		t.Errorf("csv back:\n%s\nwant:\n%s", back, want)
	}
}

func TestCSVToYAMLAndBack(t *testing.T) {
	out, err := Convert("csv", "yaml", []byte(sampleCSV), nil)
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := yaml.Unmarshal(out, &rows); err != nil {
		t.Fatalf("yaml: %v\n%s", err, out)
	}
	if rows[0]["code"] != "007" || rows[1]["code"] != "true" || rows[0]["age"] != "36" {
		t.Errorf("yaml cells must round-trip as strings: %v\n%s", rows, out)
	}
	back, err := Convert("yaml", "csv", out, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(back), "Ada") || !strings.Contains(string(back), "007") {
		t.Errorf("csv back:\n%s", back)
	}
}

func TestJSONYAML(t *testing.T) {
	src := []byte(`{"b": [1, 2.5, "x"], "a": {"nested": true, "n": null}}`)
	y, err := Convert("json", "yaml", src, nil)
	if err != nil {
		t.Fatal(err)
	}
	j, err := Convert("yaml", "json", y, nil)
	if err != nil {
		t.Fatal(err)
	}
	var v map[string]any
	if err := json.Unmarshal(j, &v); err != nil {
		t.Fatal(err)
	}
	if v["a"].(map[string]any)["nested"] != true || len(v["b"].([]any)) != 3 {
		t.Errorf("round trip changed data: %s", j)
	}
	if _, err := Convert("json", "csv", []byte(`"just a string"`), nil); err == nil {
		t.Error("a scalar is not tabular")
	}
	if _, err := Convert("json", "yaml", []byte(`{bad json`), nil); err == nil {
		t.Error("bad json should fail")
	}
}

func TestJSONArrayOfArraysToTSV(t *testing.T) {
	out, err := Convert("json", "tsv", []byte(`[["a","b"],[1,true],[null,"x\ty"]]`), nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "a\tb\n1\ttrue\n\t\"x\ty\"\n"
	if string(out) != want {
		t.Errorf("tsv:\n%q\nwant:\n%q", out, want)
	}
}

func TestCSVTSV(t *testing.T) {
	tsv, err := Convert("csv", "tsv", []byte(sampleCSV), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(tsv), "Şule, Hanım\t29") {
		t.Errorf("tsv: %q", tsv)
	}
	csv, err := Convert("tsv", "csv", tsv, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(csv) != sampleCSV {
		t.Errorf("csv→tsv→csv changed:\n%q\nwant\n%q", csv, sampleCSV)
	}
}

// ── text ──

func TestMarkdownToHTML(t *testing.T) {
	src := []byte("# Başlık\n\nMerhaba **dünya**.\n\n| a | b |\n|---|---|\n| 1 | 2 |\n")
	out, err := Convert("md", "html", src, nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	for _, want := range []string{"<title>Başlık</title>", "<h1", "<strong>dünya</strong>", "<table>", `charset="utf-8"`} {
		if !strings.Contains(s, want) {
			t.Errorf("html lacks %q:\n%s", want, s)
		}
	}
}

func TestTxtToHTMLEscapes(t *testing.T) {
	out, err := Convert("txt", "html", []byte("a < b & c\nline2\n\nsecond <para>"), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, "a &lt; b &amp; c<br>\nline2") || !strings.Contains(s, "<p>second &lt;para&gt;</p>") {
		t.Errorf("html:\n%s", s)
	}
	if strings.Contains(s, "<para>") {
		t.Error("unescaped tag")
	}
}

func TestHTMLToText(t *testing.T) {
	src := `<html><head><title>T</title><style>p{color:red}</style></head>
<body><h1>Head</h1><p>One &amp; two&nbsp;three</p><script>alert(1)</script>
<ul><li>x</li><li>y</li></ul><table><tr><td>a</td><td>b</td></tr></table><!-- c --></body></html>`
	out, err := Convert("html", "txt", []byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "alert") || strings.Contains(s, "color:red") || strings.Contains(s, "<") {
		t.Errorf("text still has markup:\n%s", s)
	}
	for _, want := range []string{"Head", "One & two three", "x\ny", "a\tb"} {
		if !strings.Contains(s, want) {
			t.Errorf("text lacks %q:\n%q", want, s)
		}
	}
	if strings.Contains(s, "\n\n\n") {
		t.Errorf("blank lines not collapsed:\n%q", s)
	}
}

func TestTxtMdCopy(t *testing.T) {
	src := []byte("plain\n")
	out, _ := Convert("txt", "md", src, nil)
	if string(out) != "plain\n" {
		t.Error("txt→md must copy")
	}
	out, _ = Convert("md", "txt", src, nil)
	if string(out) != "plain\n" {
		t.Error("md→txt must copy")
	}
}
