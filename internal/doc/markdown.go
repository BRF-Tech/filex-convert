package doc

import (
	"bytes"
	"fmt"
	"strconv"
	"strings"

	"github.com/yuin/goldmark"
	"github.com/yuin/goldmark/ast"
	"github.com/yuin/goldmark/extension"
	east "github.com/yuin/goldmark/extension/ast"
	"github.com/yuin/goldmark/text"
)

var mdParser = goldmark.New(goldmark.WithExtensions(extension.GFM))

// FromMarkdown parses CommonMark + GFM into blocks. A YAML front matter
// is metadata, not content: its title and language go on the document.
func FromMarkdown(src []byte) *Document {
	fm, src := SplitFrontMatter(src)
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	root := mdParser.Parser().Parse(text.NewReader(src))
	d := &Document{Title: fm.Title, Lang: fm.DeclaredLang()}
	for n := root.FirstChild(); n != nil; n = n.NextSibling() {
		d.Blocks = append(d.Blocks, mdBlock(n, src, 0)...)
	}
	return d
}

func mdBlock(n ast.Node, src []byte, depth int) []Block {
	switch v := n.(type) {
	case *ast.Heading:
		return []Block{{Kind: Heading, Level: v.Level, Spans: mdInline(v, src, Span{})}}
	case *ast.Paragraph, *ast.TextBlock:
		return []Block{{Kind: Paragraph, Spans: mdInline(n, src, Span{})}}
	case *ast.FencedCodeBlock:
		info := ""
		if v.Info != nil {
			info = string(v.Info.Segment.Value(src))
		}
		return []Block{{Kind: Code, Text: mdLines(v, src), Info: info}}
	case *ast.CodeBlock:
		return []Block{{Kind: Code, Text: mdLines(v, src)}}
	case *ast.Blockquote:
		var out []Block
		for c := v.FirstChild(); c != nil; c = c.NextSibling() {
			for _, b := range mdBlock(c, src, depth) {
				if b.Kind == Paragraph {
					b.Kind = Quote
				}
				out = append(out, b)
			}
		}
		return out
	case *ast.ThematicBreak:
		return []Block{{Kind: Rule}}
	case *ast.List:
		var out []Block
		num := v.Start
		if num == 0 {
			num = 1
		}
		for item := v.FirstChild(); item != nil; item = item.NextSibling() {
			first := true
			for c := item.FirstChild(); c != nil; c = c.NextSibling() {
				if sub, ok := c.(*ast.List); ok {
					out = append(out, mdBlock(sub, src, depth+1)...)
					continue
				}
				for _, b := range mdBlock(c, src, depth) {
					if first && b.Kind == Paragraph {
						b.Kind = ListItem
						b.Level = depth
						b.Ordered = v.IsOrdered()
						b.Number = num
						first = false
					} else if b.Kind == Paragraph {
						b.Kind = ListItem
						b.Level = depth + 1
					}
					out = append(out, b)
				}
			}
			if first {
				out = append(out, Block{Kind: ListItem, Level: depth, Ordered: v.IsOrdered(), Number: num})
			}
			num++
		}
		return out
	case *ast.HTMLBlock:
		txt := mdLines(v, src)
		if strings.TrimSpace(txt) == "" {
			return nil
		}
		return []Block{{Kind: Paragraph, Spans: []Span{{Text: strings.TrimSpace(stripTags(txt))}}}}
	case *east.Table:
		b := Block{Kind: Table}
		for r := v.FirstChild(); r != nil; r = r.NextSibling() {
			var row [][]Span
			for c := r.FirstChild(); c != nil; c = c.NextSibling() {
				row = append(row, mdInline(c, src, Span{}))
			}
			if _, isHead := r.(*east.TableHeader); isHead {
				b.HasHeader = true
				b.Rows = append([][][]Span{row}, b.Rows...)
				continue
			}
			b.Rows = append(b.Rows, row)
		}
		return []Block{b}
	}
	return nil
}

// mdLines joins a block's raw lines.
func mdLines(n ast.Node, src []byte) string {
	var b strings.Builder
	lines := n.Lines()
	for i := 0; i < lines.Len(); i++ {
		seg := lines.At(i)
		b.Write(seg.Value(src))
	}
	return b.String()
}

func mdInline(n ast.Node, src []byte, style Span) []Span {
	var out []Span
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		switch v := c.(type) {
		case *ast.Text:
			s := style
			s.Text = string(v.Segment.Value(src))
			out = append(out, s)
			if v.HardLineBreak() {
				out = append(out, Span{Text: "\n"})
			} else if v.SoftLineBreak() {
				out = append(out, Span{Text: " "})
			}
		case *ast.String:
			s := style
			s.Text = string(v.Value)
			out = append(out, s)
		case *ast.CodeSpan:
			s := style
			s.Code = true
			s.Text = mdCodeText(v, src)
			out = append(out, s)
		case *ast.Emphasis:
			s := style
			if v.Level >= 2 {
				s.Bold = true
			} else {
				s.Italic = true
			}
			out = append(out, mdInline(v, src, s)...)
		case *east.Strikethrough:
			s := style
			s.Strike = true
			out = append(out, mdInline(v, src, s)...)
		case *ast.Link:
			s := style
			s.Href = string(v.Destination)
			out = append(out, mdInline(v, src, s)...)
		case *ast.AutoLink:
			s := style
			url := string(v.URL(src))
			s.Href = url
			s.Text = string(v.Label(src))
			out = append(out, s)
		case *ast.Image:
			s := style
			alt := PlainText(mdInline(v, src, Span{}))
			if alt == "" {
				alt = string(v.Destination)
			}
			s.Text = "[" + alt + "]"
			out = append(out, s)
		case *ast.RawHTML:
			var raw strings.Builder
			for i := 0; i < v.Segments.Len(); i++ {
				seg := v.Segments.At(i)
				raw.Write(seg.Value(src))
			}
			t := strings.ToLower(raw.String())
			if strings.HasPrefix(t, "<br") {
				out = append(out, Span{Text: "\n"})
			}
		case *east.TaskCheckBox:
			s := style
			if v.IsChecked {
				s.Text = "[x] "
			} else {
				s.Text = "[ ] "
			}
			out = append(out, s)
		default:
			if c.Type() == ast.TypeInline {
				out = append(out, mdInline(c, src, style)...)
			}
		}
	}
	return mergeSpans(out)
}

func mdCodeText(n ast.Node, src []byte) string {
	var b strings.Builder
	for c := n.FirstChild(); c != nil; c = c.NextSibling() {
		if t, ok := c.(*ast.Text); ok {
			b.Write(t.Segment.Value(src))
		}
	}
	return b.String()
}

// stripTags drops HTML tags from a raw block, keeping text.
func stripTags(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == '<':
			in = true
		case r == '>':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// ToMarkdown renders blocks as GitHub-flavoured Markdown.
func ToMarkdown(d *Document) []byte {
	var b strings.Builder
	prevList := false
	for i, bl := range d.Blocks {
		isList := bl.Kind == ListItem
		if i > 0 && !(prevList && isList) {
			b.WriteString("\n")
		}
		switch bl.Kind {
		case Heading:
			b.WriteString(strings.Repeat("#", clampInt(bl.Level, 1, 6)) + " " + spansMarkdown(bl.Spans) + "\n")
		case Paragraph:
			b.WriteString(spansMarkdown(bl.Spans) + "\n")
		case Quote:
			for _, line := range strings.Split(spansMarkdown(bl.Spans), "\n") {
				b.WriteString("> " + line + "\n")
			}
		case Code:
			fence := "```"
			for strings.Contains(bl.Text, fence) {
				fence += "`"
			}
			b.WriteString(fence + bl.Info + "\n" + strings.TrimRight(bl.Text, "\n") + "\n" + fence + "\n")
		case ListItem:
			indent := strings.Repeat("  ", bl.Level)
			marker := "- "
			if bl.Ordered {
				marker = strconv.Itoa(max(bl.Number, 1)) + ". "
			}
			b.WriteString(indent + marker + strings.ReplaceAll(spansMarkdown(bl.Spans), "\n", "\n"+indent+"  ") + "\n")
		case Table:
			b.WriteString(tableMarkdown(bl))
		case Rule:
			b.WriteString("---\n")
		}
		prevList = isList
	}
	return []byte(b.String())
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

func spansMarkdown(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		t := s.Text
		if s.Code {
			t = "`" + t + "`"
		} else {
			t = escapeMarkdown(t)
			if s.Bold {
				t = "**" + t + "**"
			}
			if s.Italic {
				t = "*" + t + "*"
			}
			if s.Strike {
				t = "~~" + t + "~~"
			}
		}
		if s.Href != "" {
			t = "[" + t + "](" + s.Href + ")"
		}
		b.WriteString(t)
	}
	return strings.TrimSpace(b.String())
}

// escapeMarkdown protects the characters that would otherwise change
// meaning at the start of a line or inside a run.
func escapeMarkdown(s string) string {
	r := strings.NewReplacer("*", `\*`, "_", `\_`, "`", "\\`", "[", `\[`, "]", `\]`, "<", `\<`, "|", `\|`)
	out := r.Replace(s)
	if strings.HasPrefix(out, "#") || strings.HasPrefix(out, ">") || strings.HasPrefix(out, "-") || strings.HasPrefix(out, "+") {
		out = `\` + out
	}
	return out
}

func tableMarkdown(bl Block) string {
	if len(bl.Rows) == 0 {
		return ""
	}
	cols := 0
	for _, r := range bl.Rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	cell := func(r [][]Span, i int) string {
		if i < len(r) {
			return strings.ReplaceAll(spansMarkdown(r[i]), "\n", " ")
		}
		return ""
	}
	var b strings.Builder
	rows := bl.Rows
	if !bl.HasHeader {
		// GFM tables need a header row; use empty cells
		b.WriteString("|" + strings.Repeat(" |", cols) + "\n")
	} else {
		b.WriteString("|")
		for i := 0; i < cols; i++ {
			b.WriteString(" " + cell(rows[0], i) + " |")
		}
		b.WriteString("\n")
		rows = rows[1:]
	}
	b.WriteString("|" + strings.Repeat("---|", cols) + "\n")
	for _, r := range rows {
		b.WriteString("|")
		for i := 0; i < cols; i++ {
			b.WriteString(" " + cell(r, i) + " |")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// TableFromRows makes a table document out of string cells (CSV → Markdown).
func TableFromRows(rows [][]string, header bool) *Document {
	bl := Block{Kind: Table, HasHeader: header}
	for _, r := range rows {
		var row [][]Span
		for _, c := range r {
			row = append(row, []Span{{Text: c}})
		}
		bl.Rows = append(bl.Rows, row)
	}
	return &Document{Blocks: []Block{bl}}
}

// RowsFromDocument pulls the first table out of a document as strings.
func RowsFromDocument(d *Document) ([][]string, bool) {
	for _, bl := range d.Blocks {
		if bl.Kind != Table {
			continue
		}
		var out [][]string
		for _, r := range bl.Rows {
			var row []string
			for _, c := range r {
				row = append(row, PlainText(c))
			}
			out = append(out, row)
		}
		return out, true
	}
	return nil, false
}

// String is a debugging aid.
func (b Block) String() string {
	return fmt.Sprintf("%d:%q", b.Kind, PlainText(b.Spans))
}
