package purego

import (
	"github.com/brf-tech/filex-convert/internal/doc"
	"github.com/brf-tech/filex-convert/internal/office"
	"github.com/brf-tech/filex-convert/internal/options"
	"github.com/brf-tech/filex-convert/internal/pdfw"
)

// Document conversions through the block model: every reader reaches
// every writer, so Word → PDF, EPUB → Markdown or HTML → DOCX all run in
// the sandbox without the office engine. Fidelity is "text, structure,
// tables"; the office engine (the connected ONLYOFFICE) keeps layout when it
// is there and the graph prefers it.

// DocReaders parse a document format into blocks.
var DocReaders = map[string]func([]byte) (*doc.Document, error){
	"md":   func(in []byte) (*doc.Document, error) { return doc.FromMarkdown(in), nil },
	"html": func(in []byte) (*doc.Document, error) { return doc.FromHTML(in), nil },
	"txt":  func(in []byte) (*doc.Document, error) { return doc.FromText(in), nil },
	"docx": office.ReadDOCX,
	"odt":  office.ReadODT,
	"rtf":  office.ReadRTF,
	"pptx": office.ReadPPTX,
	"epub": office.ReadEPUB,
}

// DocWriters render blocks into a document format.
var DocWriters = map[string]func(*doc.Document) ([]byte, error){
	"md":   func(d *doc.Document) ([]byte, error) { return doc.ToMarkdown(d), nil },
	"html": func(d *doc.Document) ([]byte, error) { return doc.ToHTML(d), nil },
	"txt":  func(d *doc.Document) ([]byte, error) { return doc.ToText(d), nil },
	"pdf":  pdfw.Document,
	"docx": office.WriteDOCX,
	"epub": func(d *doc.Document) ([]byte, error) { return office.WriteEPUB(d, "") },
}

// docSkip names the pairs text.go handles with a better direct path
// (goldmark's own HTML for Markdown, verbatim copies for plain text).
var docSkip = map[string]bool{"md>html": true, "txt>html": true, "html>txt": true, "txt>md": true}

func docConverter(read func([]byte) (*doc.Document, error), write func(*doc.Document) ([]byte, error)) Func {
	return func(in []byte, opts options.Options) ([]byte, error) {
		d, err := read(in)
		if err != nil {
			return nil, err
		}
		// The source's own language first; else the language of the person
		// who ran the conversion; the writer turns "neither" into "und".
		d.Lang = doc.FirstLang(d.Lang, opts.String(JobLocale))
		return write(d)
	}
}

// DocPairs lists every reader → writer pair the sandbox offers.
func DocPairs() [][2]string {
	var out [][2]string
	for from := range DocReaders {
		for to := range DocWriters {
			if from == to || docSkip[from+">"+to] {
				continue
			}
			out = append(out, [2]string{from, to})
		}
	}
	return out
}

func init() {
	for _, p := range DocPairs() {
		register(p[0], p[1], docConverter(DocReaders[p[0]], DocWriters[p[1]]))
	}
}
