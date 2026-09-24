package office

import (
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/brf-tech/filex-convert/internal/doc"
)

var slideRe = regexp.MustCompile(`^ppt/slides/slide(\d+)\.xml$`)

// ReadPPTX reads every slide's text: the title placeholder becomes a
// heading, other paragraphs paragraphs or list items by their level, and
// tables tables. Slides are separated by "Slide N" headings when they have
// no title of their own.
func ReadPPTX(data []byte) (*doc.Document, error) {
	p, err := openPkg(data)
	if err != nil {
		return nil, err
	}
	type slide struct {
		n    int
		name string
	}
	var slides []slide
	for name := range p.files {
		if m := slideRe.FindStringSubmatch(name); m != nil {
			n, _ := strconv.Atoi(m[1])
			slides = append(slides, slide{n, name})
		}
	}
	if len(slides) == 0 {
		return nil, fmt.Errorf("pptx: no slides found")
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].n < slides[j].n })
	d := &doc.Document{}
	coreLang := ""
	if t, err := p.read("docProps/core.xml"); err == nil {
		d.Title = xmlText(t, "title")
		coreLang = xmlText(t, "language")
	}
	def := langSet{}
	if pres, err := p.read("ppt/presentation.xml"); err == nil {
		def = sameLang(firstAttr(pres, "defRPr", "lang"))
	}
	votes := langVotes{}
	for _, s := range slides {
		body, err := p.read(s.name)
		if err != nil {
			return nil, err
		}
		pptxLangVotes(body, def, votes)
		blocks, err := pptxSlide(body)
		if err != nil {
			return nil, err
		}
		if len(blocks) == 0 || blocks[0].Kind != doc.Heading {
			blocks = append([]doc.Block{{Kind: doc.Heading, Level: 2, Spans: []doc.Span{{Text: "Slide " + strconv.Itoa(s.n)}}}}, blocks...)
		}
		d.Blocks = append(d.Blocks, blocks...)
	}
	d.Lang = doc.FirstLang(coreLang, votes.top())
	return d, nil
}

func pptxSlide(body []byte) ([]doc.Block, error) {
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	var out []doc.Block
	var spans []doc.Span
	style := doc.Span{}
	inText := false
	isTitle := false
	inPara := false
	level := 0
	bullet := true
	var table *doc.Block
	var row [][]doc.Span
	var cell []doc.Span
	inCell := false
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("pptx: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "sp":
				isTitle = false
			case "ph":
				typ := attrOf(t, "type")
				if typ == "title" || typ == "ctrTitle" || typ == "subTitle" {
					isTitle = true
				}
			case "p":
				inPara = true
				spans = nil
				level = 0
				bullet = true
			case "pPr":
				if inPara {
					level, _ = strconv.Atoi(attrOf(t, "lvl"))
				}
			case "buNone":
				bullet = false
			case "r", "fld":
				style = doc.Span{}
			case "rPr":
				if attrOf(t, "b") == "1" {
					style.Bold = true
				}
				if attrOf(t, "i") == "1" {
					style.Italic = true
				}
				if attrOf(t, "strike") != "" && attrOf(t, "strike") != "noStrike" {
					style.Strike = true
				}
			case "hlinkClick":
				style.Href = attrOf(t, "id")
			case "t":
				inText = true
			case "br":
				spans = append(spans, doc.Span{Text: "\n"})
			case "tbl":
				table = &doc.Block{Kind: doc.Table, HasHeader: true}
			case "tr":
				if table != nil {
					row = [][]doc.Span{}
				}
			case "tc":
				if table != nil {
					inCell = true
					cell = nil
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "t":
				inText = false
			case "p":
				inPara = false
				s := trimSpans(mergeSpans(spans))
				spans = nil
				if inCell {
					if len(cell) > 0 && len(s) > 0 {
						cell = append(cell, doc.Span{Text: "\n"})
					}
					cell = append(cell, s...)
					continue
				}
				if len(s) == 0 {
					continue
				}
				switch {
				case isTitle:
					if len(out) == 0 {
						out = append(out, doc.Block{Kind: doc.Heading, Level: 2, Spans: s})
					} else {
						out = append(out, doc.Block{Kind: doc.Heading, Level: 3, Spans: s})
					}
				case bullet && (level > 0 || len(s) > 0):
					out = append(out, doc.Block{Kind: doc.ListItem, Level: level, Spans: s})
				default:
					out = append(out, doc.Block{Kind: doc.Paragraph, Spans: s})
				}
			case "tc":
				if inCell {
					inCell = false
					row = append(row, cell)
				}
			case "tr":
				if table != nil && row != nil {
					table.Rows = append(table.Rows, row)
					row = nil
				}
			case "tbl":
				if table != nil && len(table.Rows) > 0 {
					out = append(out, *table)
				}
				table = nil
			}
		case xml.CharData:
			if inText {
				s := style
				s.Text = string(t)
				if s.Href != "" {
					s.Href = ""
				}
				spans = append(spans, s)
			}
		}
	}
	// body paragraphs on a slide without bullets read better as paragraphs
	return out, nil
}
