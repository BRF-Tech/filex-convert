package office

import (
	"encoding/xml"
	"errors"
	"fmt"
	"path"
	"strings"

	"github.com/brf-tech/filex-convert/internal/doc"
)

// ── EPUB reader ──

// ReadEPUB follows container.xml → the OPF → the spine and reads every
// XHTML chapter in reading order into one document. The book's title comes
// from dc:title.
func ReadEPUB(data []byte) (*doc.Document, error) {
	p, err := openPkg(data)
	if err != nil {
		return nil, err
	}
	opfPath := ""
	if c, err := p.read("META-INF/container.xml"); err == nil {
		opfPath = attrIn(c, "rootfile", "full-path")
	}
	if opfPath == "" {
		for name := range p.files {
			if strings.HasSuffix(strings.ToLower(name), ".opf") {
				opfPath = name
				break
			}
		}
	}
	if opfPath == "" {
		return nil, errors.New("epub: no package document (OPF) found")
	}
	opf, err := p.read(opfPath)
	if err != nil {
		return nil, err
	}
	items := map[string]string{} // id → href
	var spine []string
	title, lang := "", ""
	dec := xml.NewDecoder(strings.NewReader(string(opf)))
	inTitle, inLang := false, false
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "item":
				mt := attrOf(t, "media-type")
				if strings.Contains(mt, "html") || strings.Contains(mt, "xml") {
					items[attrOf(t, "id")] = attrOf(t, "href")
				}
			case "itemref":
				spine = append(spine, attrOf(t, "idref"))
			case "title":
				inTitle = title == ""
			case "language":
				inLang = lang == ""
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "title":
				inTitle = false
			case "language":
				inLang = false
			}
		case xml.CharData:
			if inTitle {
				title += string(t)
			}
			if inLang {
				lang += string(t)
			}
		}
	}
	// dc:language is required by EPUB; a book without one still has
	// chapters that may say it (<html lang>), the first that does wins.
	d := &doc.Document{Title: strings.TrimSpace(title), Lang: doc.NormalizeLang(lang)}
	n := 0
	for _, id := range spine {
		href, ok := items[id]
		if !ok {
			continue
		}
		href = strings.SplitN(href, "#", 2)[0]
		body, err := p.read(resolve(opfPath, unescapeHref(href)))
		if err != nil {
			continue
		}
		ch := doc.FromHTML(body)
		d.Blocks = append(d.Blocks, ch.Blocks...)
		if d.Lang == "" {
			d.Lang = ch.Lang
		}
		n++
	}
	if n == 0 {
		return nil, errors.New("epub: no readable chapters in the spine")
	}
	return d, nil
}

func unescapeHref(h string) string {
	return strings.NewReplacer("%20", " ", "%23", "#", "%26", "&").Replace(h)
}

// attrIn answers one attribute of the first element with that local name.
func attrIn(data []byte, local, attr string) string {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == local {
			return attrOf(se, attr)
		}
	}
}

// ── EPUB writer ──

const epubContainer = `<?xml version="1.0" encoding="UTF-8"?>
<container version="1.0" xmlns="urn:oasis:names:tc:opendocument:xmlns:container">
<rootfiles><rootfile full-path="OEBPS/content.opf" media-type="application/oebps-package+xml"/></rootfiles>
</container>`

const epubCSS = `body{font-family:serif;line-height:1.5;margin:1em}h1,h2,h3{font-family:sans-serif}pre{white-space:pre-wrap;background:#f4f4f4;padding:.5em;font-size:.9em}code{font-family:monospace}table{border-collapse:collapse}td,th{border:1px solid #999;padding:.2em .5em}blockquote{border-left:3px solid #999;margin-left:0;padding-left:1em;color:#444}`

// WriteEPUB packs the document as one-chapter EPUB 3 (with an EPUB 2
// NCX for older readers). Chapters split at level-1 headings when there
// are several, so the table of contents is useful.
//
// ⚠⚠ The book's language is the document's (doc.Document.Lang: the source's
// own declaration, else the job's locale — see doc/lang.go), "und" when
// neither is known. Until v0.43.0 every book was written as Turkish.
func WriteEPUB(d *doc.Document, identifier string) ([]byte, error) {
	title := d.TitleOf()
	lang := d.LangOrUnd()
	htmlOpen := `<html xmlns="http://www.w3.org/1999/xhtml" xmlns:epub="http://www.idpf.org/2007/ops" lang="` + lang + `" xml:lang="` + lang + `">`
	chapters := splitChapters(d)
	if identifier == "" {
		identifier = "urn:filex-convert:" + sanitizeID(title)
	}
	parts := []part{
		{name: "mimetype", data: []byte("application/epub+zip"), store: true},
		{name: "META-INF/container.xml", data: []byte(epubContainer)},
		{name: "OEBPS/style.css", data: []byte(epubCSS)},
	}
	var manifest, spine, navList, ncxPoints strings.Builder
	for i, ch := range chapters {
		name := fmt.Sprintf("chapter%d.xhtml", i+1)
		chTitle := ch.TitleOf()
		var b strings.Builder
		b.WriteString(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
` + htmlOpen + `<head><meta charset="utf-8"/><title>`)
		b.WriteString(esc(chTitle))
		b.WriteString(`</title><link rel="stylesheet" type="text/css" href="style.css"/></head><body>`)
		b.WriteString(xhtmlBody(ch))
		b.WriteString("</body></html>")
		parts = append(parts, part{name: "OEBPS/" + name, data: []byte(b.String())})
		manifest.WriteString(fmt.Sprintf(`<item id="ch%d" href="%s" media-type="application/xhtml+xml"/>`, i+1, name))
		spine.WriteString(fmt.Sprintf(`<itemref idref="ch%d"/>`, i+1))
		navList.WriteString(fmt.Sprintf(`<li><a href="%s">%s</a></li>`, name, esc(chTitle)))
		ncxPoints.WriteString(fmt.Sprintf(`<navPoint id="np%d" playOrder="%d"><navLabel><text>%s</text></navLabel><content src="%s"/></navPoint>`, i+1, i+1, esc(chTitle), name))
	}
	nav := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE html>
` + htmlOpen + `<head><meta charset="utf-8"/><title>` + esc(title) + `</title></head><body><nav epub:type="toc" id="toc"><h1>` + esc(title) + `</h1><ol>` + navList.String() + `</ol></nav></body></html>`
	ncx := `<?xml version="1.0" encoding="UTF-8"?>
<ncx xmlns="http://www.daisy.org/z3986/2005/ncx/" version="2005-1"><head><meta name="dtb:uid" content="` + esc(identifier) + `"/><meta name="dtb:depth" content="1"/></head><docTitle><text>` + esc(title) + `</text></docTitle><navMap>` + ncxPoints.String() + `</navMap></ncx>`
	opf := `<?xml version="1.0" encoding="UTF-8"?>
<package xmlns="http://www.idpf.org/2007/opf" version="3.0" unique-identifier="uid" xml:lang="` + lang + `">
<metadata xmlns:dc="http://purl.org/dc/elements/1.1/"><dc:identifier id="uid">` + esc(identifier) + `</dc:identifier><dc:title>` + esc(title) + `</dc:title><dc:language>` + lang + `</dc:language><meta property="dcterms:modified">2026-01-01T00:00:00Z</meta></metadata>
<manifest><item id="nav" href="nav.xhtml" media-type="application/xhtml+xml" properties="nav"/><item id="ncx" href="toc.ncx" media-type="application/x-dtbncx+xml"/><item id="css" href="style.css" media-type="text/css"/>` + manifest.String() + `</manifest>
<spine toc="ncx">` + spine.String() + `</spine>
</package>`
	parts = append(parts,
		part{name: "OEBPS/content.opf", data: []byte(opf)},
		part{name: "OEBPS/nav.xhtml", data: []byte(nav)},
		part{name: "OEBPS/toc.ncx", data: []byte(ncx)},
	)
	return writePkg(parts)
}

// splitChapters cuts at level-1 headings when the document has more than
// one; otherwise the whole document is one chapter.
func splitChapters(d *doc.Document) []*doc.Document {
	h1 := 0
	for _, b := range d.Blocks {
		if b.Kind == doc.Heading && b.Level == 1 {
			h1++
		}
	}
	if h1 < 2 {
		return []*doc.Document{d}
	}
	var out []*doc.Document
	var cur *doc.Document
	for _, b := range d.Blocks {
		if b.Kind == doc.Heading && b.Level == 1 || cur == nil {
			cur = &doc.Document{}
			out = append(out, cur)
		}
		cur.Blocks = append(cur.Blocks, b)
	}
	return out
}

// xhtmlBody renders a fragment that is well-formed XML (EPUB needs XHTML):
// <br> and <hr> become self-closing.
func xhtmlBody(d *doc.Document) string {
	s := doc.BodyHTML(d)
	s = strings.ReplaceAll(s, "<br>", "<br/>")
	s = strings.ReplaceAll(s, "<hr>", "<hr/>")
	return s
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
		} else if b.Len() > 0 && b.String()[b.Len()-1] != '-' {
			b.WriteByte('-')
		}
	}
	id := strings.Trim(b.String(), "-")
	if id == "" {
		id = "book"
	}
	return path.Base(id)
}
