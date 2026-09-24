package pdfw

import (
	"fmt"
	"sort"
	"strings"

	"golang.org/x/image/font"
	"golang.org/x/image/font/gofont/gobold"
	"golang.org/x/image/font/gofont/goitalic"
	"golang.org/x/image/font/gofont/gomono"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/sfnt"
	"golang.org/x/image/math/fixed"
)

// Style picks one of the embedded faces.
type Style int

// The faces the layout uses.
const (
	Regular Style = iota
	Bold
	Italic
	Mono
)

// face is one embedded TrueType font with the metrics the layout needs.
type face struct {
	res     string // resource name in the page's /Font dictionary
	base    string // BaseFont name
	data    []byte
	f       *sfnt.Font
	buf     sfnt.Buffer
	upem    int
	glyphs  map[rune]sfnt.GlyphIndex
	adv     map[sfnt.GlyphIndex]int // advance in 1000-unit text space
	used    map[sfnt.GlyphIndex]rune
	ascent  int
	descent int
	capH    int
	bbox    [4]int
	num     int // object number once written
}

func loadFace(res, base string, data []byte) *face {
	f, err := sfnt.Parse(data)
	if err != nil {
		panic("pdfw: embedded font " + base + ": " + err.Error())
	}
	fc := &face{res: res, base: base, data: data, f: f, glyphs: map[rune]sfnt.GlyphIndex{}, adv: map[sfnt.GlyphIndex]int{}, used: map[sfnt.GlyphIndex]rune{}}
	fc.upem = int(f.UnitsPerEm())
	ppem := fixed.I(fc.upem)
	m, err := f.Metrics(&fc.buf, ppem, font.HintingNone)
	if err == nil {
		fc.ascent = scale1000(int(m.Ascent>>6), fc.upem)
		fc.descent = -scale1000(int(m.Descent>>6), fc.upem)
		fc.capH = scale1000(int(m.CapHeight>>6), fc.upem)
	}
	if b, err := f.Bounds(&fc.buf, ppem, font.HintingNone); err == nil {
		fc.bbox = [4]int{scale1000(int(b.Min.X>>6), fc.upem), -scale1000(int(b.Max.Y>>6), fc.upem), scale1000(int(b.Max.X>>6), fc.upem), -scale1000(int(b.Min.Y>>6), fc.upem)}
	}
	if fc.ascent == 0 {
		fc.ascent, fc.descent, fc.capH = 900, -200, 700
	}
	return fc
}

func scale1000(v, upem int) int {
	if upem == 0 {
		return v
	}
	return v * 1000 / upem
}

// glyph answers the glyph index for a rune, remembering it for the
// ToUnicode map; a rune the font lacks maps to .notdef (0).
func (fc *face) glyph(r rune) sfnt.GlyphIndex {
	if g, ok := fc.glyphs[r]; ok {
		return g
	}
	g, err := fc.f.GlyphIndex(&fc.buf, r)
	if err != nil {
		g = 0
	}
	fc.glyphs[r] = g
	if _, seen := fc.used[g]; !seen {
		fc.used[g] = r
	}
	return g
}

// advance answers a glyph's width in 1000-unit text space.
func (fc *face) advance(g sfnt.GlyphIndex) int {
	if w, ok := fc.adv[g]; ok {
		return w
	}
	a, err := fc.f.GlyphAdvance(&fc.buf, g, fixed.I(fc.upem), font.HintingNone)
	w := 0
	if err == nil {
		w = scale1000(int(a>>6), fc.upem)
	}
	fc.adv[g] = w
	return w
}

// width measures a string at a size in points.
func (fc *face) width(s string, size float64) float64 {
	total := 0
	for _, r := range s {
		total += fc.advance(fc.glyph(r))
	}
	return float64(total) * size / 1000
}

// encode renders a string as the hex glyph string for a Tj operator.
func (fc *face) encode(s string) string {
	var b strings.Builder
	b.WriteByte('<')
	for _, r := range s {
		b.WriteString(fmt.Sprintf("%04X", uint16(fc.glyph(r))))
	}
	b.WriteByte('>')
	return b.String()
}

// write emits the font objects (Type0 → CIDFontType2 → descriptor →
// FontFile2, plus ToUnicode) and answers the Type0 object's number.
func (fc *face) write(f *file) int {
	if fc.num != 0 {
		return fc.num
	}
	ff := f.addStream(" /Length1 "+itoa(len(fc.data)), fc.data, true)
	desc := f.add(fmt.Sprintf("<< /Type /FontDescriptor /FontName /%s /Flags 4 /FontBBox [%d %d %d %d] /ItalicAngle 0 /Ascent %d /Descent %d /CapHeight %d /StemV 80 /FontFile2 %d 0 R >>",
		fc.base, fc.bbox[0], fc.bbox[1], fc.bbox[2], fc.bbox[3], fc.ascent, fc.descent, fc.capH, ff))
	// widths of every glyph used, as individual entries
	gids := make([]int, 0, len(fc.used))
	for g := range fc.used {
		gids = append(gids, int(g))
	}
	sort.Ints(gids)
	var w strings.Builder
	for _, g := range gids {
		w.WriteString(fmt.Sprintf("%d [%d] ", g, fc.advance(sfnt.GlyphIndex(g))))
	}
	cid := f.add(fmt.Sprintf("<< /Type /Font /Subtype /CIDFontType2 /BaseFont /%s /CIDSystemInfo << /Registry (Adobe) /Ordering (Identity) /Supplement 0 >> /FontDescriptor %d 0 R /DW 500 /W [ %s] /CIDToGIDMap /Identity >>", fc.base, desc, w.String()))
	tu := f.addStream("", []byte(fc.toUnicode()), true)
	fc.num = f.add(fmt.Sprintf("<< /Type /Font /Subtype /Type0 /BaseFont /%s /Encoding /Identity-H /DescendantFonts [%d 0 R] /ToUnicode %d 0 R >>", fc.base, cid, tu))
	return fc.num
}

func (fc *face) toUnicode() string {
	var b strings.Builder
	b.WriteString("/CIDInit /ProcSet findresource begin\n12 dict begin\nbegincmap\n/CIDSystemInfo << /Registry (Adobe) /Ordering (UCS) /Supplement 0 >> def\n/CMapName /Adobe-Identity-UCS def\n/CMapType 2 def\n1 begincodespacerange\n<0000> <FFFF>\nendcodespacerange\n")
	gids := make([]int, 0, len(fc.used))
	for g := range fc.used {
		gids = append(gids, int(g))
	}
	sort.Ints(gids)
	for i := 0; i < len(gids); i += 100 {
		end := min(i+100, len(gids))
		b.WriteString(fmt.Sprintf("%d beginbfchar\n", end-i))
		for _, g := range gids[i:end] {
			r := fc.used[sfnt.GlyphIndex(g)]
			if r > 0xFFFF {
				r -= 0x10000
				b.WriteString(fmt.Sprintf("<%04X> <%04X%04X>\n", g, 0xD800+(r>>10), 0xDC00+(r&0x3FF)))
				continue
			}
			b.WriteString(fmt.Sprintf("<%04X> <%04X>\n", g, r))
		}
		b.WriteString("endbfchar\n")
	}
	b.WriteString("endcmap\nCMapName currentdict /CMap defineresource pop\nend\nend\n")
	return b.String()
}

func itoa(n int) string { return fmt.Sprintf("%d", n) }

// faces builds the four embedded faces for one document.
func newFaces() map[Style]*face {
	return map[Style]*face{
		Regular: loadFace("F1", "GoRegular", goregular.TTF),
		Bold:    loadFace("F2", "GoBold", gobold.TTF),
		Italic:  loadFace("F3", "GoItalic", goitalic.TTF),
		Mono:    loadFace("F4", "GoMono", gomono.TTF),
	}
}
