package doc

import (
	htmlpkg "html"
	"strconv"
	"strings"
	"unicode"
)

// FromHTML reads an HTML document into blocks with a small tokenizer: no
// DOM, no CSS, just the structural tags that carry meaning for a reader
// (headings, paragraphs, lists, preformatted text, quotes, tables, rules,
// links and inline emphasis). Scripts, styles, comments and the head are
// dropped; entities are decoded; whitespace collapses as browsers do.
func FromHTML(src []byte) *Document {
	p := &htmlParser{d: &Document{Lang: htmlLang(src)}}
	p.run(string(src))
	p.flush()
	return p.d
}

type listState struct {
	ordered bool
	num     int
}

type htmlParser struct {
	d      *Document
	spans  []Span
	style  Span
	styles []Span // style stack for inline tags
	lists  []listState
	inPre  bool
	pre    strings.Builder
	quote  int
	kind   Kind
	level  int
	inItem bool
	itemNo int
	// tables
	table    *Block
	row      [][]Span
	inCell   bool
	cellHead bool
	rowHead  bool
	title    strings.Builder
	inTitle  bool
	skipTo   string
}

func (p *htmlParser) run(s string) {
	i := 0
	for i < len(s) {
		if p.skipTo != "" {
			j := strings.Index(strings.ToLower(s[i:]), p.skipTo)
			if j < 0 {
				return
			}
			i += j
			p.skipTo = ""
			continue
		}
		c := s[i]
		if c != '<' {
			j := strings.IndexByte(s[i:], '<')
			if j < 0 {
				j = len(s) - i
			}
			p.text(htmlpkg.UnescapeString(s[i : i+j]))
			i += j
			continue
		}
		if strings.HasPrefix(s[i:], "<!--") {
			j := strings.Index(s[i+4:], "-->")
			if j < 0 {
				return
			}
			i += 4 + j + 3
			continue
		}
		end := strings.IndexByte(s[i:], '>')
		if end < 0 {
			return
		}
		tag := s[i+1 : i+end]
		i += end + 1
		if strings.HasPrefix(tag, "!") || strings.HasPrefix(tag, "?") {
			continue
		}
		closing := strings.HasPrefix(tag, "/")
		tag = strings.TrimPrefix(tag, "/")
		name := tag
		attrs := ""
		if k := strings.IndexFunc(tag, func(r rune) bool { return unicode.IsSpace(r) || r == '/' }); k >= 0 {
			name = tag[:k]
			attrs = tag[k:]
		}
		name = strings.ToLower(name)
		selfClose := strings.HasSuffix(tag, "/")
		p.tag(name, attrs, closing, selfClose)
	}
}

func attr(attrs, key string) string {
	lower := strings.ToLower(attrs)
	k := strings.Index(lower, key+"=")
	if k < 0 {
		return ""
	}
	v := strings.TrimSpace(attrs[k+len(key)+1:])
	if v == "" {
		return ""
	}
	if v[0] == '"' || v[0] == '\'' {
		q := v[0]
		if e := strings.IndexByte(v[1:], q); e >= 0 {
			return htmlpkg.UnescapeString(v[1 : 1+e])
		}
		return htmlpkg.UnescapeString(v[1:])
	}
	if e := strings.IndexFunc(v, unicode.IsSpace); e >= 0 {
		v = v[:e]
	}
	return htmlpkg.UnescapeString(v)
}

func (p *htmlParser) text(t string) {
	if p.inTitle {
		p.title.WriteString(t)
		return
	}
	if p.inPre {
		p.pre.WriteString(t)
		return
	}
	if p.table != nil && !p.inCell {
		return
	}
	s := p.style
	s.Text = t
	p.spans = append(p.spans, s)
}

// flush closes the pending paragraph-like block.
func (p *htmlParser) flush() {
	spans := trimSpans(mergeSpans(collapseSpans(p.spans)))
	p.spans = nil
	if len(spans) == 0 {
		if p.kind == ListItem && p.inItem {
			p.d.Blocks = append(p.d.Blocks, Block{Kind: ListItem, Level: p.level, Ordered: p.listOrdered(), Number: p.itemNo})
			p.inItem = false
		}
		p.kind = Paragraph
		return
	}
	b := Block{Kind: p.kind, Spans: spans}
	switch p.kind {
	case Heading:
		b.Level = p.level
	case ListItem:
		b.Level = p.level
		b.Ordered = p.listOrdered()
		b.Number = p.itemNo
		p.inItem = false
	default:
		if p.quote > 0 {
			b.Kind = Quote
		} else if len(p.lists) > 0 {
			b.Kind = ListItem
			b.Level = len(p.lists)
		}
	}
	p.d.Blocks = append(p.d.Blocks, b)
	p.kind = Paragraph
}

func (p *htmlParser) listOrdered() bool {
	if len(p.lists) == 0 {
		return false
	}
	return p.lists[len(p.lists)-1].ordered
}

func collapseSpans(spans []Span) []Span {
	out := make([]Span, 0, len(spans))
	for _, s := range spans {
		if s.Text == "\n" {
			out = append(out, s)
			continue
		}
		s.Text = collapseSpace(s.Text)
		out = append(out, s)
	}
	return out
}

func trimSpans(spans []Span) []Span {
	for len(spans) > 0 {
		t := strings.TrimLeft(spans[0].Text, " \n")
		if t == "" {
			spans = spans[1:]
			continue
		}
		spans[0].Text = t
		break
	}
	for len(spans) > 0 {
		n := len(spans) - 1
		t := strings.TrimRight(spans[n].Text, " \n")
		if t == "" {
			spans = spans[:n]
			continue
		}
		spans[n].Text = t
		break
	}
	return spans
}

func (p *htmlParser) push(mod func(*Span)) {
	p.styles = append(p.styles, p.style)
	mod(&p.style)
}

func (p *htmlParser) pop() {
	if n := len(p.styles); n > 0 {
		p.style = p.styles[n-1]
		p.styles = p.styles[:n-1]
	}
}

func (p *htmlParser) tag(name, attrs string, closing, selfClose bool) {
	switch name {
	case "script", "style", "noscript", "template", "svg", "head":
		if !closing {
			p.skipTo = "</" + name
		}
		return
	case "title":
		p.inTitle = !closing
		return
	case "br":
		p.text("\n")
		return
	case "hr":
		p.flush()
		p.d.Blocks = append(p.d.Blocks, Block{Kind: Rule})
		return
	case "b", "strong":
		if closing {
			p.pop()
		} else {
			p.push(func(s *Span) { s.Bold = true })
		}
		return
	case "i", "em":
		if closing {
			p.pop()
		} else {
			p.push(func(s *Span) { s.Italic = true })
		}
		return
	case "code", "kbd", "samp", "tt":
		if p.inPre {
			return
		}
		if closing {
			p.pop()
		} else {
			p.push(func(s *Span) { s.Code = true })
		}
		return
	case "s", "del", "strike":
		if closing {
			p.pop()
		} else {
			p.push(func(s *Span) { s.Strike = true })
		}
		return
	case "a":
		if closing {
			p.pop()
		} else {
			href := attr(attrs, "href")
			p.push(func(s *Span) { s.Href = href })
		}
		return
	case "img":
		alt := attr(attrs, "alt")
		if alt == "" {
			alt = attr(attrs, "src")
		}
		if alt != "" {
			p.text("[" + alt + "]")
		}
		return
	case "pre":
		if closing {
			if p.inPre {
				p.inPre = false
				txt := strings.TrimPrefix(p.pre.String(), "\n")
				p.pre.Reset()
				p.d.Blocks = append(p.d.Blocks, Block{Kind: Code, Text: txt})
			}
			return
		}
		p.flush()
		p.inPre = true
		return
	case "h1", "h2", "h3", "h4", "h5", "h6":
		p.flush()
		if !closing {
			p.kind = Heading
			p.level, _ = strconv.Atoi(name[1:])
		}
		return
	case "p", "div", "section", "article", "header", "footer", "main", "aside", "nav", "figure", "figcaption", "dt", "dd", "address", "summary", "details":
		p.flush()
		return
	case "blockquote":
		p.flush()
		if closing {
			if p.quote > 0 {
				p.quote--
			}
		} else {
			p.quote++
		}
		return
	case "ul", "ol":
		p.flush()
		if closing {
			if len(p.lists) > 0 {
				p.lists = p.lists[:len(p.lists)-1]
			}
		} else {
			start := 1
			if s := attr(attrs, "start"); s != "" {
				if n, err := strconv.Atoi(s); err == nil {
					start = n
				}
			}
			p.lists = append(p.lists, listState{ordered: name == "ol", num: start - 1})
		}
		return
	case "li":
		p.flush()
		if closing {
			return
		}
		if len(p.lists) == 0 {
			p.lists = append(p.lists, listState{})
		}
		l := &p.lists[len(p.lists)-1]
		l.num++
		p.kind = ListItem
		p.level = len(p.lists) - 1
		p.itemNo = l.num
		p.inItem = true
		return
	case "table":
		p.flush()
		if closing {
			if p.table != nil {
				p.endRow()
				if len(p.table.Rows) > 0 {
					p.d.Blocks = append(p.d.Blocks, *p.table)
				}
				p.table = nil
			}
		} else {
			p.table = &Block{Kind: Table}
		}
		return
	case "tr":
		if p.table == nil {
			return
		}
		p.endRow()
		if !closing {
			p.row = [][]Span{}
			p.rowHead = false
		}
		return
	case "td", "th":
		if p.table == nil {
			return
		}
		if closing {
			p.endCell()
			return
		}
		p.endCell()
		p.inCell = true
		p.cellHead = name == "th"
		p.spans = nil
		return
	case "thead", "tbody", "tfoot", "caption", "colgroup", "col":
		return
	}
}

func (p *htmlParser) endCell() {
	if !p.inCell {
		return
	}
	p.inCell = false
	spans := trimSpans(mergeSpans(collapseSpans(p.spans)))
	p.spans = nil
	if p.row == nil {
		p.row = [][]Span{}
	}
	p.row = append(p.row, spans)
	if p.cellHead {
		p.rowHead = true
	}
}

func (p *htmlParser) endRow() {
	p.endCell()
	if p.row == nil {
		return
	}
	if len(p.row) > 0 {
		if p.rowHead && len(p.table.Rows) == 0 {
			p.table.HasHeader = true
		}
		p.table.Rows = append(p.table.Rows, p.row)
	}
	p.row = nil
}

// ToHTML renders blocks as a standalone page.
func ToHTML(d *Document) []byte {
	var b strings.Builder
	b.WriteString("<!DOCTYPE html>\n<html" + HTMLLangAttr(d.Lang) + ">\n<head>\n<meta charset=\"utf-8\">\n<meta name=\"viewport\" content=\"width=device-width, initial-scale=1\">\n<title>")
	b.WriteString(htmlpkg.EscapeString(d.TitleOf()))
	b.WriteString("</title>\n<style>body{max-width:48rem;margin:2rem auto;padding:0 1rem;font:16px/1.6 system-ui,sans-serif;color:#222}pre{overflow:auto;padding:.75rem;background:#f4f4f4}code{font-family:ui-monospace,monospace}table{border-collapse:collapse}td,th{border:1px solid #ccc;padding:.25rem .5rem}blockquote{border-left:3px solid #ccc;margin-left:0;padding-left:1rem;color:#555}</style>\n</head>\n<body>\n")
	b.WriteString(BodyHTML(d))
	b.WriteString("</body>\n</html>\n")
	return []byte(b.String())
}

// BodyHTML renders blocks as an HTML fragment (no html/head/body).
func BodyHTML(d *Document) string {
	var b strings.Builder
	var open []listState
	closeLists := func(to int) {
		for len(open) > to {
			l := open[len(open)-1]
			open = open[:len(open)-1]
			if l.ordered {
				b.WriteString("</ol>\n")
			} else {
				b.WriteString("</ul>\n")
			}
		}
	}
	for _, bl := range d.Blocks {
		if bl.Kind != ListItem {
			closeLists(0)
		}
		switch bl.Kind {
		case Heading:
			lv := strconv.Itoa(clampInt(bl.Level, 1, 6))
			b.WriteString("<h" + lv + ">" + spansHTML(bl.Spans) + "</h" + lv + ">\n")
		case Paragraph:
			b.WriteString("<p>" + spansHTML(bl.Spans) + "</p>\n")
		case Quote:
			b.WriteString("<blockquote><p>" + spansHTML(bl.Spans) + "</p></blockquote>\n")
		case Code:
			cls := ""
			if bl.Info != "" {
				cls = " class=\"language-" + htmlpkg.EscapeString(bl.Info) + "\""
			}
			b.WriteString("<pre><code" + cls + ">" + htmlpkg.EscapeString(bl.Text) + "</code></pre>\n")
		case ListItem:
			for len(open) > bl.Level+1 {
				closeLists(len(open) - 1)
			}
			for len(open) < bl.Level+1 {
				if bl.Ordered {
					b.WriteString("<ol>\n")
				} else {
					b.WriteString("<ul>\n")
				}
				open = append(open, listState{ordered: bl.Ordered})
			}
			b.WriteString("<li>" + spansHTML(bl.Spans) + "</li>\n")
		case Table:
			b.WriteString("<table>\n")
			for i, r := range bl.Rows {
				tag := "td"
				if i == 0 && bl.HasHeader {
					tag = "th"
				}
				b.WriteString("<tr>")
				for _, c := range r {
					b.WriteString("<" + tag + ">" + spansHTML(c) + "</" + tag + ">")
				}
				b.WriteString("</tr>\n")
			}
			b.WriteString("</table>\n")
		case Rule:
			b.WriteString("<hr>\n")
		}
	}
	closeLists(0)
	return b.String()
}

func spansHTML(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		t := strings.ReplaceAll(htmlpkg.EscapeString(s.Text), "\n", "<br>\n")
		if s.Code {
			t = "<code>" + t + "</code>"
		}
		if s.Bold {
			t = "<strong>" + t + "</strong>"
		}
		if s.Italic {
			t = "<em>" + t + "</em>"
		}
		if s.Strike {
			t = "<del>" + t + "</del>"
		}
		if s.Href != "" {
			t = "<a href=\"" + htmlpkg.EscapeString(s.Href) + "\">" + t + "</a>"
		}
		b.WriteString(t)
	}
	return b.String()
}
