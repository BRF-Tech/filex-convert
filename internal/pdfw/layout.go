package pdfw

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/brf-tech/filex-convert/internal/doc"
)

// Page geometry: A4 portrait, 20 mm margins.
const (
	pageW   = 595.28
	pageH   = 841.89
	margin  = 56.7
	usableW = pageW - 2*margin
	bottomY = margin
	topY    = pageH - margin
)

const (
	bodySize = 11.0
	codeSize = 9.5
	leading  = 1.4
)

// run is a piece of one laid-out line.
type run struct {
	face   *face
	size   float64
	text   string
	width  float64
	color  string // "r g b rg" or "" for black
	strike bool
	href   string
}

// line is a laid-out row of runs with its height.
type line struct {
	runs   []run
	width  float64
	height float64
	ascent float64
}

type layout struct {
	f        *file
	faces    map[Style]*face
	contents []*bytes.Buffer
	links    [][]string // per page: link annotation dicts
	cur      *bytes.Buffer
	y        float64
}

// Document lays blocks out on A4 pages and answers the PDF bytes.
func Document(d *doc.Document) ([]byte, error) {
	l := &layout{f: newFile(), faces: newFaces()}
	l.newPage()
	for i, b := range d.Blocks {
		l.block(b, i > 0 && d.Blocks[i-1].Kind == doc.ListItem && b.Kind == doc.ListItem)
	}
	return l.finish(d.TitleOf()), nil
}

func (l *layout) newPage() {
	l.cur = &bytes.Buffer{}
	l.contents = append(l.contents, l.cur)
	l.links = append(l.links, nil)
	l.y = topY
}

// need makes sure h points fit on the page, else starts a new one.
func (l *layout) need(h float64) {
	if l.y-h < bottomY && l.y < topY-1 {
		l.newPage()
	}
}

func (l *layout) faceFor(s doc.Span) (*face, float64, string) {
	switch {
	case s.Code:
		return l.faces[Mono], codeSize, "0.2 0.2 0.2 rg"
	case s.Bold:
		return l.faces[Bold], bodySize, ""
	case s.Italic:
		return l.faces[Italic], bodySize, ""
	}
	return l.faces[Regular], bodySize, ""
}

// wrap lays spans into lines no wider than maxW.
func (l *layout) wrap(spans []doc.Span, maxW float64, sizeScale float64, forceFace *face) []line {
	var lines []line
	cur := line{}
	flush := func() {
		if cur.height == 0 {
			cur.height = bodySize * sizeScale * leading
			cur.ascent = bodySize * sizeScale
		}
		lines = append(lines, cur)
		cur = line{}
	}
	push := func(fc *face, size float64, color, href string, strike bool, word string) {
		w := fc.width(word, size)
		if cur.width+w > maxW && cur.width > 0 {
			// drop trailing space of the finished line
			trimTrailing(&cur)
			flush()
		}
		if w > maxW {
			// a single word wider than the line: break by rune
			for _, r := range word {
				rw := fc.width(string(r), size)
				if cur.width+rw > maxW && cur.width > 0 {
					flush()
				}
				cur.runs = append(cur.runs, run{face: fc, size: size, text: string(r), width: rw, color: color, strike: strike, href: href})
				cur.width += rw
				bump(&cur, size)
			}
			return
		}
		cur.runs = append(cur.runs, run{face: fc, size: size, text: word, width: w, color: color, strike: strike, href: href})
		cur.width += w
		bump(&cur, size)
	}
	for _, s := range spans {
		fc, size, color := l.faceFor(s)
		if forceFace != nil {
			fc = forceFace
		}
		size *= sizeScale
		if s.Href != "" && color == "" {
			color = "0 0.2 0.6 rg"
		}
		text := s.Text
		for text != "" {
			nl := strings.IndexByte(text, '\n')
			seg := text
			if nl >= 0 {
				seg = text[:nl]
				text = text[nl+1:]
			} else {
				text = ""
			}
			for _, word := range splitWords(seg) {
				push(fc, size, color, s.Href, s.Strike, word)
			}
			if nl >= 0 {
				bump(&cur, size)
				flush()
			}
		}
	}
	if len(cur.runs) > 0 {
		trimTrailing(&cur)
		flush()
	}
	return lines
}

func bump(ln *line, size float64) {
	if h := size * leading; h > ln.height {
		ln.height = h
	}
	if size > ln.ascent {
		ln.ascent = size
	}
}

func trimTrailing(ln *line) {
	for len(ln.runs) > 0 {
		last := &ln.runs[len(ln.runs)-1]
		t := strings.TrimRight(last.text, " ")
		if t == last.text {
			return
		}
		ln.width -= last.width
		last.text = t
		last.width = last.face.width(t, last.size)
		ln.width += last.width
		if t == "" {
			ln.runs = ln.runs[:len(ln.runs)-1]
			continue
		}
		return
	}
}

// splitWords splits at spaces, keeping each space attached to the word
// before it so widths include it.
func splitWords(s string) []string {
	var out []string
	start := 0
	for i := 0; i < len(s); i++ {
		if s[i] == ' ' {
			out = append(out, s[start:i+1])
			start = i + 1
		}
	}
	if start < len(s) {
		out = append(out, s[start:])
	}
	return out
}

// drawLines writes lines starting at x, moving y down; it breaks pages.
func (l *layout) drawLines(lines []line, x float64) {
	for _, ln := range lines {
		l.need(ln.height)
		l.drawLine(ln, x)
	}
}

func (l *layout) drawLine(ln line, x float64) {
	base := l.y - ln.ascent
	fmt.Fprintf(l.cur, "BT\n")
	cx := x
	for _, r := range ln.runs {
		color := r.color
		if color == "" {
			color = "0 0 0 rg"
		}
		fmt.Fprintf(l.cur, "%s /%s %s Tf 1 0 0 1 %s %s Tm %s Tj\n", color, r.face.res, ftoa(r.size), ftoa(cx), ftoa(base), r.face.encode(r.text))
		if r.href != "" {
			page := len(l.links) - 1
			l.links[page] = append(l.links[page], fmt.Sprintf("<< /Type /Annot /Subtype /Link /Rect [%s %s %s %s] /Border [0 0 0] /A << /S /URI /URI %s >> >>",
				ftoa(cx), ftoa(base-r.size*0.25), ftoa(cx+r.width), ftoa(base+r.size), pdfLiteral(r.href)))
		}
		cx += r.width
	}
	fmt.Fprintf(l.cur, "ET\n")
	cx = x
	for _, r := range ln.runs {
		if r.strike {
			fmt.Fprintf(l.cur, "0.5 w 0 0 0 RG %s %s m %s %s l S\n", ftoa(cx), ftoa(base+r.size*0.3), ftoa(cx+r.width), ftoa(base+r.size*0.3))
		}
		cx += r.width
	}
	l.y -= ln.height
}

func pdfLiteral(s string) string {
	r := strings.NewReplacer(`\`, `\\`, "(", `\(`, ")", `\)`, "\n", `\n`, "\r", `\r`)
	var b strings.Builder
	esc := r.Replace(s)
	for i := 0; i < len(esc); i++ {
		c := esc[i]
		if c < 32 || c > 126 {
			b.WriteString(fmt.Sprintf("\\%03o", c))
			continue
		}
		b.WriteByte(c)
	}
	return "(" + b.String() + ")"
}

var headingSize = map[int]float64{1: 22, 2: 17, 3: 14, 4: 12.5, 5: 11.5, 6: 11}

func (l *layout) block(b doc.Block, continuesList bool) {
	switch b.Kind {
	case doc.Heading:
		size := headingSize[clamp(b.Level, 1, 6)]
		spans := make([]doc.Span, len(b.Spans))
		for i, s := range b.Spans {
			s.Bold = true
			spans[i] = s
		}
		lines := l.wrap(spans, usableW, size/bodySize, nil)
		l.y -= 10
		if len(lines) > 0 {
			l.need(lines[0].height + 8)
		}
		l.drawLines(lines, margin)
		l.y -= 4
	case doc.Paragraph:
		l.drawLines(l.wrap(b.Spans, usableW, 1, nil), margin)
		l.y -= 7
	case doc.Quote:
		lines := l.wrap(b.Spans, usableW-20, 1, nil)
		for _, ln := range lines {
			l.need(ln.height)
			fmt.Fprintf(l.cur, "0.75 g %s %s 2.5 %s re f 0 g\n", ftoa(margin), ftoa(l.y-ln.height), ftoa(ln.height))
			l.drawLine(ln, margin+14)
		}
		l.y -= 7
	case doc.Code:
		l.code(b.Text)
	case doc.ListItem:
		if !continuesList {
			l.y -= 2
		}
		indent := margin + 16 + float64(b.Level)*16
		lines := l.wrap(b.Spans, usableW-(indent-margin), 1, nil)
		if len(lines) == 0 {
			lines = []line{{height: bodySize * leading, ascent: bodySize}}
		}
		l.need(lines[0].height)
		marker := "•"
		fc := l.faces[Regular]
		if b.Ordered {
			marker = strconv.Itoa(max(b.Number, 1)) + "."
		}
		mw := fc.width(marker, bodySize)
		fmt.Fprintf(l.cur, "BT 0 0 0 rg /%s %s Tf 1 0 0 1 %s %s Tm %s Tj ET\n", fc.res, ftoa(bodySize), ftoa(indent-mw-5), ftoa(l.y-lines[0].ascent), fc.encode(marker))
		l.drawLines(lines, indent)
		l.y -= 2
	case doc.Table:
		l.table(b)
	case doc.Rule:
		l.need(12)
		fmt.Fprintf(l.cur, "0.6 G 0.8 w %s %s m %s %s l S 0 G\n", ftoa(margin), ftoa(l.y-6), ftoa(pageW-margin), ftoa(l.y-6))
		l.y -= 12
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func (l *layout) code(text string) {
	fc := l.faces[Mono]
	pad := 6.0
	lh := codeSize * leading
	text = strings.TrimRight(strings.ReplaceAll(text, "\t", "    "), "\n")
	var rows []string
	maxChars := int((usableW - 2*pad) / fc.width("M", codeSize))
	for _, ln := range strings.Split(text, "\n") {
		r := []rune(ln)
		for len(r) > maxChars && maxChars > 0 {
			rows = append(rows, string(r[:maxChars]))
			r = r[maxChars:]
		}
		rows = append(rows, string(r))
	}
	l.y -= 2
	for i := 0; i < len(rows); {
		l.need(lh + 2*pad)
		fit := int((l.y - bottomY - 2*pad) / lh)
		if fit < 1 {
			fit = 1
		}
		end := min(i+fit, len(rows))
		h := float64(end-i)*lh + 2*pad
		fmt.Fprintf(l.cur, "0.95 g %s %s %s %s re f 0 g\n", ftoa(margin), ftoa(l.y-h), ftoa(usableW), ftoa(h))
		l.y -= pad
		for _, row := range rows[i:end] {
			l.drawLine(line{runs: []run{{face: fc, size: codeSize, text: row, width: fc.width(row, codeSize), color: "0.1 0.1 0.1 rg"}}, height: lh, ascent: codeSize}, margin+pad)
		}
		l.y -= pad
		i = end
	}
	l.y -= 7
}

func (l *layout) table(b doc.Block) {
	if len(b.Rows) == 0 {
		return
	}
	cols := 0
	for _, r := range b.Rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return
	}
	// column widths proportional to the longest cell, bounded
	weights := make([]float64, cols)
	for _, r := range b.Rows {
		for i, c := range r {
			w := l.faces[Regular].width(doc.PlainText(c), bodySize*0.9) + 12
			if w > weights[i] {
				weights[i] = w
			}
		}
	}
	total := 0.0
	for i := range weights {
		if weights[i] < 30 {
			weights[i] = 30
		}
		if weights[i] > usableW/2 {
			weights[i] = usableW / 2
		}
		total += weights[i]
	}
	widths := make([]float64, cols)
	for i := range weights {
		widths[i] = weights[i] / total * usableW
	}
	pad := 4.0
	l.y -= 2
	for ri, r := range b.Rows {
		cells := make([][]line, cols)
		rowH := 0.0
		for i := 0; i < cols; i++ {
			var spans []doc.Span
			if i < len(r) {
				spans = r[i]
			}
			if ri == 0 && b.HasHeader {
				bolded := make([]doc.Span, len(spans))
				for k, s := range spans {
					s.Bold = true
					bolded[k] = s
				}
				spans = bolded
			}
			cells[i] = l.wrap(spans, widths[i]-2*pad, 0.9, nil)
			h := 2 * pad
			for _, ln := range cells[i] {
				h += ln.height
			}
			if h > rowH {
				rowH = h
			}
		}
		if rowH < bodySize*leading+2*pad {
			rowH = bodySize*leading + 2*pad
		}
		l.need(rowH)
		top := l.y
		x := margin
		if ri == 0 && b.HasHeader {
			fmt.Fprintf(l.cur, "0.92 g %s %s %s %s re f 0 g\n", ftoa(margin), ftoa(top-rowH), ftoa(usableW), ftoa(rowH))
		}
		for i := 0; i < cols; i++ {
			fmt.Fprintf(l.cur, "0.7 G 0.5 w %s %s %s %s re S 0 G\n", ftoa(x), ftoa(top-rowH), ftoa(widths[i]), ftoa(rowH))
			l.y = top - pad
			for _, ln := range cells[i] {
				l.drawLine(ln, x+pad)
			}
			x += widths[i]
		}
		l.y = top - rowH
	}
	l.y -= 9
}

// finish writes pages, fonts and the catalog.
func (l *layout) finish(title string) []byte {
	f := l.f
	pagesNum := f.reserve()
	var fontDict strings.Builder
	for _, st := range []Style{Regular, Bold, Italic, Mono} {
		fc := l.faces[st]
		if len(fc.used) == 0 {
			continue
		}
		fontDict.WriteString(fmt.Sprintf("/%s %d 0 R ", fc.res, fc.write(f)))
	}
	res := f.add("<< /Font << " + fontDict.String() + ">> >>")
	var kids []string
	for i, c := range l.contents {
		cn := f.addStream("", c.Bytes(), true)
		annots := ""
		if len(l.links[i]) > 0 {
			var refs []string
			for _, a := range l.links[i] {
				refs = append(refs, strconv.Itoa(f.add(a))+" 0 R")
			}
			annots = " /Annots [" + strings.Join(refs, " ") + "]"
		}
		pn := f.add(fmt.Sprintf("<< /Type /Page /Parent %d 0 R /MediaBox [0 0 %s %s] /Resources %d 0 R /Contents %d 0 R%s >>", pagesNum, ftoa(pageW), ftoa(pageH), res, cn, annots))
		kids = append(kids, strconv.Itoa(pn)+" 0 R")
	}
	f.writeAt(pagesNum, fmt.Sprintf("<< /Type /Pages /Kids [%s] /Count %d >>", strings.Join(kids, " "), len(kids)))
	root := f.add(fmt.Sprintf("<< /Type /Catalog /Pages %d 0 R >>", pagesNum))
	return f.finish(root, title)
}
