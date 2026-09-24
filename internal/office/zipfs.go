// Package office reads and writes the zip-of-XML document formats without
// an engine: DOCX (read and write), XLSX (read and write), EPUB (read and
// write), ODT, ODS and PPTX (read), and RTF (read). Readers produce the
// document model in internal/doc or plain rows; writers take the same.
// Everything is written from the public specifications of those formats
// (ECMA-376, OASIS ODF, EPUB 3, the RTF 1.9 reference); fidelity is
// "the text, its structure and its tables", not layout.
package office

import (
	"archive/zip"
	"bytes"
	"errors"
	"fmt"
	"io"
	"path"
	"strings"
)

// maxPart bounds one XML part read out of a package (64 MiB).
const maxPart = 64 << 20

// ErrNotPackage is answered for a file that is not a zip package.
var ErrNotPackage = errors.New("office: not a zip package")

// pkg is an opened zip package with case-preserving path lookup.
type pkg struct {
	files map[string]*zip.File
}

func openPkg(data []byte) (*pkg, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrNotPackage, err)
	}
	p := &pkg{files: map[string]*zip.File{}}
	for _, f := range zr.File {
		p.files[strings.TrimPrefix(f.Name, "/")] = f
	}
	return p, nil
}

func (p *pkg) has(name string) bool {
	_, ok := p.files[name]
	return ok
}

func (p *pkg) read(name string) ([]byte, error) {
	f, ok := p.files[name]
	if !ok {
		// case-insensitive fallback
		for k, v := range p.files {
			if strings.EqualFold(k, name) {
				f, ok = v, true
				break
			}
		}
		if !ok {
			return nil, fmt.Errorf("office: %s missing from package", name)
		}
	}
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, maxPart+1))
	if err != nil {
		return nil, err
	}
	if len(b) > maxPart {
		return nil, fmt.Errorf("office: %s exceeds %d bytes", name, maxPart)
	}
	return b, nil
}

// resolve joins a part's directory with a relative target.
func resolve(base, target string) string {
	if strings.HasPrefix(target, "/") {
		return strings.TrimPrefix(target, "/")
	}
	return path.Clean(path.Join(path.Dir(base), target))
}

// part is one file to write into a package.
type part struct {
	name  string
	data  []byte
	store bool // no compression (EPUB's mimetype)
}

func writePkg(parts []part) ([]byte, error) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, p := range parts {
		h := &zip.FileHeader{Name: p.name, Method: zip.Deflate}
		if p.store {
			h.Method = zip.Store
		}
		w, err := zw.CreateHeader(h)
		if err != nil {
			return nil, err
		}
		if _, err := w.Write(p.data); err != nil {
			return nil, err
		}
	}
	if err := zw.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// esc escapes text for an XML element or attribute value.
func esc(s string) string {
	var b strings.Builder
	for _, r := range s {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\t', '\n', '\r':
			b.WriteRune(r)
		default:
			if r < 0x20 {
				continue
			}
			b.WriteRune(r)
		}
	}
	return b.String()
}
