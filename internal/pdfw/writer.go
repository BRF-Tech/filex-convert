// Package pdfw is a small PDF writer with no dependency beyond the
// standard library and golang.org/x/image/font/sfnt: it lays document
// blocks (internal/doc) out on A4 pages with the embedded Go fonts, and it
// wraps raster images one per page. It writes PDF 1.7 with Flate streams,
// Type0/CIDFontType2 fonts with Identity-H encoding and a ToUnicode map, so
// the text stays searchable and copyable, including the Turkish letters the
// Go fonts carry (ı İ ş Ş ğ Ğ ü Ü ö Ö ç Ç).
package pdfw

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"strconv"
)

// file accumulates PDF objects and writes the xref table at the end.
type file struct {
	buf     bytes.Buffer
	offsets []int // 1-based object number → offset (index 0 unused)
}

func newFile() *file {
	f := &file{}
	f.buf.WriteString("%PDF-1.7\n%\xe2\xe3\xcf\xd3\n")
	f.offsets = []int{0}
	return f
}

// reserve allocates an object number to be written later with writeAt.
func (f *file) reserve() int {
	f.offsets = append(f.offsets, -1)
	return len(f.offsets) - 1
}

// add writes a whole object now and answers its number.
func (f *file) add(body string) int {
	n := f.reserve()
	f.writeAt(n, body)
	return n
}

func (f *file) writeAt(n int, body string) {
	f.offsets[n] = f.buf.Len()
	f.buf.WriteString(strconv.Itoa(n) + " 0 obj\n" + body + "\nendobj\n")
}

// stream writes a stream object with the given dictionary entries (without
// /Length) and raw bytes; deflate compresses them with FlateDecode.
func (f *file) stream(n int, dict string, data []byte, deflate bool) {
	if deflate {
		var z bytes.Buffer
		w, _ := zlib.NewWriterLevel(&z, zlib.BestCompression)
		w.Write(data)
		w.Close()
		data = z.Bytes()
		dict += " /Filter /FlateDecode"
	}
	f.offsets[n] = f.buf.Len()
	f.buf.WriteString(strconv.Itoa(n) + " 0 obj\n<<" + dict + " /Length " + strconv.Itoa(len(data)) + ">>\nstream\n")
	f.buf.Write(data)
	f.buf.WriteString("\nendstream\nendobj\n")
}

func (f *file) addStream(dict string, data []byte, deflate bool) int {
	n := f.reserve()
	f.stream(n, dict, data, deflate)
	return n
}

// finish writes the xref and trailer; root is the catalog's number.
func (f *file) finish(root int, title string) []byte {
	info := f.add("<< /Producer (filex-convert) /Title " + pdfString(title) + " >>")
	start := f.buf.Len()
	f.buf.WriteString("xref\n0 " + strconv.Itoa(len(f.offsets)) + "\n")
	f.buf.WriteString("0000000000 65535 f \n")
	for _, off := range f.offsets[1:] {
		f.buf.WriteString(fmt.Sprintf("%010d 00000 n \n", off))
	}
	f.buf.WriteString("trailer\n<< /Size " + strconv.Itoa(len(f.offsets)) + " /Root " + strconv.Itoa(root) + " 0 R /Info " + strconv.Itoa(info) + " 0 R >>\nstartxref\n" + strconv.Itoa(start) + "\n%%EOF\n")
	return f.buf.Bytes()
}

// pdfString encodes text as a UTF-16BE hex string with BOM so any Unicode
// title survives.
func pdfString(s string) string {
	var b bytes.Buffer
	b.WriteString("<FEFF")
	for _, r := range s {
		if r > 0xFFFF {
			r -= 0x10000
			b.WriteString(fmt.Sprintf("%04X%04X", 0xD800+(r>>10), 0xDC00+(r&0x3FF)))
			continue
		}
		b.WriteString(fmt.Sprintf("%04X", r))
	}
	b.WriteString(">")
	return b.String()
}

func ftoa(v float64) string {
	s := strconv.FormatFloat(v, 'f', 2, 64)
	// trim trailing zeros for compactness
	for len(s) > 1 && s[len(s)-1] == '0' && bytes.IndexByte([]byte(s), '.') >= 0 {
		s = s[:len(s)-1]
	}
	if s[len(s)-1] == '.' {
		s = s[:len(s)-1]
	}
	return s
}
