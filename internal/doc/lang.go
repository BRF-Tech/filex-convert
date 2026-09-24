package doc

import (
	"regexp"
	"strings"
)

// A document's language, as a BCP-47 tag ("en", "de-DE", "pt-BR").
//
// ⚠⚠ Why this exists (v0.43.0): every EPUB the converter wrote declared
// itself Turkish — `xml:lang="tr"` and `<dc:language>tr</dc:language>` were
// written literally — whatever the book was. A reader, a screen reader or an
// e-book store then hyphenates, speaks and files an English or German book
// as Turkish. The language a writer declares now comes, in this order, from:
//
//  1. the source document's own declaration — a Word/OpenDocument/PowerPoint
//     file's language settings, an EPUB's dc:language, an HTML `lang`,
//     a Markdown front matter `lang:`, an RTF \deflang — kept on
//     Document.Lang by the reader;
//  2. the language of the person who ran the conversion (the job's locale,
//     which the purego layer fills in when the source said nothing — see
//     purego.JobLocale);
//  3. `und` — BCP-47's "undetermined", the honest answer when nobody knows.
//
// Never a hard-coded language.

var langTag = regexp.MustCompile(`^[A-Za-z]{2,3}(?:-[A-Za-z0-9]{1,8})*$`)

// NormalizeLang answers a well-formed BCP-47 tag for s, or "" when s is not
// a language: empty, malformed, or one of the markers that say "no language"
// ("und", "zxx", "x-none", "none"). "en_US" and "EN-us" become "en-US"; a
// four-letter subtag is a script ("zh-Hant").
func NormalizeLang(s string) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "_", "-")
	if !langTag.MatchString(s) {
		return ""
	}
	parts := strings.Split(s, "-")
	parts[0] = strings.ToLower(parts[0])
	switch parts[0] {
	case "und", "zxx", "mis", "mul":
		return ""
	}
	for i := 1; i < len(parts); i++ {
		switch len(parts[i]) {
		case 2:
			parts[i] = strings.ToUpper(parts[i])
		case 4:
			parts[i] = strings.ToUpper(parts[i][:1]) + strings.ToLower(parts[i][1:])
		default:
			parts[i] = strings.ToLower(parts[i])
		}
	}
	return strings.Join(parts, "-")
}

// FirstLang is the first of the candidates that is a language.
func FirstLang(candidates ...string) string {
	for _, c := range candidates {
		if l := NormalizeLang(c); l != "" {
			return l
		}
	}
	return ""
}

// LangOrUnd is the document's language for a writer that must declare one:
// the language it carries, else "und".
func (d *Document) LangOrUnd() string {
	if l := NormalizeLang(d.Lang); l != "" {
		return l
	}
	return "und"
}

// HTMLLangAttr is the ` lang="…"` attribute for an HTML root element, or
// "" when the language is unknown — an HTML page without lang is honest,
// one that claims a language it does not know is not.
func HTMLLangAttr(lang string) string {
	if l := NormalizeLang(lang); l != "" {
		return ` lang="` + l + `"`
	}
	return ""
}

var (
	htmlLangAttr = regexp.MustCompile(`(?is)<html\b[^>]*?\s(?:xml:)?lang\s*=\s*["']?([A-Za-z0-9_-]+)`)
	htmlMetaLang = regexp.MustCompile(`(?is)<meta\b[^>]*http-equiv\s*=\s*["']?content-language["']?[^>]*content\s*=\s*["']?([A-Za-z0-9_-]+)`)
)

// htmlLang is what an HTML page says its language is: <html lang>,
// <html xml:lang>, or a Content-Language meta.
func htmlLang(src []byte) string {
	var cands []string
	if m := htmlLangAttr.FindSubmatch(src); m != nil {
		cands = append(cands, string(m[1]))
	}
	if m := htmlMetaLang.FindSubmatch(src); m != nil {
		cands = append(cands, string(m[1]))
	}
	return FirstLang(cands...)
}
