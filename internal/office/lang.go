package office

import (
	"encoding/xml"
	"sort"
	"strings"
	"unicode"

	"github.com/brf-tech/filex-convert/internal/doc"
)

// The language an office file declares (doc/lang.go says why it matters
// and in which order a writer falls back).
//
// ⚠⚠ Word, OpenDocument, PowerPoint and RTF mark language PER RUN of text
// on top of a document-wide default, and neither half alone names the
// document's language: Word on an English install writes the default
// "en-US" and marks every run typed on a Turkish keyboard "tr-TR"; Word on
// a Turkish install writes "tr-TR" once and marks only the English
// quotation. Reading just the default calls the first document English;
// reading just "the first marked run" calls the second one English too.
// So the readers resolve every letter to the language its run, its style
// or the default gives it and take the language that covers most letters.
//
// ⚠ Letters no level marks count as "unknown", and unknown wins a tie: a
// document that mostly declares nothing declares nothing, and the job's
// locale decides — a stray marked word must not name the whole book.

// langSet is a run's language per script, because the formats mark them
// separately: Latin/Cyrillic/Greek… text, East Asian text and complex-script
// (Arabic, Hebrew…) text.
type langSet struct{ latin, asian, complex string }

// or fills the scripts s leaves unmarked from base.
func (s langSet) or(base langSet) langSet {
	if s.latin == "" {
		s.latin = base.latin
	}
	if s.asian == "" {
		s.asian = base.asian
	}
	if s.complex == "" {
		s.complex = base.complex
	}
	return s
}

// sameLang marks every script with one language (PowerPoint's rPr lang).
func sameLang(l string) langSet { return langSet{l, l, l} }

// langVotes counts letters per language; "" collects the unmarked ones.
type langVotes map[string]int

// add counts text's letters toward the language their script is marked
// with. Digits, punctuation and spaces say nothing about a language.
func (v langVotes) add(text string, s langSet) {
	latin, asian, complex := doc.NormalizeLang(s.latin), doc.NormalizeLang(s.asian), doc.NormalizeLang(s.complex)
	for _, r := range text {
		switch {
		case unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana, unicode.Hangul):
			v[asian]++
		case unicode.In(r, unicode.Arabic, unicode.Hebrew, unicode.Syriac, unicode.Thaana):
			v[complex]++
		case unicode.IsLetter(r):
			v[latin]++
		}
	}
}

// top is the language that covers most letters, "" when unmarked letters
// are at least as many (or there is no text). Ties between languages go to
// the alphabetically first, so the answer never depends on map order.
func (v langVotes) top() string {
	langs := make([]string, 0, len(v))
	for l := range v {
		if l != "" {
			langs = append(langs, l)
		}
	}
	sort.Strings(langs)
	best, n := "", v[""]
	for _, l := range langs {
		if v[l] > n {
			best, n = l, v[l]
		}
	}
	return best
}

// firstAttr answers the first non-empty value of attr on an element with
// that local name (attrIn stops at the first element, even without it).
func firstAttr(data []byte, local, attr string) string {
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		tok, err := dec.Token()
		if err != nil {
			return ""
		}
		if se, ok := tok.(xml.StartElement); ok && se.Name.Local == local {
			if v := attrOf(se, attr); v != "" {
				return v
			}
		}
	}
}

// ── Word ──

// docxLangs is what styles.xml says about language: the document default
// (docDefaults), each style's own marking and its basedOn parent, and the
// paragraph style a paragraph without w:pStyle gets.
type docxLangs struct {
	def    langSet
	own    map[string]langSet
	based  map[string]string
	normal string
	cache  map[string]langSet
}

func wLang(t xml.StartElement) langSet {
	return langSet{attrOf(t, "val"), attrOf(t, "eastAsia"), attrOf(t, "bidi")}
}

func readDocxLangs(p *pkg) *docxLangs {
	l := &docxLangs{own: map[string]langSet{}, based: map[string]string{}, cache: map[string]langSet{}}
	data, err := p.read("word/styles.xml")
	if err != nil {
		return l
	}
	dec := xml.NewDecoder(strings.NewReader(string(data)))
	inDefaults, cur := false, ""
	for {
		tok, err := dec.Token()
		if err != nil {
			return l
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "rPrDefault":
				inDefaults = true
			case "style":
				cur = attrOf(t, "styleId")
				if attrOf(t, "type") == "paragraph" && (attrOf(t, "default") == "1" || attrOf(t, "default") == "true") {
					l.normal = cur
				}
			case "basedOn":
				if cur != "" {
					l.based[cur] = attrOf(t, "val")
				}
			case "lang":
				switch {
				case inDefaults:
					l.def = wLang(t)
				case cur != "":
					l.own[cur] = wLang(t)
				}
			}
		case xml.EndElement:
			switch t.Name.Local {
			case "rPrDefault":
				inDefaults = false
			case "style":
				cur = ""
			}
		}
	}
}

// para is the language a paragraph of style id gives its runs: the style's
// own marking, then its basedOn chain, then the document default.
func (l *docxLangs) para(id string) langSet {
	if id == "" {
		id = l.normal
	}
	if s, ok := l.cache[id]; ok {
		return s
	}
	s := langSet{}
	seen := map[string]bool{}
	for cur := id; cur != "" && !seen[cur]; cur = l.based[cur] {
		seen[cur] = true
		s = s.or(l.own[cur])
	}
	s = s.or(l.def)
	l.cache[id] = s
	return s
}

// ── OpenDocument ──

// odfLang reads a style:text-properties element's language per script
// (fo:language + fo:country, and the -asian and -complex variants).
func odfLang(t xml.StartElement) langSet {
	tag := func(lang, country string) string {
		lang = attrOf(t, lang)
		if doc.NormalizeLang(lang) == "" {
			return ""
		}
		if c := attrOf(t, country); c != "" && c != "none" {
			return lang + "-" + c
		}
		return lang
	}
	return langSet{
		tag("language", "country"),
		tag("language-asian", "country-asian"),
		tag("language-complex", "country-complex"),
	}
}

// ── PowerPoint ──

// pptxLangVotes counts a slide's letters toward the language each run
// declares (a:rPr lang), falling back to def (presentation.xml's default
// text style).
func pptxLangVotes(body []byte, def langSet, v langVotes) {
	dec := xml.NewDecoder(strings.NewReader(string(body)))
	cur := langSet{}
	inText := false
	for {
		tok, err := dec.Token()
		if err != nil {
			return
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "r", "fld":
				cur = langSet{}
			case "rPr":
				if l := attrOf(t, "lang"); l != "" {
					cur = sameLang(l)
				}
			case "t":
				inText = true
			}
		case xml.EndElement:
			if t.Name.Local == "t" {
				inText = false
			}
		case xml.CharData:
			if inText {
				v.add(string(t), cur.or(def))
			}
		}
	}
}

// ── RTF ──

// rtfLCID maps the Windows language ids RTF writes (\deflang, \lang,
// \langfe) to BCP-47. An id missing here reads as unmarked rather than as
// a guess; 1024 is "no language" (Word's \noproof text).
var rtfLCID = map[int]string{
	1025: "ar-SA", 1026: "bg-BG", 1027: "ca-ES", 1028: "zh-TW", 1029: "cs-CZ",
	1030: "da-DK", 1031: "de-DE", 1032: "el-GR", 1033: "en-US", 1034: "es-ES",
	1035: "fi-FI", 1036: "fr-FR", 1037: "he-IL", 1038: "hu-HU", 1040: "it-IT",
	1041: "ja-JP", 1042: "ko-KR", 1043: "nl-NL", 1044: "nb-NO", 1045: "pl-PL",
	1046: "pt-BR", 1048: "ro-RO", 1049: "ru-RU", 1050: "hr-HR", 1051: "sk-SK",
	1053: "sv-SE", 1054: "th-TH", 1055: "tr-TR", 1057: "id-ID", 1058: "uk-UA",
	1060: "sl-SI", 1061: "et-EE", 1062: "lv-LV", 1063: "lt-LT", 1065: "fa-IR",
	1066: "vi-VN", 1068: "az-Latn-AZ", 1081: "hi-IN", 1086: "ms-MY", 1087: "kk-KZ",
	2052: "zh-CN", 2055: "de-CH", 2057: "en-GB", 2058: "es-MX", 2060: "fr-BE",
	2067: "nl-BE", 2070: "pt-PT", 3076: "zh-HK", 3079: "de-AT", 3081: "en-AU",
	3082: "es-ES", 3084: "fr-CA", 4105: "en-CA", 4108: "fr-CH", 5129: "en-NZ",
	6153: "en-IE", 11274: "es-AR",
}
