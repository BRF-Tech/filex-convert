package office

import (
	"encoding/xml"
	"fmt"
	"io"
	"regexp"
	"strconv"
	"strings"

	"github.com/brf-tech/filex-convert/internal/doc"
)

// ── DOCX writer ──

const docxContentTypes = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
<Override PartName="/word/styles.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.styles+xml"/>
<Override PartName="/word/numbering.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.numbering+xml"/>
<Override PartName="/docProps/core.xml" ContentType="application/vnd.openxmlformats-package.core-properties+xml"/>
</Types>`

const docxRootRels = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/package/2006/relationships/metadata/core-properties" Target="docProps/core.xml"/>
</Relationships>`

const docxStyles = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:styles xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:docDefaults><w:rPrDefault><w:rPr><w:rFonts w:ascii="Calibri" w:hAnsi="Calibri" w:cs="Calibri"/><w:sz w:val="22"/>{{lang}}</w:rPr></w:rPrDefault><w:pPrDefault><w:pPr><w:spacing w:after="160" w:line="276" w:lineRule="auto"/></w:pPr></w:pPrDefault></w:docDefaults>
<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>
<w:style w:type="paragraph" w:styleId="Heading1"><w:name w:val="heading 1"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="360" w:after="120"/><w:outlineLvl w:val="0"/></w:pPr><w:rPr><w:b/><w:sz w:val="40"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading2"><w:name w:val="heading 2"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="280" w:after="100"/><w:outlineLvl w:val="1"/></w:pPr><w:rPr><w:b/><w:sz w:val="32"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading3"><w:name w:val="heading 3"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:spacing w:before="240" w:after="80"/><w:outlineLvl w:val="2"/></w:pPr><w:rPr><w:b/><w:sz w:val="28"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading4"><w:name w:val="heading 4"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:outlineLvl w:val="3"/></w:pPr><w:rPr><w:b/><w:sz w:val="24"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading5"><w:name w:val="heading 5"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:outlineLvl w:val="4"/></w:pPr><w:rPr><w:b/><w:i/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Heading6"><w:name w:val="heading 6"/><w:basedOn w:val="Normal"/><w:next w:val="Normal"/><w:pPr><w:keepNext/><w:outlineLvl w:val="5"/></w:pPr><w:rPr><w:i/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="ListParagraph"><w:name w:val="List Paragraph"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="40"/><w:ind w:left="720"/></w:pPr></w:style>
<w:style w:type="paragraph" w:styleId="Quote"><w:name w:val="Quote"/><w:basedOn w:val="Normal"/><w:pPr><w:ind w:left="720"/><w:pBdr><w:left w:val="single" w:sz="12" w:space="8" w:color="BFBFBF"/></w:pBdr></w:pPr><w:rPr><w:i/><w:color w:val="404040"/></w:rPr></w:style>
<w:style w:type="paragraph" w:styleId="Code"><w:name w:val="Code"/><w:basedOn w:val="Normal"/><w:pPr><w:spacing w:after="0" w:line="240" w:lineRule="auto"/><w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/></w:pPr><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:sz w:val="19"/></w:rPr></w:style>
<w:style w:type="character" w:styleId="CodeChar"><w:name w:val="Code Char"/><w:rPr><w:rFonts w:ascii="Consolas" w:hAnsi="Consolas" w:cs="Consolas"/><w:shd w:val="clear" w:color="auto" w:fill="F2F2F2"/></w:rPr></w:style>
<w:style w:type="character" w:styleId="Hyperlink"><w:name w:val="Hyperlink"/><w:rPr><w:color w:val="0563C1"/><w:u w:val="single"/></w:rPr></w:style>
<w:style w:type="table" w:styleId="TableGrid"><w:name w:val="Table Grid"/><w:tblPr><w:tblBorders><w:top w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:left w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:bottom w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:right w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:insideH w:val="single" w:sz="4" w:space="0" w:color="auto"/><w:insideV w:val="single" w:sz="4" w:space="0" w:color="auto"/></w:tblBorders><w:tblCellMar><w:left w:w="80" w:type="dxa"/><w:right w:w="80" w:type="dxa"/></w:tblCellMar></w:tblPr></w:style>
</w:styles>`

const docxNumbering = `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:numbering xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:abstractNum w:abstractNumId="0"><w:multiLevelType w:val="hybridMultilevel"/>
<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="&#8226;"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr><w:rPr><w:rFonts w:ascii="Symbol" w:hAnsi="Symbol" w:hint="default"/></w:rPr></w:lvl>
<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="&#9702;"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="1440" w:hanging="360"/></w:pPr></w:lvl>
<w:lvl w:ilvl="2"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="&#9642;"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="2160" w:hanging="360"/></w:pPr></w:lvl>
<w:lvl w:ilvl="3"><w:start w:val="1"/><w:numFmt w:val="bullet"/><w:lvlText w:val="&#8226;"/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="2880" w:hanging="360"/></w:pPr></w:lvl>
</w:abstractNum>
<w:abstractNum w:abstractNumId="1"><w:multiLevelType w:val="hybridMultilevel"/>
<w:lvl w:ilvl="0"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%%1."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="720" w:hanging="360"/></w:pPr></w:lvl>
<w:lvl w:ilvl="1"><w:start w:val="1"/><w:numFmt w:val="lowerLetter"/><w:lvlText w:val="%%2."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="1440" w:hanging="360"/></w:pPr></w:lvl>
<w:lvl w:ilvl="2"><w:start w:val="1"/><w:numFmt w:val="lowerRoman"/><w:lvlText w:val="%%3."/><w:lvlJc w:val="right"/><w:pPr><w:ind w:left="2160" w:hanging="180"/></w:pPr></w:lvl>
<w:lvl w:ilvl="3"><w:start w:val="1"/><w:numFmt w:val="decimal"/><w:lvlText w:val="%%4."/><w:lvlJc w:val="left"/><w:pPr><w:ind w:left="2880" w:hanging="360"/></w:pPr></w:lvl>
</w:abstractNum>
%s</w:numbering>`

// docxLang is the docDefaults w:lang for the document's language, or ""
// when it is unknown.
//
// ⚠⚠ Until v0.43.0 every document was written "tr-TR", so Word proofed an
// English report as Turkish. Unknown now writes NO w:lang — Word then uses
// the reader's own editing language, which is right; a made-up tag is not.
func docxLang(d *doc.Document) (styles, core string) {
	l := doc.NormalizeLang(d.Lang)
	if l == "" {
		return "", ""
	}
	return `<w:lang w:val="` + l + `" w:eastAsia="` + l + `" w:bidi="` + l + `"/>`, `<dc:language>` + l + `</dc:language>`
}

// WriteDOCX renders blocks as a Word document.
func WriteDOCX(d *doc.Document) ([]byte, error) {
	stylesLang, coreLang := docxLang(d)
	w := &docxWriter{}
	w.body.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main" xmlns:r="http://schemas.openxmlformats.org/officeDocument/2006/relationships"><w:body>`)
	prevList := -1
	for _, b := range d.Blocks {
		if b.Kind == doc.ListItem {
			if prevList < 0 {
				// every list gets its own numbering instance so numbers restart
				w.lists++
			}
			prevList = w.lists
		} else {
			prevList = -1
		}
		w.block(b)
	}
	w.body.WriteString(`<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1417" w:right="1417" w:bottom="1417" w:left="1417" w:header="708" w:footer="708" w:gutter="0"/></w:sectPr></w:body></w:document>`)

	var rels strings.Builder
	rels.WriteString(`<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/styles" Target="styles.xml"/>
<Relationship Id="rId2" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/numbering" Target="numbering.xml"/>`)
	for i, href := range w.links {
		rels.WriteString(fmt.Sprintf(`<Relationship Id="rId%d" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/hyperlink" Target="%s" TargetMode="External"/>`, 100+i, esc(href)))
	}
	rels.WriteString("</Relationships>")

	var nums strings.Builder
	for i := 1; i <= w.lists; i++ {
		// even ids bullet, odd ids decimal: numId = 2*list + ordered
		nums.WriteString(fmt.Sprintf(`<w:num w:numId="%d"><w:abstractNumId w:val="0"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num><w:num w:numId="%d"><w:abstractNumId w:val="1"/><w:lvlOverride w:ilvl="0"><w:startOverride w:val="1"/></w:lvlOverride></w:num>`, 2*i, 2*i+1))
	}
	core := `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<cp:coreProperties xmlns:cp="http://schemas.openxmlformats.org/package/2006/metadata/core-properties" xmlns:dc="http://purl.org/dc/elements/1.1/" xmlns:dcterms="http://purl.org/dc/terms/" xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"><dc:title>` + esc(d.TitleOf()) + `</dc:title>` + coreLang + `<dc:creator>filex-convert</dc:creator></cp:coreProperties>`
	return writePkg([]part{
		{name: "[Content_Types].xml", data: []byte(docxContentTypes)},
		{name: "_rels/.rels", data: []byte(docxRootRels)},
		{name: "docProps/core.xml", data: []byte(core)},
		{name: "word/document.xml", data: []byte(w.body.String())},
		{name: "word/_rels/document.xml.rels", data: []byte(rels.String())},
		{name: "word/styles.xml", data: []byte(strings.Replace(docxStyles, "{{lang}}", stylesLang, 1))},
		{name: "word/numbering.xml", data: []byte(fmt.Sprintf(docxNumbering, nums.String()))},
	})
}

type docxWriter struct {
	body  strings.Builder
	links []string
	lists int
}

func (w *docxWriter) block(b doc.Block) {
	switch b.Kind {
	case doc.Heading:
		w.para(`<w:pStyle w:val="Heading`+strconv.Itoa(clampInt(b.Level, 1, 6))+`"/>`, b.Spans)
	case doc.Paragraph:
		w.para("", b.Spans)
	case doc.Quote:
		w.para(`<w:pStyle w:val="Quote"/>`, b.Spans)
	case doc.Code:
		lines := strings.Split(strings.TrimRight(b.Text, "\n"), "\n")
		var spans []doc.Span
		for i, l := range lines {
			if i > 0 {
				spans = append(spans, doc.Span{Text: "\n"})
			}
			spans = append(spans, doc.Span{Text: l})
		}
		w.para(`<w:pStyle w:val="Code"/>`, spans)
	case doc.ListItem:
		numID := 2 * w.lists
		if b.Ordered {
			numID++
		}
		w.para(fmt.Sprintf(`<w:pStyle w:val="ListParagraph"/><w:numPr><w:ilvl w:val="%d"/><w:numId w:val="%d"/></w:numPr>`, clampInt(b.Level, 0, 3), numID), b.Spans)
	case doc.Table:
		w.table(b)
	case doc.Rule:
		w.body.WriteString(`<w:p><w:pPr><w:pBdr><w:bottom w:val="single" w:sz="6" w:space="1" w:color="A0A0A0"/></w:pBdr></w:pPr></w:p>`)
	}
}

func (w *docxWriter) para(pPr string, spans []doc.Span) {
	w.body.WriteString("<w:p>")
	if pPr != "" {
		w.body.WriteString("<w:pPr>" + pPr + "</w:pPr>")
	}
	w.runs(spans)
	w.body.WriteString("</w:p>")
}

func (w *docxWriter) runs(spans []doc.Span) {
	for _, s := range spans {
		var rPr strings.Builder
		if s.Code {
			rPr.WriteString(`<w:rStyle w:val="CodeChar"/>`)
		}
		if s.Href != "" {
			rPr.WriteString(`<w:rStyle w:val="Hyperlink"/>`)
		}
		if s.Bold {
			rPr.WriteString("<w:b/>")
		}
		if s.Italic {
			rPr.WriteString("<w:i/>")
		}
		if s.Strike {
			rPr.WriteString("<w:strike/>")
		}
		var run strings.Builder
		run.WriteString("<w:r>")
		if rPr.Len() > 0 {
			run.WriteString("<w:rPr>" + rPr.String() + "</w:rPr>")
		}
		for i, seg := range strings.Split(s.Text, "\n") {
			if i > 0 {
				run.WriteString("<w:br/>")
			}
			for j, t := range strings.Split(seg, "\t") {
				if j > 0 {
					run.WriteString("<w:tab/>")
				}
				if t != "" {
					run.WriteString(`<w:t xml:space="preserve">` + esc(t) + "</w:t>")
				}
			}
		}
		run.WriteString("</w:r>")
		if s.Href != "" {
			w.links = append(w.links, s.Href)
			w.body.WriteString(fmt.Sprintf(`<w:hyperlink r:id="rId%d">%s</w:hyperlink>`, 100+len(w.links)-1, run.String()))
			continue
		}
		w.body.WriteString(run.String())
	}
}

func (w *docxWriter) table(b doc.Block) {
	cols := 0
	for _, r := range b.Rows {
		if len(r) > cols {
			cols = len(r)
		}
	}
	if cols == 0 {
		return
	}
	w.body.WriteString(`<w:tbl><w:tblPr><w:tblStyle w:val="TableGrid"/><w:tblW w:w="0" w:type="auto"/><w:tblLook w:val="04A0"/></w:tblPr><w:tblGrid>`)
	for i := 0; i < cols; i++ {
		w.body.WriteString(fmt.Sprintf(`<w:gridCol w:w="%d"/>`, 9070/cols))
	}
	w.body.WriteString("</w:tblGrid>")
	for ri, r := range b.Rows {
		w.body.WriteString("<w:tr>")
		for i := 0; i < cols; i++ {
			w.body.WriteString("<w:tc><w:tcPr><w:tcW w:w=\"0\" w:type=\"auto\"/></w:tcPr>")
			var spans []doc.Span
			if i < len(r) {
				spans = r[i]
			}
			if ri == 0 && b.HasHeader {
				bold := make([]doc.Span, len(spans))
				for k, s := range spans {
					s.Bold = true
					bold[k] = s
				}
				spans = bold
			}
			w.para(`<w:spacing w:after="0"/>`, spans)
			w.body.WriteString("</w:tc>")
		}
		w.body.WriteString("</w:tr>")
	}
	w.body.WriteString("</w:tbl><w:p/>")
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

// ── DOCX reader ──

// ReadDOCX pulls headings, paragraphs, lists, tables, links and inline
// styles out of word/document.xml.
func ReadDOCX(data []byte) (*doc.Document, error) {
	p, err := openPkg(data)
	if err != nil {
		return nil, err
	}
	body, err := p.read("word/document.xml")
	if err != nil {
		return nil, err
	}
	rels := readRels(p, "word/_rels/document.xml.rels")
	styles := docxStyleMap(p)
	numbering := docxNumbering2(p)
	r := &docxReader{d: &doc.Document{}, rels: rels, styles: styles, numFmt: numbering, langs: readDocxLangs(p), votes: langVotes{}}
	if err := r.parse(body); err != nil {
		return nil, err
	}
	coreLang := ""
	if t, err := p.read("docProps/core.xml"); err == nil {
		r.d.Title = xmlText(t, "title")
		coreLang = xmlText(t, "language")
	}
	themeLang := ""
	if s, err := p.read("word/settings.xml"); err == nil {
		themeLang = firstAttr(s, "themeFontLang", "val")
	}
	// The file's Language property is the author saying so outright; the
	// text's own marking comes next; the theme-font language (the Word
	// install's language) is the last thing the file itself says.
	r.d.Lang = doc.FirstLang(coreLang, r.votes.top(), themeLang)
	return r.d, nil
}

// readRels maps relationship ids to targets.
func readRels(p *pkg, name string) map[string]string {
	out := map[string]string{}
	data, err := p.read(name)
	if err != nil {
		return out
	}
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == "Relationship" {
			var id, target string
			for _, a := range se.Attr {
				switch a.Name.Local {
				case "Id":
					id = a.Value
				case "Target":
					target = a.Value
				}
			}
			out[id] = target
		}
	}
	return out
}

// docxStyleMap answers styleId → outline level+1 for heading styles (from
// styles.xml), so documents whose headings are named oddly still work.
func docxStyleMap(p *pkg) map[string]int {
	out := map[string]int{}
	data, err := p.read("word/styles.xml")
	if err != nil {
		return out
	}
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	cur := ""
	name := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "style":
				cur = attrOf(t, "styleId")
				name = ""
			case "name":
				name = strings.ToLower(attrOf(t, "val"))
				if cur != "" {
					if strings.HasPrefix(name, "heading ") {
						if n, err := strconv.Atoi(strings.TrimPrefix(name, "heading ")); err == nil {
							out[cur] = n
						}
					} else if name == "title" {
						out[cur] = 1
					}
				}
			case "outlineLvl":
				if cur != "" {
					if n, err := strconv.Atoi(attrOf(t, "val")); err == nil {
						if _, has := out[cur]; !has {
							out[cur] = n + 1
						}
					}
				}
			}
		case xml.EndElement:
			if t.Name.Local == "style" {
				cur = ""
			}
		}
	}
	return out
}

// docxNumbering2 answers numId → true when the list is numbered (any
// non-bullet format at level 0).
func docxNumbering2(p *pkg) map[string]bool {
	out := map[string]bool{}
	data, err := p.read("word/numbering.xml")
	if err != nil {
		return out
	}
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	abstractFmt := map[string]bool{}
	curAbs := ""
	curLvl := ""
	numID := ""
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "abstractNum":
				curAbs = attrOf(t, "abstractNumId")
			case "lvl":
				curLvl = attrOf(t, "ilvl")
			case "numFmt":
				if curAbs != "" && curLvl == "0" {
					abstractFmt[curAbs] = attrOf(t, "val") != "bullet"
				}
			case "num":
				numID = attrOf(t, "numId")
			case "abstractNumId":
				if numID != "" {
					out[numID] = abstractFmt[attrOf(t, "val")]
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "abstractNum":
				curAbs = ""
			case "num":
				numID = ""
			}
		}
	}
	return out
}

func attrOf(se xml.StartElement, name string) string {
	for _, a := range se.Attr {
		if a.Name.Local == name {
			return a.Value
		}
	}
	return ""
}

var xmlTextRe = regexp.MustCompile(`(?s)<(?:[a-zA-Z0-9]+:)?%s[^>]*>(.*?)</`)

// xmlText answers the text of the first element with that local name.
func xmlText(data []byte, local string) string {
	re := regexp.MustCompile(strings.Replace(xmlTextRe.String(), "%s", regexp.QuoteMeta(local), 1))
	m := re.FindSubmatch(data)
	if m == nil {
		return ""
	}
	var s string
	if err := xml.Unmarshal([]byte("<x>"+string(m[1])+"</x>"), &struct {
		XMLName xml.Name `xml:"x"`
		S       *string  `xml:",chardata"`
	}{S: &s}); err != nil {
		return strings.TrimSpace(string(m[1]))
	}
	return strings.TrimSpace(s)
}

type docxReader struct {
	d      *doc.Document
	rels   map[string]string
	styles map[string]int
	numFmt map[string]bool
	langs  *docxLangs
	votes  langVotes
	// runLang is the current run's own w:lang, reset at every w:r — so the
	// paragraph mark's rPr (inside pPr, before any run) never reaches text.
	runLang langSet

	spans   []doc.Span
	style   doc.Span
	inPara  bool
	pStyle  string
	ilvl    int
	numID   string
	hasNum  bool
	href    string
	inRPr   bool
	inPPr   bool
	inText  bool
	table   *doc.Block
	row     [][]doc.Span
	cell    []doc.Span
	inCell  bool
	tblDeep int
	numbers map[string]int
}

func (r *docxReader) parse(body []byte) error {
	r.numbers = map[string]int{}
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	for {
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("docx: %w", err)
		}
		switch t := tok.(type) {
		case xml.StartElement:
			r.start(t)
		case xml.EndElement:
			r.end(t)
		case xml.CharData:
			if r.inText {
				s := r.style
				s.Text = string(t)
				s.Href = r.href
				r.spans = append(r.spans, s)
				r.votes.add(s.Text, r.runLang.or(r.langs.para(r.pStyle)))
			}
		}
	}
	return nil
}

func (r *docxReader) start(t xml.StartElement) {
	switch t.Name.Local {
	case "tbl":
		r.tblDeep++
		if r.tblDeep == 1 {
			r.table = &doc.Block{Kind: doc.Table}
		}
	case "tr":
		if r.tblDeep == 1 {
			r.row = [][]doc.Span{}
		}
	case "tc":
		if r.tblDeep == 1 {
			r.inCell = true
			r.cell = nil
		}
	case "p":
		r.inPara = true
		r.spans = nil
		r.pStyle = ""
		r.ilvl = 0
		r.numID = ""
		r.hasNum = false
	case "pPr":
		r.inPPr = true
	case "rPr":
		r.inRPr = true
	case "pStyle":
		if r.inPPr {
			r.pStyle = attrOf(t, "val")
		}
	case "numPr":
		if r.inPPr {
			r.hasNum = true
		}
	case "ilvl":
		if r.inPPr {
			r.ilvl, _ = strconv.Atoi(attrOf(t, "val"))
		}
	case "numId":
		if r.inPPr {
			r.numID = attrOf(t, "val")
		}
	case "r":
		if !r.inRPr {
			r.style = doc.Span{}
			r.runLang = langSet{}
		}
	case "lang":
		if r.inRPr {
			r.runLang = wLang(t)
		}
	case "b":
		if r.inRPr {
			r.style.Bold = attrOf(t, "val") != "0" && attrOf(t, "val") != "false"
		}
	case "i":
		if r.inRPr {
			r.style.Italic = attrOf(t, "val") != "0" && attrOf(t, "val") != "false"
		}
	case "strike", "dstrike":
		if r.inRPr {
			r.style.Strike = true
		}
	case "rStyle":
		if r.inRPr {
			v := strings.ToLower(attrOf(t, "val"))
			if strings.Contains(v, "code") || strings.Contains(v, "verbatim") {
				r.style.Code = true
			}
		}
	case "rFonts":
		if r.inRPr {
			f := strings.ToLower(attrOf(t, "ascii"))
			if strings.Contains(f, "courier") || strings.Contains(f, "consolas") || strings.Contains(f, "mono") {
				r.style.Code = true
			}
		}
	case "hyperlink":
		if id := attrOf(t, "id"); id != "" {
			r.href = r.rels[id]
		}
	case "t", "delText":
		if t.Name.Local == "t" {
			r.inText = true
		}
	case "tab":
		if r.inPara {
			r.spans = append(r.spans, doc.Span{Text: "\t"})
		}
	case "br", "cr":
		if r.inPara {
			r.spans = append(r.spans, doc.Span{Text: "\n"})
		}
	}
}

func (r *docxReader) end(t xml.EndElement) {
	switch t.Name.Local {
	case "t":
		r.inText = false
	case "pPr":
		r.inPPr = false
	case "rPr":
		r.inRPr = false
	case "hyperlink":
		r.href = ""
	case "p":
		r.inPara = false
		r.emitPara()
	case "tc":
		if r.tblDeep == 1 && r.inCell {
			r.inCell = false
			r.row = append(r.row, r.cell)
			r.cell = nil
		}
	case "tr":
		if r.tblDeep == 1 && r.row != nil {
			r.table.Rows = append(r.table.Rows, r.row)
			r.row = nil
		}
	case "tbl":
		r.tblDeep--
		if r.tblDeep == 0 && r.table != nil {
			if len(r.table.Rows) > 0 {
				r.table.HasHeader = true
				unboldHeader(r.table.Rows[0])
				r.d.Blocks = append(r.d.Blocks, *r.table)
			}
			r.table = nil
		}
	}
}

func (r *docxReader) emitPara() {
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
	b := doc.Block{Kind: doc.Paragraph, Spans: spans}
	lower := strings.ToLower(r.pStyle)
	switch {
	case r.styles[r.pStyle] > 0:
		b.Kind = doc.Heading
		b.Level = r.styles[r.pStyle]
	case strings.HasPrefix(lower, "heading"):
		b.Kind = doc.Heading
		b.Level, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(lower, "heading")))
		if b.Level == 0 {
			b.Level = 1
		}
	case lower == "title":
		b.Kind = doc.Heading
		b.Level = 1
	case r.hasNum:
		b.Kind = doc.ListItem
		b.Level = r.ilvl
		b.Ordered = r.numFmt[r.numID]
		key := r.numID + ":" + strconv.Itoa(r.ilvl)
		r.numbers[key]++
		b.Number = r.numbers[key]
	case strings.Contains(lower, "quote"):
		b.Kind = doc.Quote
	case strings.Contains(lower, "code") || strings.Contains(lower, "sourcecode") || strings.Contains(lower, "verbatim"):
		b.Kind = doc.Code
		b.Text = doc.PlainText(spans)
		b.Spans = nil
	}
	if b.Kind == doc.Paragraph && allCode(spans) {
		b.Kind = doc.Code
		b.Text = doc.PlainText(spans)
		b.Spans = nil
	}
	r.d.Blocks = append(r.d.Blocks, b)
}

func allCode(spans []doc.Span) bool {
	if len(spans) == 0 {
		return false
	}
	for _, s := range spans {
		if !s.Code {
			return false
		}
	}
	return true
}

func mergeSpans(in []doc.Span) []doc.Span {
	var out []doc.Span
	for _, s := range in {
		if s.Text == "" {
			continue
		}
		if n := len(out); n > 0 {
			p := out[n-1]
			if p.Bold == s.Bold && p.Italic == s.Italic && p.Code == s.Code && p.Strike == s.Strike && p.Href == s.Href {
				out[n-1].Text += s.Text
				continue
			}
		}
		out = append(out, s)
	}
	return out
}

func trimSpans(spans []doc.Span) []doc.Span {
	for len(spans) > 0 {
		t := strings.TrimLeft(spans[0].Text, " \n\t")
		if t == "" {
			spans = spans[1:]
			continue
		}
		spans[0].Text = t
		break
	}
	for len(spans) > 0 {
		n := len(spans) - 1
		t := strings.TrimRight(spans[n].Text, " \n\t")
		if t == "" {
			spans = spans[:n]
			continue
		}
		spans[n].Text = t
		break
	}
	return spans
}

// unboldHeader clears the bold flag on a header row whose every span is
// bold: the header is emphasised by position, and writers would otherwise
// double it ("| **a** |").
func unboldHeader(row [][]doc.Span) {
	for _, cell := range row {
		for _, s := range cell {
			if !s.Bold && s.Text != "" {
				return
			}
		}
	}
	for _, cell := range row {
		for i := range cell {
			cell[i].Bold = false
		}
	}
}
