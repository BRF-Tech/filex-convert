package doc

import (
	"bytes"

	"gopkg.in/yaml.v3"
)

// FrontMatter is the part of a Markdown document's YAML front matter a
// conversion reads.
type FrontMatter struct {
	Title    string `yaml:"title"`
	Lang     string `yaml:"lang"`
	Language string `yaml:"language"`
}

// DeclaredLang is the language the front matter declares ("lang:" or
// "language:"), "" when none.
func (m FrontMatter) DeclaredLang() string { return FirstLang(m.Lang, m.Language) }

// SplitFrontMatter splits a leading YAML front matter (a `---` line, YAML,
// then a `---` or `...` line — the Jekyll/Hugo/Pandoc convention) off a
// Markdown source. It answers the parsed front matter and the body after
// it; a source without one comes back unchanged.
//
// ⚠ Only a block that parses as a YAML MAPPING counts. A document may well
// open with a thematic break (`---`) followed by prose and another `---`;
// that is Markdown, and treating it as metadata would delete the prose.
//
// ⚠ Before this, front matter was rendered as content: a horizontal rule,
// then its own keys as a setext heading ("title: Report lang: de"), and the
// Markdown → HTML page took "---" as its title.
func SplitFrontMatter(src []byte) (FrontMatter, []byte) {
	var m FrontMatter
	src = bytes.ReplaceAll(src, []byte("\r\n"), []byte("\n"))
	body := bytes.TrimPrefix(src, []byte("\xef\xbb\xbf"))
	if !bytes.HasPrefix(body, []byte("---\n")) {
		return m, src
	}
	rest := body[len("---\n"):]
	off := 0
	for off <= len(rest) {
		end := bytes.IndexByte(rest[off:], '\n')
		line := rest[off:]
		next := len(rest)
		if end >= 0 {
			line = rest[off : off+end]
			next = off + end + 1
		}
		if t := string(bytes.TrimRight(line, " \t")); t == "---" || t == "..." {
			block := rest[:off]
			var probe map[string]any
			if err := yaml.Unmarshal(block, &probe); err != nil || len(probe) == 0 {
				return FrontMatter{}, src
			}
			// A key of the wrong type (a list under "title") leaves that
			// field empty; the rest of the front matter still reads.
			_ = yaml.Unmarshal(block, &m)
			return m, rest[next:]
		}
		if end < 0 {
			break
		}
		off = next
	}
	return FrontMatter{}, src
}
