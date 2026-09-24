package purego

import (
	"archive/zip"
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"strings"
	"testing"

	"golang.org/x/image/font/gofont/goregular"

	"github.com/brf-tech/filex-convert/internal/options"
)

// ── images: the new codecs ──

func TestNewImageCodecsRoundTrip(t *testing.T) {
	src := testImage()
	var pngBuf bytes.Buffer
	if err := png.Encode(&pngBuf, src); err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"qoi", "pnm", "tga", "webp"} {
		enc, err := Convert("png", id, pngBuf.Bytes(), nil)
		if err != nil {
			t.Fatalf("png→%s: %v", id, err)
		}
		back, err := Convert(id, "png", enc, nil)
		if err != nil {
			t.Fatalf("%s→png: %v", id, err)
		}
		img, err := png.Decode(bytes.NewReader(back))
		if err != nil {
			t.Fatalf("%s: decode back: %v", id, err)
		}
		if img.Bounds() != src.Bounds() {
			t.Errorf("%s: bounds %v", id, img.Bounds())
		}
		// all four are lossless: pixels survive exactly
		for y := 0; y < 12; y++ {
			for x := 0; x < 16; x++ {
				want := color.NRGBAModel.Convert(src.At(x, y)).(color.NRGBA)
				got := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
				if want != got {
					t.Fatalf("%s: pixel %d,%d = %v want %v", id, x, y, got, want)
				}
			}
		}
	}
}

func TestPNMASCIIAndGray(t *testing.T) {
	img, err := decodePNM(strings.NewReader("P3\n# comment\n2 1\n255\n255 0 0  0 0 255\n"))
	if err != nil {
		t.Fatal(err)
	}
	if c := color.NRGBAModel.Convert(img.At(1, 0)).(color.NRGBA); c.B != 255 || c.R != 0 {
		t.Errorf("P3 pixel %v", c)
	}
	img, err = decodePNM(strings.NewReader("P1\n3 1\n1 0 1\n"))
	if err != nil {
		t.Fatal(err)
	}
	if g := img.(*image.Gray); g.Pix[0] != 0 || g.Pix[1] != 255 {
		t.Errorf("P1 pixels %v", g.Pix)
	}
	if _, err := decodePNM(strings.NewReader("P9\n1 1\n")); err == nil {
		t.Error("bad magic accepted")
	}
}

func TestICOEncodeDecode(t *testing.T) {
	src := image.NewNRGBA(image.Rect(0, 0, 64, 40))
	for i := range src.Pix {
		src.Pix[i] = uint8(i)
	}
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, src)
	for _, set := range []struct {
		name  string
		count int
	}{{"favicon", 3}, {"app", 7}, {"single", 1}} {
		ico, err := Convert("png", "ico", pngBuf.Bytes(), options.Options{"icon_sizes": set.name})
		if err != nil {
			t.Fatal(err)
		}
		if n := int(ico[4]) | int(ico[5])<<8; n != set.count {
			t.Errorf("%s: %d entries, want %d", set.name, n, set.count)
		}
		back, err := decodeICO(bytes.NewReader(ico))
		if err != nil {
			t.Fatalf("%s: decode: %v", set.name, err)
		}
		if set.name == "single" && back.Bounds().Dx() != 64 {
			t.Errorf("single keeps the source size, got %v", back.Bounds())
		}
	}
	// a classic BMP-DIB icon with an AND mask decodes too
	dib := bytes.Buffer{}
	hdr := make([]byte, 40)
	hdr[0] = 40
	hdr[4], hdr[8] = 2, 4 // 2 px wide, 2*2 high (xor + and)
	hdr[12] = 1
	hdr[14] = 24
	dib.Write(hdr)
	dib.Write([]byte{0, 0, 255, 0, 255, 0, 0, 0}) // row 0 (bottom): red, green + pad
	dib.Write([]byte{255, 0, 0, 255, 255, 255, 0, 0})
	dib.Write([]byte{0x40, 0, 0, 0, 0, 0, 0, 0}) // AND mask: second pixel of bottom row transparent
	var ico bytes.Buffer
	ico.Write([]byte{0, 0, 1, 0, 1, 0, 2, 2, 0, 0, 1, 0, 24, 0})
	ico.Write([]byte{byte(dib.Len()), 0, 0, 0, 22, 0, 0, 0})
	ico.Write(dib.Bytes())
	img, err := decodeICO(bytes.NewReader(ico.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	if c := img.At(0, 1).(color.NRGBA); c.R != 255 || c.A != 255 {
		t.Errorf("bottom-left should be opaque red, got %v", c)
	}
	if c := img.At(1, 1).(color.NRGBA); c.A != 0 {
		t.Errorf("masked pixel should be transparent, got %v", c)
	}
}

func TestTGAVariants(t *testing.T) {
	// 2×2 RLE true-colour, bottom-left origin: one run of two blue pixels
	// then two raw pixels
	tga := []byte{0, 0, 10, 0, 0, 0, 0, 0, 0, 0, 0, 0, 2, 0, 2, 0, 24, 0}
	tga = append(tga, 0x81, 255, 0, 0) // run of 2: B=255
	tga = append(tga, 0x01, 0, 255, 0, 0, 0, 255)
	img, err := decodeTGA(bytes.NewReader(tga))
	if err != nil {
		t.Fatal(err)
	}
	if c := img.At(0, 1).(color.NRGBA); c.B != 255 || c.R != 0 {
		t.Errorf("bottom row run pixel %v", c)
	}
	if c := img.At(1, 0).(color.NRGBA); c.R != 255 {
		t.Errorf("top-right raw pixel %v", c)
	}
	if _, err := decodeTGA(bytes.NewReader(tga[:10])); err == nil {
		t.Error("short header accepted")
	}
}

func TestImageToPDFSVGText(t *testing.T) {
	var pngBuf bytes.Buffer
	png.Encode(&pngBuf, testImage())
	pdf, err := Convert("png", "pdf", pngBuf.Bytes(), nil)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) || !bytes.Contains(pdf, []byte("/Width 16 /Height 12")) {
		t.Errorf("png→pdf: %v", err)
	}
	jpg, _ := Convert("png", "jpg", pngBuf.Bytes(), nil)
	pdf, err = Convert("jpg", "pdf", jpg, nil)
	if err != nil || !bytes.Contains(pdf, []byte("/DCTDecode")) || !bytes.Contains(pdf, jpg) {
		t.Errorf("jpg→pdf should pass the JPEG through: %v", err)
	}
	svg, err := Convert("png", "svg", pngBuf.Bytes(), nil)
	if err != nil || !bytes.Contains(svg, []byte(`width="16" height="12"`)) || !bytes.Contains(svg, []byte("data:image/png;base64,")) {
		t.Errorf("png→svg: %v %s", err, svg)
	}
	txt, err := Convert("png", "txt", pngBuf.Bytes(), nil)
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimRight(string(txt), "\n"), "\n")
	if len(lines) != 6 || len(lines[0]) != 16 {
		t.Errorf("ascii art %d lines of %d", len(lines), len(lines[0]))
	}
}

// ── documents ──

const turkishMD = "# Başlık\n\nBir **kalın** ve *eğik* paragraf, `kod` ve [link](https://example.com).\n\n- madde bir\n- madde iki\n\n1. sıra\n2. sıra\n\n> alıntı\n\n```go\nfmt.Println(\"ş\")\n```\n\n| a | b |\n|---|---|\n| ç | ğ |\n\n---\n\nSon.\n"

func TestMarkdownDocxRoundTrip(t *testing.T) {
	docx, err := Convert("md", "docx", []byte(turkishMD), nil)
	if err != nil {
		t.Fatal(err)
	}
	zr, err := zip.NewReader(bytes.NewReader(docx), int64(len(docx)))
	if err != nil {
		t.Fatal(err)
	}
	names := map[string]bool{}
	for _, f := range zr.File {
		names[f.Name] = true
	}
	for _, want := range []string{"[Content_Types].xml", "word/document.xml", "word/styles.xml", "word/numbering.xml", "word/_rels/document.xml.rels"} {
		if !names[want] {
			t.Errorf("docx lacks %s", want)
		}
	}
	back, err := Convert("docx", "md", docx, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := string(back)
	for _, want := range []string{"# Başlık", "**kalın**", "*eğik*", "`kod`", "[link](https://example.com)", "- madde bir", "1. sıra", "2. sıra", "> alıntı", "```", "| a | b |", "| ç | ğ |", "Son."} {
		if !strings.Contains(md, want) {
			t.Errorf("docx→md lost %q:\n%s", want, md)
		}
	}
	txt, err := Convert("docx", "txt", docx, nil)
	if err != nil || !strings.Contains(string(txt), "Başlık") {
		t.Errorf("docx→txt: %v", err)
	}
	pdf, err := Convert("docx", "pdf", docx, nil)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("docx→pdf: %v", err)
	}
}

func TestMarkdownEPUBRoundTrip(t *testing.T) {
	epub, err := Convert("md", "epub", []byte(turkishMD), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(epub[30:], []byte("mimetypeapplication/epub+zip")) {
		t.Error("mimetype must be the first, stored entry")
	}
	back, err := Convert("epub", "md", epub, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(back), "# Başlık") || !strings.Contains(string(back), "**kalın**") || !strings.Contains(string(back), "| ç | ğ |") {
		t.Errorf("epub→md:\n%s", back)
	}
	html, err := Convert("epub", "html", epub, nil)
	if err != nil || !bytes.Contains(html, []byte("<h1>Başlık</h1>")) {
		t.Errorf("epub→html: %v", err)
	}
}

func TestHTMLToMarkdown(t *testing.T) {
	src := `<html><head><title>T</title><style>p{}</style></head><body><h2>Selam &amp; dünya</h2><p>Bir <b>kalın</b> <a href="/x">bağ</a>.<br>ikinci</p><ul><li>bir</li><li>iki<ul><li>iç</li></ul></li></ul><pre>x = 1
y = 2</pre><table><tr><th>a</th><th>b</th></tr><tr><td>1</td><td>2</td></tr></table><script>alert(1)</script></body></html>`
	out, err := Convert("html", "md", []byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	md := string(out)
	for _, want := range []string{"## Selam & dünya", "**kalın**", "[bağ](/x)", "- bir", "- iki", "  - iç", "```\nx = 1\ny = 2\n```", "| a | b |", "| 1 | 2 |"} {
		if !strings.Contains(md, want) {
			t.Errorf("html→md lost %q:\n%s", want, md)
		}
	}
	if strings.Contains(md, "alert") {
		t.Error("script leaked")
	}
	pdf, err := Convert("html", "pdf", []byte(src), nil)
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("html→pdf: %v", err)
	}
}

func TestRTFToText(t *testing.T) {
	rtf := `{\rtf1\ansi\ansicpg1254\deff0{\fonttbl{\f0 Arial;}}{\colortbl;\red0\green0\blue0;}
\pard Merhaba \b d\'fcnya\b0  ve \u351?arap.\par
\pard\intbl a\cell b\cell\row
\pard Son sat\'fdr.\par}`
	out, err := Convert("rtf", "txt", []byte(rtf), nil)
	if err != nil {
		t.Fatal(err)
	}
	txt := string(out)
	for _, want := range []string{"Merhaba dünya ve şarap.", "a\tb", "Son satır."} {
		if !strings.Contains(txt, want) {
			t.Errorf("rtf→txt lost %q:\n%s", want, txt)
		}
	}
	if strings.Contains(txt, "Arial") {
		t.Error("font table leaked")
	}
	md, _ := Convert("rtf", "md", []byte(rtf), nil)
	if !strings.Contains(string(md), "**dünya**") {
		t.Errorf("bold lost: %s", md)
	}
	if _, err := Convert("rtf", "txt", []byte("plain"), nil); err == nil {
		t.Error("non-RTF accepted")
	}
}

// zipOf builds an in-memory package for the ODF and PPTX readers.
func zipOf(files map[string]string) []byte {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for name, body := range files {
		w, _ := zw.Create(name)
		w.Write([]byte(body))
	}
	zw.Close()
	return buf.Bytes()
}

func TestODTAndODSReaders(t *testing.T) {
	odt := zipOf(map[string]string{
		"content.xml": `<?xml version="1.0"?><office:document-content xmlns:office="o" xmlns:text="t" xmlns:style="s" xmlns:fo="f" xmlns:table="tb">
<office:automatic-styles><style:style style:name="T1"><style:text-properties fo:font-weight="bold"/></style:style>
<text:list-style style:name="L1"><text:list-level-style-number text:level="1"/></text:list-style></office:automatic-styles>
<office:body><office:text><text:h text:outline-level="2">Başlık</text:h><text:p>Bir <text:span text:style-name="T1">kalın</text:span><text:s text:c="2"/>söz.</text:p>
<text:list text:style-name="L1"><text:list-item><text:p>ilk</text:p></text:list-item><text:list-item><text:p>ikinci</text:p></text:list-item></text:list>
<table:table><table:table-row><table:table-cell><text:p>a</text:p></table:table-cell><table:table-cell><text:p>b</text:p></table:table-cell></table:table-row></table:table>
</office:text></office:body></office:document-content>`,
	})
	out, err := Convert("odt", "md", odt, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := string(out)
	for _, want := range []string{"## Başlık", "Bir **kalın**  söz.", "1. ilk", "2. ikinci", "| a | b |"} {
		if !strings.Contains(md, want) {
			t.Errorf("odt→md lost %q:\n%s", want, md)
		}
	}
	ods := zipOf(map[string]string{
		"content.xml": `<?xml version="1.0"?><office:document-content xmlns:office="o" xmlns:table="tb" xmlns:text="t"><office:body><office:spreadsheet>
<table:table table:name="S1"><table:table-row><table:table-cell office:value-type="string"><text:p>ad</text:p></table:table-cell><table:table-cell><text:p>sayı</text:p></table:table-cell></table:table-row>
<table:table-row><table:table-cell><text:p>ş</text:p></table:table-cell><table:table-cell office:value-type="float" office:value="12.5"><text:p>12,5</text:p></table:table-cell><table:table-cell table:number-columns-repeated="1000"/></table:table-row>
<table:table-row table:number-rows-repeated="1048000"><table:table-cell table:number-columns-repeated="1002"/></table:table-row>
</table:table></office:spreadsheet></office:body></office:document-content>`,
	})
	csv, err := Convert("ods", "csv", ods, nil)
	if err != nil {
		t.Fatal(err)
	}
	if string(csv) != "ad,sayı\nş,\"12,5\"\n" {
		t.Errorf("ods→csv = %q", csv)
	}
}

func TestPPTXReader(t *testing.T) {
	slide := func(title, body string) string {
		return `<p:sld xmlns:a="a" xmlns:p="p"><p:cSld><p:spTree><p:sp><p:nvSpPr><p:nvPr><p:ph type="title"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:r><a:t>` + title + `</a:t></a:r></a:p></p:txBody></p:sp>
<p:sp><p:nvSpPr><p:nvPr><p:ph type="body"/></p:nvPr></p:nvSpPr><p:txBody><a:p><a:pPr lvl="0"/><a:r><a:rPr b="1"/><a:t>` + body + `</a:t></a:r></a:p><a:p><a:pPr lvl="1"/><a:r><a:t>alt madde</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`
	}
	pptx := zipOf(map[string]string{
		"ppt/slides/slide2.xml":  slide("İkinci", "gövde iki"),
		"ppt/slides/slide1.xml":  slide("Birinci", "gövde bir"),
		"ppt/slides/slide10.xml": slide("Onuncu", "gövde on"),
	})
	out, err := Convert("pptx", "md", pptx, nil)
	if err != nil {
		t.Fatal(err)
	}
	md := string(out)
	i1, i2, i10 := strings.Index(md, "## Birinci"), strings.Index(md, "## İkinci"), strings.Index(md, "## Onuncu")
	if i1 < 0 || i2 < i1 || i10 < i2 {
		t.Errorf("slides out of order:\n%s", md)
	}
	if !strings.Contains(md, "- **gövde bir**") || !strings.Contains(md, "  - alt madde") {
		t.Errorf("bullets:\n%s", md)
	}
	if _, err := Convert("pptx", "txt", zipOf(map[string]string{"x": "y"}), nil); err == nil {
		t.Error("package without slides accepted")
	}
}

// ── data ──

func TestJSONXMLRoundTrip(t *testing.T) {
	src := `{"kişi":{"ad":"Şule","yaş":30,"etiket":["a","b"],"aktif":true}}`
	xml, err := Convert("json", "xml", []byte(src), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<root>", "<kişi>", "<ad>Şule</ad>", "<etiket>a</etiket>", "<etiket>b</etiket>", "<yaş>30</yaş>"} {
		if !strings.Contains(string(xml), want) {
			t.Errorf("json→xml lacks %q:\n%s", want, xml)
		}
	}
	back, err := Convert("xml", "json", xml, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`"ad": "Şule"`, `"etiket": [`, `"a"`, `"yaş": "30"`} {
		if !strings.Contains(string(back), want) {
			t.Errorf("xml→json lacks %q:\n%s", want, back)
		}
	}
	// attributes and repeated elements
	j, err := Convert("xml", "json", []byte(`<list v="1"><item id="a">x</item><item id="b">y</item></list>`), nil)
	if err != nil || !strings.Contains(string(j), `"@v": "1"`) || !strings.Contains(string(j), `"#text": "x"`) {
		t.Errorf("xml attrs: %v %s", err, j)
	}
	arr, err := Convert("json", "xml", []byte(`[{"a":1},{"a":2}]`), nil)
	if err != nil || strings.Count(string(arr), "<item>") != 2 {
		t.Errorf("array root: %v %s", err, arr)
	}
	back, _ = Convert("xml", "json", arr, nil)
	if !strings.HasPrefix(strings.TrimSpace(string(back)), "[") {
		t.Errorf("array should round-trip as an array: %s", back)
	}
}

func TestTOML(t *testing.T) {
	toml, err := Convert("json", "toml", []byte(`{"title":"örnek","owner":{"name":"Ş","dob":null},"ports":[80,443]}`), nil)
	if err != nil {
		t.Fatal(err)
	}
	s := string(toml)
	if !strings.Contains(s, `title = "örnek"`) || !strings.Contains(s, "[owner]") || !strings.Contains(s, "ports = [80, 443]") || strings.Contains(s, "dob") {
		t.Errorf("json→toml:\n%s", s)
	}
	y, err := Convert("toml", "yaml", toml, nil)
	if err != nil || !strings.Contains(string(y), "title: örnek") {
		t.Errorf("toml→yaml: %v %s", err, y)
	}
	if _, err := Convert("toml", "json", []byte("= broken"), nil); err == nil {
		t.Error("broken TOML accepted")
	}
}

func TestCSVToMarkdownXLSXAndBack(t *testing.T) {
	csv := "ad,sayı,not\nŞule,007,\"a, b\"\nAli,12.5,\n"
	md, err := Convert("csv", "md", []byte(csv), nil)
	if err != nil || string(md) != "| ad | sayı | not |\n|---|---|---|\n| Şule | 007 | a, b |\n| Ali | 12.5 |  |\n" {
		t.Errorf("csv→md: %v\n%s", err, md)
	}
	html, err := Convert("csv", "html", []byte(csv), nil)
	if err != nil || !strings.Contains(string(html), "<th>ad</th>") || !strings.Contains(string(html), "<td>a, b</td>") {
		t.Errorf("csv→html: %v", err)
	}
	xlsx, err := Convert("csv", "xlsx", []byte(csv), nil)
	if err != nil {
		t.Fatal(err)
	}
	zr, _ := zip.NewReader(bytes.NewReader(xlsx), int64(len(xlsx)))
	found := false
	for _, f := range zr.File {
		if f.Name == "xl/worksheets/sheet1.xml" {
			found = true
			rc, _ := f.Open()
			var b bytes.Buffer
			b.ReadFrom(rc)
			sheet := b.String()
			if !strings.Contains(sheet, `<c r="B3"><v>12.5</v></c>`) || !strings.Contains(sheet, `<c r="B2" t="inlineStr"><is><t xml:space="preserve">007</t></is></c>`) {
				t.Errorf("sheet cells: %s", sheet)
			}
			if !strings.Contains(sheet, `<c r="A1" s="1" t="inlineStr">`) {
				t.Errorf("header should be bold: %s", sheet)
			}
		}
	}
	if !found {
		t.Fatal("no sheet in xlsx")
	}
	back, err := Convert("xlsx", "csv", xlsx, nil)
	if err != nil || string(back) != csv {
		t.Errorf("xlsx→csv = %q (%v)", back, err)
	}
	j, err := Convert("xlsx", "json", xlsx, nil)
	if err != nil || !strings.Contains(string(j), `"sayı": "007"`) {
		t.Errorf("xlsx→json: %v %s", err, j)
	}
	pdf, err := Convert("csv", "md", []byte(csv), nil)
	if err == nil {
		pdf, err = Convert("md", "pdf", pdf, nil)
	}
	if err != nil || !bytes.HasPrefix(pdf, []byte("%PDF-")) {
		t.Errorf("csv→md→pdf: %v", err)
	}
}

func TestXLSXSharedStrings(t *testing.T) {
	xlsx := zipOf(map[string]string{
		"xl/workbook.xml":            `<workbook xmlns:r="r"><sheets><sheet name="Veri" sheetId="1" r:id="rId9"/></sheets></workbook>`,
		"xl/_rels/workbook.xml.rels": `<Relationships><Relationship Id="rId9" Target="worksheets/data.xml"/></Relationships>`,
		"xl/sharedStrings.xml":       `<sst><si><t>ad</t></si><si><r><t>Şu</t></r><r><t>le</t></r></si></sst>`,
		"xl/worksheets/data.xml":     `<worksheet><sheetData><row r="1"><c r="A1" t="s"><v>0</v></c><c r="C1"><v>3</v></c></row><row r="2"><c r="A2" t="s"><v>1</v></c><c r="B2" t="b"><v>1</v></c></row></sheetData></worksheet>`,
	})
	csv, err := Convert("xlsx", "csv", xlsx, nil)
	if err != nil || string(csv) != "ad,,3\nŞule,TRUE,\n" {
		t.Errorf("xlsx→csv = %q (%v)", csv, err)
	}
}

// ── archives ──

func TestTxzRoundTripAndBz2Fixture(t *testing.T) {
	zipData := makeZip(t)
	txz, err := Convert("zip", "txz", zipData, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(txz, []byte{0xFD, '7', 'z', 'X', 'Z', 0}) {
		t.Error("not an xz stream")
	}
	back, err := Convert("txz", "zip", txz, nil)
	if err != nil {
		t.Fatal(err)
	}
	if names := zipNames(t, back); strings.Join(names, ",") != "dir/,dir/hello.txt,abs.txt" {
		t.Errorf("members %v", names)
	}
	bz, err := os.ReadFile("testdata/sample.tar.bz2")
	if err != nil {
		t.Fatal(err)
	}
	z, err := Convert("tbz2", "zip", bz, nil)
	if err != nil {
		t.Fatal(err)
	}
	if names := zipNames(t, z); strings.Join(names, ",") != "dir/hello.txt,dir/" {
		t.Errorf("tbz2 members %v", names)
	}
	if _, err := Convert("rar", "zip", []byte("not a rar"), nil); err == nil {
		t.Error("garbage rar accepted")
	}
}

func TestPackAnyFile(t *testing.T) {
	out, err := Convert("md", "zip", []byte("# x"), options.Options{SourceName: `C:\docs\Notlar ş.md`})
	if err != nil {
		t.Fatal(err)
	}
	if names := zipNames(t, out); len(names) != 1 || names[0] != "Notlar ş.md" {
		t.Errorf("members %v", names)
	}
	out, err = Convert("mp3", "tar", []byte("ID3"), nil)
	if err != nil || !bytes.Contains(out, []byte("file")) {
		t.Errorf("nameless pack: %v", err)
	}
	if _, ok := Lookup("zip", "zip"); ok {
		t.Error("an archive is never packed into itself")
	}
}

func zipNames(t *testing.T, data []byte) []string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, f := range zr.File {
		out = append(out, f.Name)
	}
	return out
}

// ── fonts ──

func TestFontWOFFRoundTrip(t *testing.T) {
	woff, err := Convert("ttf", "woff", goregular.TTF, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(woff, []byte("wOFF")) || len(woff) >= len(goregular.TTF) {
		t.Errorf("woff header/size: %d vs %d", len(woff), len(goregular.TTF))
	}
	back, err := Convert("woff", "ttf", woff, nil)
	if err != nil {
		t.Fatal(err)
	}
	_, tables, err := parseSFNT(back)
	if err != nil {
		t.Fatal(err)
	}
	_, orig, _ := parseSFNT(goregular.TTF)
	if len(tables) != len(orig) {
		t.Fatalf("%d tables, want %d", len(tables), len(orig))
	}
	for i := range tables {
		if tables[i].tag != orig[i].tag || !bytes.Equal(tables[i].data, orig[i].data) {
			t.Errorf("table %q differs after the round trip", tables[i].tag[:])
		}
	}
	if _, err := Convert("woff", "ttf", []byte("wOF2....."), nil); err == nil {
		t.Error("WOFF2 accepted")
	}
	otf, err := Convert("ttf", "otf", goregular.TTF, nil)
	if err != nil || !bytes.Equal(otf, goregular.TTF) {
		t.Errorf("ttf→otf: %v", err)
	}
}

// ── subtitles ──

func TestSubtitles(t *testing.T) {
	srt := "1\n00:00:01,000 --> 00:00:02,500\nMerhaba <i>dünya</i>\n\n2\n00:01:00,250 --> 00:01:03,000\nİkinci & satır\nikinci satır 2\n\n"
	vtt, err := Convert("srt", "vtt", []byte(srt), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(vtt), "WEBVTT\n\n1\n00:00:01.000 --> 00:00:02.500\nMerhaba <i>dünya</i>\n") || !strings.Contains(string(vtt), "İkinci &amp; satır") {
		t.Errorf("srt→vtt:\n%s", vtt)
	}
	back, err := Convert("vtt", "srt", vtt, nil)
	if err != nil || string(back) != srt {
		t.Errorf("vtt→srt = %q (%v)", back, err)
	}
	txt, _ := Convert("srt", "txt", []byte(srt), nil)
	if string(txt) != "Merhaba dünya\nİkinci & satır\nikinci satır 2\n" {
		t.Errorf("srt→txt = %q", txt)
	}
	if _, err := Convert("srt", "vtt", []byte("no cues"), nil); err == nil {
		t.Error("cueless input accepted")
	}
}
