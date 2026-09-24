package office

import (
	"encoding/xml"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/brf-tech/filex-convert/internal/doc"
)

// ── ODT reader ──

// ReadODT reads an OpenDocument Text file's content.xml.
func ReadODT(data []byte) (*doc.Document, error) {
	p, err := openPkg(data)
	if err != nil {
		return nil, err
	}
	body, err := p.read("content.xml")
	if err != nil {
		return nil, err
	}
	r := &odtReader{d: &doc.Document{}, bold: map[string]bool{}, italic: map[string]bool{}, mono: map[string]bool{}, numbered: map[string]bool{},
		langs: map[string]langSet{}, parents: map[string]string{}, votes: langVotes{}}
	if st, err := p.read("styles.xml"); err == nil {
		r.styles(st)
	}
	r.styles(body)
	if err := r.parse(body); err != nil {
		return nil, err
	}
	metaLang := ""
	if meta, err := p.read("meta.xml"); err == nil {
		r.d.Title = xmlText(meta, "title")
		metaLang = xmlText(meta, "language")
	}
	// meta.xml's dc:language is the author saying so outright; otherwise
	// the language the text's styles mark most of it with.
	r.d.Lang = doc.FirstLang(metaLang, r.votes.top())
	return r.d, nil
}

type odtReader struct {
	d        *doc.Document
	bold     map[string]bool
	italic   map[string]bool
	mono     map[string]bool
	numbered map[string]bool // list style name → level 1 is numbered
	// Language per style (own marking), each style's parent, the default
	// paragraph style's language, and the language stack of the open
	// h/p/span elements — see lang.go.
	langs       map[string]langSet
	parents     map[string]string
	langDefault langSet
	langStack   []langSet
	votes       langVotes

	spans    []doc.Span
	style    []doc.Span // span style stack
	inText   bool
	kind     doc.Kind
	level    int
	lists    []bool // ordered per nesting level
	counts   []int
	inItem   bool
	table    *doc.Block
	row      [][]doc.Span
	cell     []doc.Span
	inCell   bool
	tblDeep  int
	href     string
	skipDeep int
	lastCode bool
}

// styles collects bold/italic/monospace flags per style name.
func (r *odtReader) styles(data []byte) {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	cur := ""
	inDefault := false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "style", "list-style":
				cur = attrOf(t, "name")
				if parent := attrOf(t, "parent-style-name"); cur != "" && parent != "" {
					r.parents[cur] = parent
				}
			case "default-style":
				inDefault = attrOf(t, "family") == "paragraph"
			case "list-level-style-number":
				if cur != "" && attrOf(t, "level") == "1" {
					r.numbered[cur] = true
				}
			case "text-properties":
				if inDefault {
					r.langDefault = odfLang(t)
				}
				if cur == "" {
					continue
				}
				if l := odfLang(t); l != (langSet{}) {
					r.langs[cur] = l
				}
				for _, a := range t.Attr {
					switch a.Name.Local {
					case "font-weight":
						if a.Value == "bold" || a.Value >= "600" && len(a.Value) == 3 {
							r.bold[cur] = true
						}
					case "font-style":
						if a.Value == "italic" || a.Value == "oblique" {
							r.italic[cur] = true
						}
					case "font-name", "font-family":
						f := strings.ToLower(a.Value)
						if strings.Contains(f, "mono") || strings.Contains(f, "courier") || strings.Contains(f, "consolas") {
							r.mono[cur] = true
						}
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "style" || t.Name.Local == "list-style" {
				cur = ""
			}
			if t.Name.Local == "default-style" {
				inDefault = false
			}
		}
	}
}

func (r *odtReader) cur() doc.Span {
	if n := len(r.style); n > 0 {
		return r.style[n-1]
	}
	return doc.Span{}
}

func (r *odtReader) parse(body []byte) error {
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("odt: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			r.start(t)
		case xml.EndElement:
			r.end(t)
		case xml.CharData:
			if r.inText && r.skipDeep == 0 {
				s := r.cur()
				s.Text = string(t)
				s.Href = r.href
				r.spans = append(r.spans, s)
				r.votes.add(s.Text, r.langTop().or(r.langDefault))
			}
		}
	}
	return nil
}

// styleLang is a style's language: its own marking, then its parents'.
func (r *odtReader) styleLang(name string) langSet {
	s := langSet{}
	seen := map[string]bool{}
	for cur := name; cur != "" && !seen[cur]; cur = r.parents[cur] {
		seen[cur] = true
		s = s.or(r.langs[cur])
	}
	return s
}

func (r *odtReader) langTop() langSet {
	if n := len(r.langStack); n > 0 {
		return r.langStack[n-1]
	}
	return langSet{}
}

// pushLang opens an h/p/span: its style's language, inheriting what the
// enclosing element leaves unmarked. end() pops it.
func (r *odtReader) pushLang(style string) {
	r.langStack = append(r.langStack, r.styleLang(style).or(r.langTop()))
}

func (r *odtReader) popLang() {
	if n := len(r.langStack); n > 0 {
		r.langStack = r.langStack[:n-1]
	}
}

func (r *odtReader) styled(name string) doc.Span {
	s := r.cur()
	if r.bold[name] {
		s.Bold = true
	}
	if r.italic[name] {
		s.Italic = true
	}
	if r.mono[name] {
		s.Code = true
	}
	return s
}

func (r *odtReader) start(t xml.StartElement) {
	if r.skipDeep > 0 {
		r.skipDeep++
		return
	}
	switch t.Name.Local {
	case "tracked-changes", "annotation", "note-citation", "bookmark", "soft-page-break":
		if t.Name.Local == "tracked-changes" || t.Name.Local == "annotation" {
			r.skipDeep = 1
		}
	case "h":
		r.begin(doc.Heading)
		r.level, _ = strconv.Atoi(attrOf(t, "outline-level"))
		if r.level == 0 {
			r.level = 1
		}
		r.style = append(r.style, r.styled(attrOf(t, "style-name")))
		r.pushLang(attrOf(t, "style-name"))
	case "p":
		if r.inCell || len(r.lists) == 0 {
			r.begin(doc.Paragraph)
		} else {
			r.begin(doc.ListItem)
		}
		name := attrOf(t, "style-name")
		r.lastCode = r.mono[name] || strings.Contains(strings.ToLower(name), "preformatted") || strings.Contains(strings.ToLower(name), "code")
		r.style = append(r.style, r.styled(name))
		r.pushLang(name)
	case "span":
		r.style = append(r.style, r.styled(attrOf(t, "style-name")))
		r.pushLang(attrOf(t, "style-name"))
	case "a":
		r.href = attrOf(t, "href")
	case "list":
		ordered := false
		if name := attrOf(t, "style-name"); name != "" {
			ordered = r.numbered[name]
		} else if n := len(r.lists); n > 0 {
			ordered = r.lists[n-1]
		}
		r.lists = append(r.lists, ordered)
		r.counts = append(r.counts, 0)
	case "list-item":
		if n := len(r.counts); n > 0 {
			r.counts[n-1]++
		}
		r.inItem = true
	case "s":
		n, _ := strconv.Atoi(attrOf(t, "c"))
		if n == 0 {
			n = 1
		}
		r.spans = append(r.spans, doc.Span{Text: strings.Repeat(" ", n)})
	case "tab":
		r.spans = append(r.spans, doc.Span{Text: "\t"})
	case "line-break":
		r.spans = append(r.spans, doc.Span{Text: "\n"})
	case "table":
		r.tblDeep++
		if r.tblDeep == 1 {
			r.table = &doc.Block{Kind: doc.Table}
		}
	case "table-row":
		if r.tblDeep == 1 {
			r.row = [][]doc.Span{}
		}
	case "table-cell", "covered-table-cell":
		if r.tblDeep == 1 {
			r.inCell = true
			r.cell = nil
		}
	case "table-header-rows":
		if r.table != nil {
			r.table.HasHeader = true
		}
	}
}

func (r *odtReader) begin(k doc.Kind) {
	r.inText = true
	r.spans = nil
	r.kind = k
}

func (r *odtReader) end(t xml.EndElement) {
	if r.skipDeep > 0 {
		r.skipDeep--
		return
	}
	switch t.Name.Local {
	case "h", "p":
		r.emit()
		if n := len(r.style); n > 0 {
			r.style = r.style[:n-1]
		}
		r.popLang()
	case "span":
		if n := len(r.style); n > 0 {
			r.style = r.style[:n-1]
		}
		r.popLang()
	case "a":
		r.href = ""
	case "list":
		if n := len(r.lists); n > 0 {
			r.lists = r.lists[:n-1]
			r.counts = r.counts[:n-1]
		}
	case "list-item":
		r.inItem = false
	case "table-cell", "covered-table-cell":
		if r.tblDeep == 1 && r.inCell {
			r.inCell = false
			r.row = append(r.row, r.cell)
			r.cell = nil
		}
	case "table-row":
		if r.tblDeep == 1 && r.row != nil {
			r.table.Rows = append(r.table.Rows, r.row)
			r.row = nil
		}
	case "table":
		r.tblDeep--
		if r.tblDeep == 0 && r.table != nil {
			if len(r.table.Rows) > 0 {
				r.d.Blocks = append(r.d.Blocks, *r.table)
			}
			r.table = nil
		}
	}
}

func (r *odtReader) emit() {
	r.inText = false
	spans := trimSpans(mergeSpans(r.spans))
	r.spans = nil
	if r.inCell {
		if len(r.cell) > 0 && len(spans) > 0 {
			r.cell = append(r.cell, doc.Span{Text: "\n"})
		}
		r.cell = append(r.cell, spans...)
		return
	}
	if len(spans) == 0 {
		return
	}
	b := doc.Block{Kind: r.kind, Spans: spans}
	switch r.kind {
	case doc.Heading:
		b.Level = r.level
	case doc.ListItem:
		b.Level = len(r.lists) - 1
		b.Ordered = r.lists[len(r.lists)-1]
		b.Number = r.counts[len(r.counts)-1]
	case doc.Paragraph:
		if r.lastCode || allCode(spans) {
			b.Kind = doc.Code
			b.Text = doc.PlainText(spans)
			b.Spans = nil
		}
	}
	r.d.Blocks = append(r.d.Blocks, b)
}

// ── ODS reader ──

// ReadODS answers the first sheet of an OpenDocument Spreadsheet as rows.
func ReadODS(data []byte) ([][]string, error) {
	p, err := openPkg(data)
	if err != nil {
		return nil, err
	}
	body, err := p.read("content.xml")
	if err != nil {
		return nil, err
	}
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	var rows [][]string
	var row []string
	var cell strings.Builder
	inCell := false
	inTable := 0
	cellRepeat := 1
	rowRepeat := 1
	cellValue := ""
	cellText := false
	inP := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("ods: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "table":
				inTable++
				if inTable > 1 {
					// only the first sheet
					if len(rows) > 0 {
						return trimEmptyRows(rows), nil
					}
				}
			case "table-row":
				if inTable == 1 {
					row = nil
					rowRepeat, _ = strconv.Atoi(attrOf(t, "number-rows-repeated"))
					if rowRepeat < 1 {
						rowRepeat = 1
					}
				}
			case "table-cell", "covered-table-cell":
				if inTable == 1 {
					inCell = true
					cell.Reset()
					cellText = false
					cellValue = attrOf(t, "value")
					if attrOf(t, "value-type") == "date" || attrOf(t, "value-type") == "time" {
						cellValue = attrOf(t, "date-value") + attrOf(t, "time-value")
					}
					cellRepeat, _ = strconv.Atoi(attrOf(t, "number-columns-repeated"))
					if cellRepeat < 1 {
						cellRepeat = 1
					}
				}
			case "p":
				if inCell {
					if cellText {
						cell.WriteString("\n")
					}
					inP = true
					cellText = true
				}
			case "s":
				if inCell && inP {
					n, _ := strconv.Atoi(attrOf(t, "c"))
					if n == 0 {
						n = 1
					}
					cell.WriteString(strings.Repeat(" ", n))
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "p":
				inP = false
			case "table-cell", "covered-table-cell":
				if inTable == 1 && inCell {
					inCell = false
					v := cell.String()
					if v == "" && cellValue != "" {
						v = cellValue
					}
					if cellRepeat > 1000 {
						cellRepeat = 1
					}
					for i := 0; i < cellRepeat; i++ {
						row = append(row, v)
					}
				}
			case "table-row":
				if inTable == 1 {
					if rowRepeat > 1000 {
						rowRepeat = 1
					}
					for i := 0; i < rowRepeat; i++ {
						rows = append(rows, append([]string(nil), row...))
					}
				}
			case "table":
				inTable--
			}
		case xml.CharData:
			if inCell && inP {
				cell.Write(t)
			}
		}
	}
	return trimEmptyRows(rows), nil
}

// trimEmptyRows drops the empty remainder of the grid (spreadsheets repeat
// it to the sheet's edge) and then pads every row to the table's width, so
// a CSV written from the rows keeps its column count.
func trimEmptyRows(rows [][]string) [][]string {
	width := 0
	for i := range rows {
		for len(rows[i]) > 0 && rows[i][len(rows[i])-1] == "" {
			rows[i] = rows[i][:len(rows[i])-1]
		}
		if len(rows[i]) > width {
			width = len(rows[i])
		}
	}
	for len(rows) > 0 && len(rows[len(rows)-1]) == 0 {
		rows = rows[:len(rows)-1]
	}
	for i := range rows {
		for len(rows[i]) < width {
			rows[i] = append(rows[i], "")
		}
	}
	return rows
}
