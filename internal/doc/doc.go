// Package doc is the small document model the engine-less document
// conversions share: a flat list of blocks (headings, paragraphs, lists,
// code, quotes, tables, rules) whose text is a run of styled spans.
//
// Readers turn Markdown, HTML, plain text, DOCX, ODT, PPTX, RTF and EPUB
// into blocks; writers turn blocks into Markdown, HTML, plain text, PDF,
// DOCX and EPUB. Every conversion between two document formats is one
// reader plus one writer, so a new format costs one function, not a
// matrix row. The model is deliberately poor — no floats, no footnotes,
// no nested tables — because a converter that renders the common 95 %
// legibly beats one that fails on the rest.
package doc

import "strings"

// Kind is a block's type.
type Kind int

// The block kinds.
const (
	Paragraph Kind = iota
	Heading        // Level 1..6
	Code           // Text holds the verbatim code, Info the language
	Quote          // Spans, one paragraph of quoted text
	ListItem       // Level = nesting depth (0 based), Ordered, Number
	Table          // Rows; the first row is the header when HasHeader
	Rule
)

// Span is a run of text with one style.
type Span struct {
	Text   string
	Bold   bool
	Italic bool
	Code   bool
	Strike bool
	Href   string // a link target; empty for plain text
}

// Block is one element of a document.
type Block struct {
	Kind      Kind
	Level     int // heading level; list nesting depth
	Ordered   bool
	Number    int // ordered list item number (1 based)
	Spans     []Span
	Text      string // code blocks
	Info      string // code language
	Rows      [][][]Span
	HasHeader bool
}

// Document is a title and its blocks.
type Document struct {
	Title string
	// Lang is the language the SOURCE declares (BCP-47, see lang.go), ""
	// when it declares none. A reader sets it; a writer that must name a
	// language uses LangOrUnd.
	Lang   string
	Blocks []Block
}

// PlainText joins spans without styling.
func PlainText(spans []Span) string {
	var b strings.Builder
	for _, s := range spans {
		b.WriteString(s.Text)
	}
	return b.String()
}

// TitleOf answers the document's title: the explicit one, else the first
// heading, else the first paragraph's opening words.
func (d *Document) TitleOf() string {
	if strings.TrimSpace(d.Title) != "" {
		return strings.TrimSpace(d.Title)
	}
	for _, b := range d.Blocks {
		if b.Kind == Heading {
			if t := strings.TrimSpace(PlainText(b.Spans)); t != "" {
				return clip(t, 80)
			}
		}
	}
	for _, b := range d.Blocks {
		if b.Kind == Paragraph {
			if t := strings.TrimSpace(PlainText(b.Spans)); t != "" {
				return clip(t, 80)
			}
		}
	}
	return "Document"
}

func clip(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

// para makes a paragraph block from plain text.
func para(text string) Block {
	return Block{Kind: Paragraph, Spans: []Span{{Text: text}}}
}

// mergeSpans joins adjacent spans with identical style and drops empty
// ones, so writers emit "**bold text**" instead of "**bold** **text**".
func mergeSpans(in []Span) []Span {
	var out []Span
	for _, s := range in {
		if s.Text == "" {
			continue
		}
		if n := len(out); n > 0 && sameStyle(out[n-1], s) {
			out[n-1].Text += s.Text
			continue
		}
		out = append(out, s)
	}
	return out
}

func sameStyle(a, b Span) bool {
	return a.Bold == b.Bold && a.Italic == b.Italic && a.Code == b.Code && a.Strike == b.Strike && a.Href == b.Href
}

// collapseSpace folds runs of whitespace into one space (HTML semantics).
func collapseSpace(s string) string {
	var b strings.Builder
	space := false
	for _, r := range s {
		if r == ' ' || r == '\n' || r == '\t' || r == '\r' || r == '\f' {
			space = true
			continue
		}
		if space && b.Len() > 0 {
			b.WriteByte(' ')
		}
		space = false
		b.WriteRune(r)
	}
	if space && b.Len() > 0 {
		b.WriteByte(' ')
	}
	return b.String()
}
