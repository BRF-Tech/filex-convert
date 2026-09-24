package pdfw

import (
	"bytes"
	"compress/zlib"
	"image"
	"image/color"
	"image/jpeg"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/brf-tech/filex-convert/internal/doc"
)

// streams decompresses every Flate stream in a PDF and answers them with
// the dictionary that precedes each.
func streams(t *testing.T, pdf []byte) []struct{ dict, body string } {
	t.Helper()
	re := regexp.MustCompile(`(?s)<<([^>]*(?:>>[^>]*)*?)>>\s*stream\n`)
	var out []struct{ dict, body string }
	rest := pdf
	for {
		loc := re.FindSubmatchIndex(rest)
		if loc == nil {
			break
		}
		dict := string(rest[loc[2]:loc[3]])
		start := loc[1]
		end := bytes.Index(rest[start:], []byte("\nendstream"))
		if end < 0 {
			t.Fatal("unterminated stream")
		}
		raw := rest[start : start+end]
		body := raw
		if strings.Contains(dict, "/FlateDecode") {
			zr, err := zlib.NewReader(bytes.NewReader(raw))
			if err != nil {
				t.Fatalf("zlib: %v", err)
			}
			body, err = io.ReadAll(zr)
			if err != nil {
				t.Fatalf("inflate: %v", err)
			}
		}
		out = append(out, struct{ dict, body string }{dict, string(body)})
		rest = rest[start+end:]
	}
	return out
}

func TestTurkishMarkdownToPDF(t *testing.T) {
	src := "# Başlık: Türkçe ğüşiöç ĞÜŞİÖÇ\n\nBu bir **kalın** ve *eğik* paragraf. `kod` ve [bağlantı](https://example.com/ş).\n\n- ilk madde\n- ikinci madde\n  1. iç sıra\n\n> alıntı satırı\n\n```go\nfmt.Println(\"merhaba dünya\")\n```\n\n| ad | değer |\n|---|---|\n| ş | 1 |\n| İ | 2 |\n\n---\n\nSon paragraf.\n"
	d := doc.FromMarkdown([]byte(src))
	pdf, err := Document(d)
	if err != nil {
		t.Fatal(err)
	}
	if !IsPDF(pdf) || !bytes.HasSuffix(bytes.TrimSpace(pdf), []byte("%%EOF")) {
		t.Fatal("not a PDF")
	}
	s := string(pdf)
	for _, want := range []string{"/Type /Catalog", "/Subtype /Type0", "/Encoding /Identity-H", "/CIDFontType2", "/FontFile2", "/BaseFont /GoRegular", "/BaseFont /GoBold", "/BaseFont /GoMono", "/BaseFont /GoItalic", "/Subtype /Link", "/URI (https://example.com/\\305\\237)"} {
		if !strings.Contains(s, want) {
			t.Errorf("missing %q", want)
		}
	}
	// the embedded fonts are real TrueType files and the ToUnicode maps
	// carry the Turkish code points, so text extraction gives them back
	ttf, tounicode := 0, 0
	for _, st := range streams(t, pdf) {
		if strings.Contains(st.dict, "/Length1") {
			ttf++
			if !strings.HasPrefix(st.body, "\x00\x01\x00\x00") {
				t.Error("FontFile2 is not a TrueType font")
			}
		}
		if strings.Contains(st.body, "begincmap") {
			tounicode++
			if strings.Contains(st.body, "<0130>") || strings.Contains(st.body, "<015F>") || strings.Contains(st.body, "<011F>") {
				continue
			}
		}
	}
	if ttf < 4 {
		t.Errorf("expected 4 embedded fonts, found %d", ttf)
	}
	if tounicode < 4 {
		t.Errorf("expected 4 ToUnicode maps, found %d", tounicode)
	}
	joined := strings.Join(bodies(t, pdf), "\n")
	for _, cp := range []string{"<0130>", "<015F>", "<011F>", "<0131>", "<00E7>"} {
		if !strings.Contains(joined, cp) {
			t.Errorf("ToUnicode lacks code point %s", cp)
		}
	}
}

func bodies(t *testing.T, pdf []byte) []string {
	var out []string
	for _, st := range streams(t, pdf) {
		out = append(out, st.body)
	}
	return out
}

func TestLongDocumentPaginates(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 120; i++ {
		b.WriteString("Paragraph number ")
		b.WriteString(strings.Repeat("word ", 30))
		b.WriteString("\n\n")
	}
	pdf, err := Document(doc.FromText([]byte(b.String())))
	if err != nil {
		t.Fatal(err)
	}
	n := strings.Count(string(pdf), "/Type /Page ")
	if n < 5 {
		t.Errorf("expected several pages, got %d", n)
	}
	if !strings.Contains(string(pdf), "/Count "+itoa(n)) {
		t.Errorf("page count mismatch")
	}
}

func TestPicturesJPEGPassthroughAndFlate(t *testing.T) {
	img := image.NewRGBA(image.Rect(0, 0, 40, 30))
	for i := range img.Pix {
		img.Pix[i] = uint8(i)
	}
	var jb bytes.Buffer
	if err := jpeg.Encode(&jb, img, &jpeg.Options{Quality: 80}); err != nil {
		t.Fatal(err)
	}
	jp, err := ParseJPEG(jb.Bytes())
	if err != nil {
		t.Fatal(err)
	}
	if jp.Width != 40 || jp.Height != 30 || jp.ColorSpace != "DeviceRGB" {
		t.Errorf("jpeg header %+v", jp)
	}
	big := image.NewNRGBA(image.Rect(0, 0, 3000, 1000))
	big.Set(1, 1, color.NRGBA{R: 255, A: 128})
	pdf, err := Pictures([]*Picture{jp, FromImage(big)}, "pics")
	if err != nil {
		t.Fatal(err)
	}
	s := string(pdf)
	if !strings.Contains(s, "/Filter /DCTDecode") || !bytes.Contains(pdf, jb.Bytes()) {
		t.Error("JPEG was not passed through")
	}
	if !strings.Contains(s, "/Width 3000 /Height 1000") || !strings.Contains(s, "/Count 2") {
		t.Error("second page missing")
	}
	// the wide image page is scaled to the A4 long edge
	if !strings.Contains(s, "/MediaBox [0 0 841.89 280.63]") {
		t.Errorf("wide page not scaled: %s", regexp.MustCompile(`/MediaBox \[[^\]]*\]`).FindAllString(s, -1))
	}
	if _, err := ParseJPEG([]byte("not a jpeg")); err != ErrNotJPEG {
		t.Errorf("want ErrNotJPEG, got %v", err)
	}
}
