package office

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/text/encoding/charmap"

	"github.com/brf-tech/filex-convert/internal/doc"
)

// ErrNotRTF is answered for input without the {\rtf header.
var ErrNotRTF = errors.New("office: not an RTF file")

// ReadRTF extracts paragraphs with bold/italic runs and simple tables from
// an RTF file. Destinations that carry no body text (font and colour
// tables, style sheets, info, pictures, fields' instructions) are skipped;
// \'hh escapes decode through Windows-1252 unless \ansicpg says otherwise
// (1254 = Turkish is honoured); \uN gives Unicode directly.
func ReadRTF(data []byte) (*doc.Document, error) {
	s := string(data)
	if !strings.HasPrefix(strings.TrimSpace(s), `{\rtf`) {
		return nil, ErrNotRTF
	}
	r := &rtfReader{d: &doc.Document{}, cm: charmap.Windows1252, votes: langVotes{}}
	r.st.uc = 1
	r.run(s)
	if r.table != nil {
		r.endTable()
	}
	r.endPara()
	r.d.Lang = r.votes.top()
	return r.d, nil
}

type rtfState struct {
	bold, italic, strike bool
	skip                 bool // destination whose text is ignored
	uc                   int  // bytes to skip after \uN
	inTable              bool
	// \lang / \langfe of this group (Windows language ids); 0 = the
	// document default (\deflang / \deflangfe).
	lang, langfe int
}

type rtfReader struct {
	d     *doc.Document
	cm    *charmap.Charmap
	stack []rtfState
	st    rtfState
	spans []doc.Span
	text  strings.Builder
	table *doc.Block
	row   [][]doc.Span
	// pending bytes of a multi-byte \'hh sequence in a UTF-8 code page
	skipBytes int
	// \deflang / \deflangfe and the letters counted per language (lang.go)
	deflang, deflangfe int
	votes              langVotes
}

// runLang is the language the current text is marked with.
func (r *rtfReader) runLang() langSet {
	lang, fe := r.st.lang, r.st.langfe
	if lang == 0 {
		lang = r.deflang
	}
	if fe == 0 {
		fe = r.deflangfe
	}
	return langSet{rtfLCID[lang], rtfLCID[fe], rtfLCID[lang]}
}

func (r *rtfReader) flushText() {
	if r.text.Len() == 0 {
		return
	}
	r.spans = append(r.spans, doc.Span{Text: r.text.String(), Bold: r.st.bold, Italic: r.st.italic, Strike: r.st.strike})
	r.votes.add(r.text.String(), r.runLang())
	r.text.Reset()
}

func (r *rtfReader) endPara() {
	r.flushText()
	spans := trimSpans(mergeSpans(r.spans))
	r.spans = nil
	if len(spans) == 0 {
		return
	}
	r.d.Blocks = append(r.d.Blocks, doc.Block{Kind: doc.Paragraph, Spans: spans})
}

func (r *rtfReader) endCell() {
	r.flushText()
	spans := trimSpans(mergeSpans(r.spans))
	r.spans = nil
	if r.table == nil {
		r.table = &doc.Block{Kind: doc.Table}
	}
	r.row = append(r.row, spans)
}

func (r *rtfReader) endRow() {
	if r.table == nil {
		r.table = &doc.Block{Kind: doc.Table}
	}
	if len(r.row) > 0 {
		r.table.Rows = append(r.table.Rows, r.row)
	}
	r.row = nil
}

func (r *rtfReader) endTable() {
	if r.table != nil && len(r.table.Rows) > 0 {
		r.d.Blocks = append(r.d.Blocks, *r.table)
	}
	r.table = nil
	r.row = nil
}

// skipDestinations lists control words whose whole group carries no body
// text.
var skipDestinations = map[string]bool{
	"fonttbl": true, "colortbl": true, "stylesheet": true, "info": true, "pict": true,
	"header": true, "footer": true, "headerl": true, "headerr": true, "footerl": true, "footerr": true,
	"fldinst": true, "xmlnstbl": true, "listtable": true, "listoverridetable": true, "rsidtbl": true,
	"generator": true, "themedata": true, "colorschememapping": true, "latentstyles": true, "datastore": true,
	"object": true, "pntext": true, "revtbl": true, "mmathPr": true, "background": true, "shp": true, "shpinst": true,
	"template": true, "userprops": true, "docvar": true, "ftnsep": true, "ftnsepc": true, "aftnsep": true,
}

func (r *rtfReader) run(s string) {
	i := 0
	n := len(s)
	for i < n {
		c := s[i]
		switch c {
		case '{':
			r.stack = append(r.stack, r.st)
			i++
		case '}':
			r.flushText()
			if len(r.stack) > 0 {
				r.st = r.stack[len(r.stack)-1]
				r.stack = r.stack[:len(r.stack)-1]
			}
			i++
		case '\\':
			i++
			if i >= n {
				return
			}
			d := s[i]
			switch {
			case d == '\'':
				// \'hh
				if i+2 < n {
					if b, err := strconv.ParseUint(s[i+1:i+3], 16, 8); err == nil {
						r.byteChar(byte(b))
					}
					i += 3
				} else {
					i = n
				}
			case d == '*':
				// {\*\dest ...} — an unknown destination: skip unless known text-bearing
				i++
				if i < n && s[i] == '\\' {
					j := i + 1
					for j < n && isAlpha(s[j]) {
						j++
					}
					word := s[i+1 : j]
					if word != "" && !r.st.skip {
						r.st.skip = true
					}
					i = j
					// consume an optional numeric parameter
					if i < n && (s[i] == '-' || isDigit(s[i])) {
						i++
						for i < n && isDigit(s[i]) {
							i++
						}
					}
					if i < n && s[i] == ' ' {
						i++
					}
				}
			case d == '\\' || d == '{' || d == '}':
				r.char(string(d))
				i++
			case d == '~':
				r.char(" ")
				i++
			case d == '-' || d == '_':
				r.char("-")
				i++
			case d == '\n' || d == '\r':
				r.control("par", 0, false)
				i++
			case isAlpha(d):
				j := i
				for j < n && isAlpha(s[j]) {
					j++
				}
				word := s[i:j]
				param := 0
				hasParam := false
				if j < n && (s[j] == '-' || isDigit(s[j])) {
					k := j
					if s[k] == '-' {
						k++
					}
					for k < n && isDigit(s[k]) {
						k++
					}
					param, _ = strconv.Atoi(s[j:k])
					hasParam = true
					j = k
				}
				if j < n && s[j] == ' ' {
					j++
				}
				i = j
				r.control(word, param, hasParam)
			default:
				i++
			}
		case '\r', '\n':
			i++
		default:
			r.char(string(c))
			i++
		}
	}
}

func isAlpha(c byte) bool { return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' }
func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func (r *rtfReader) char(s string) {
	if r.skipBytes > 0 {
		r.skipBytes--
		return
	}
	if r.st.skip {
		return
	}
	r.text.WriteString(s)
}

func (r *rtfReader) byteChar(b byte) {
	if r.skipBytes > 0 {
		r.skipBytes--
		return
	}
	if r.st.skip {
		return
	}
	if r.cm == nil {
		if b < 0x80 {
			r.text.WriteByte(b)
			return
		}
		r.text.WriteByte(b)
		return
	}
	ru := r.cm.DecodeByte(b)
	if ru == utf8.RuneError {
		ru = rune(b)
	}
	r.text.WriteRune(ru)
}

func (r *rtfReader) setStyle(mod func(*rtfState)) {
	r.flushText()
	mod(&r.st)
}

func (r *rtfReader) control(word string, param int, hasParam bool) {
	if skipDestinations[word] {
		r.st.skip = true
		return
	}
	if r.st.skip && word != "cell" && word != "row" {
		return
	}
	switch word {
	case "par", "sect", "page":
		if r.st.inTable {
			r.text.WriteString("\n")
			return
		}
		if r.table != nil {
			r.endTable()
		}
		r.endPara()
	case "line":
		r.char("\n")
	case "tab":
		r.char("\t")
	case "b":
		r.setStyle(func(s *rtfState) { s.bold = !hasParam || param != 0 })
	case "i":
		r.setStyle(func(s *rtfState) { s.italic = !hasParam || param != 0 })
	case "strike", "striked":
		r.setStyle(func(s *rtfState) { s.strike = !hasParam || param != 0 })
	case "plain":
		r.setStyle(func(s *rtfState) { s.bold, s.italic, s.strike, s.lang, s.langfe = false, false, false, 0, 0 })
	case "deflang":
		r.deflang = param
	case "deflangfe":
		r.deflangfe = param
	case "lang":
		r.setStyle(func(s *rtfState) { s.lang = param })
	case "langfe":
		r.setStyle(func(s *rtfState) { s.langfe = param })
	case "uc":
		r.st.uc = param
	case "u":
		if r.st.skip {
			return
		}
		ru := rune(param)
		if param < 0 {
			ru = rune(65536 + param)
		}
		r.text.WriteRune(ru)
		r.skipBytes = r.st.uc
	case "ansicpg":
		r.cm = codepage(param)
	case "trowd":
		if !r.st.inTable {
			r.endPara()
		}
		r.st.inTable = true
	case "intbl":
		r.st.inTable = true
	case "cell":
		r.endCell()
	case "row":
		r.endRow()
	case "lastrow":
	case "pard":
		r.st.inTable = false
	case "emdash":
		r.char("—")
	case "endash":
		r.char("–")
	case "lquote":
		r.char("‘")
	case "rquote":
		r.char("’")
	case "ldblquote":
		r.char("“")
	case "rdblquote":
		r.char("”")
	case "bullet":
		r.char("•")
	}
}

func codepage(n int) *charmap.Charmap {
	switch n {
	case 1250:
		return charmap.Windows1250
	case 1251:
		return charmap.Windows1251
	case 1253:
		return charmap.Windows1253
	case 1254:
		return charmap.Windows1254
	case 1255:
		return charmap.Windows1255
	case 1256:
		return charmap.Windows1256
	case 1257:
		return charmap.Windows1257
	case 1258:
		return charmap.Windows1258
	case 28591:
		return charmap.ISO8859_1
	case 28599:
		return charmap.ISO8859_9
	case 65001:
		return nil
	}
	return charmap.Windows1252
}
