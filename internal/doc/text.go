package doc

import (
	"strconv"
	"strings"
)

// FromText reads plain text: blank lines separate paragraphs, single line
// breaks inside a paragraph are kept as hard breaks.
func FromText(src []byte) *Document {
	text := strings.ReplaceAll(string(src), "\r\n", "\n")
	text = strings.TrimPrefix(text, "\ufeff")
	d := &Document{}
	for _, para := range strings.Split(text, "\n\n") {
		para = strings.Trim(para, "\n")
		if strings.TrimSpace(para) == "" {
			continue
		}
		var spans []Span
		for i, line := range strings.Split(para, "\n") {
			if i > 0 {
				spans = append(spans, Span{Text: "\n"})
			}
			spans = append(spans, Span{Text: line})
		}
		d.Blocks = append(d.Blocks, Block{Kind: Paragraph, Spans: spans})
	}
	return d
}

// ToText renders blocks as plain text with a light structure: headings
// underlined, list markers, code indented, tables tab-separated.
func ToText(d *Document) []byte {
	var b strings.Builder
	for i, bl := range d.Blocks {
		if i > 0 && !(bl.Kind == ListItem && d.Blocks[i-1].Kind == ListItem) {
			b.WriteString("\n")
		}
		switch bl.Kind {
		case Heading:
			t := PlainText(bl.Spans)
			b.WriteString(t + "\n")
			under := "-"
			if bl.Level <= 1 {
				under = "="
			}
			b.WriteString(strings.Repeat(under, len([]rune(t))) + "\n")
		case Paragraph:
			b.WriteString(PlainText(bl.Spans) + "\n")
		case Quote:
			for _, line := range strings.Split(PlainText(bl.Spans), "\n") {
				b.WriteString("> " + line + "\n")
			}
		case Code:
			for _, line := range strings.Split(strings.TrimRight(bl.Text, "\n"), "\n") {
				b.WriteString("    " + line + "\n")
			}
		case ListItem:
			indent := strings.Repeat("  ", bl.Level)
			marker := "- "
			if bl.Ordered {
				marker = strconv.Itoa(max(bl.Number, 1)) + ". "
			}
			b.WriteString(indent + marker + strings.ReplaceAll(PlainText(bl.Spans), "\n", "\n"+indent+"  ") + "\n")
		case Table:
			for _, r := range bl.Rows {
				cells := make([]string, len(r))
				for j, c := range r {
					cells[j] = strings.ReplaceAll(PlainText(c), "\n", " ")
				}
				b.WriteString(strings.Join(cells, "\t") + "\n")
			}
		case Rule:
			b.WriteString("----------\n")
		}
	}
	return []byte(b.String())
}
