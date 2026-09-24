package purego

import (
	"archive/zip"
	"bytes"
	"io"
	"regexp"
	"strings"
	"testing"

	"github.com/brf-tech/filex-convert/internal/options"
)

// The language a written document declares (v0.43.0): the source's own
// declaration, else the job's locale, else "und" — never a hard-coded one.
// Until then every EPUB said `xml:lang="tr"` + `<dc:language>tr` and every
// DOCX said "tr-TR", whatever the text was.

// zipFile answers one member of a zip package.
func zipFile(t *testing.T, data []byte, name string) string {
	t.Helper()
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range zr.File {
		if f.Name == name {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			return string(b)
		}
	}
	t.Fatalf("package lacks %s", name)
	return ""
}

var (
	opfXMLLang = regexp.MustCompile(`<package [^>]*xml:lang="([^"]*)"`)
	opfDCLang  = regexp.MustCompile(`<dc:language>([^<]*)</dc:language>`)
	htmlLang   = regexp.MustCompile(`<html [^>]*\blang="([^"]*)" xml:lang="([^"]*)"`)
)

// epubLangs answers every language an EPUB declares — the package's
// xml:lang, dc:language, the chapter's and the nav's lang/xml:lang — and
// fails unless they all agree.
func epubLang(t *testing.T, epub []byte) string {
	t.Helper()
	opf := zipFile(t, epub, "OEBPS/content.opf")
	var got []string
	for _, m := range [][]string{opfXMLLang.FindStringSubmatch(opf), opfDCLang.FindStringSubmatch(opf)} {
		if m == nil {
			t.Fatalf("OPF declares no language:\n%s", opf)
		}
		got = append(got, m[1])
	}
	for _, name := range []string{"OEBPS/chapter1.xhtml", "OEBPS/nav.xhtml"} {
		m := htmlLang.FindStringSubmatch(zipFile(t, epub, name))
		if m == nil {
			t.Fatalf("%s: <html> has no lang/xml:lang", name)
		}
		got = append(got, m[1], m[2])
	}
	for _, l := range got[1:] {
		if l != got[0] {
			t.Fatalf("the EPUB disagrees with itself about its language: %v", got)
		}
	}
	return got[0]
}

func toEPUB(t *testing.T, from string, src []byte, locale string) string {
	t.Helper()
	var opts options.Options
	if locale != "" {
		opts = options.Options{JobLocale: locale}
	}
	epub, err := Convert(from, "epub", src, opts)
	if err != nil {
		t.Fatalf("%s→epub: %v", from, err)
	}
	return epubLang(t, epub)
}

const wNS = `xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"`

// docxOf builds a Word package: body paragraphs as raw XML, plus optional
// styles.xml, settings.xml and core.xml.
func docxOf(body, styles, settings, core string) []byte {
	files := map[string]string{"word/document.xml": `<w:document ` + wNS + `><w:body>` + body + `</w:body></w:document>`}
	if styles != "" {
		files["word/styles.xml"] = `<w:styles ` + wNS + `>` + styles + `</w:styles>`
	}
	if settings != "" {
		files["word/settings.xml"] = `<w:settings ` + wNS + `>` + settings + `</w:settings>`
	}
	if core != "" {
		files["docProps/core.xml"] = `<cp:coreProperties xmlns:cp="c" xmlns:dc="http://purl.org/dc/elements/1.1/">` + core + `</cp:coreProperties>`
	}
	return zipOf(files)
}

func wPara(lang, text string) string {
	rpr := ""
	if lang != "" {
		rpr = `<w:rPr><w:lang w:val="` + lang + `"/></w:rPr>`
	}
	return `<w:p><w:r>` + rpr + `<w:t>` + text + `</w:t></w:r></w:p>`
}

func wDefaults(lang string) string {
	return `<w:docDefaults><w:rPrDefault><w:rPr>` + lang + `</w:rPr></w:rPrDefault></w:docDefaults><w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:name w:val="Normal"/></w:style>`
}

const (
	turkishText = "Bu belge baştan sona Türkçe yazılmış uzun bir paragraftır."
	englishText = "This paragraph is written in English from start to end."
	germanText  = "Dieser Absatz ist von Anfang bis Ende auf Deutsch geschrieben."
)

func TestEPUBLanguageFromWord(t *testing.T) {
	cases := []struct {
		name string
		docx []byte
		want string
	}{
		{"the file's Language property wins over the text",
			docxOf(wPara("", englishText), wDefaults(`<w:lang w:val="en-US"/>`), "", `<dc:title>T</dc:title><dc:language>it-IT</dc:language>`), "it-IT"},
		{"English Word, Turkish text: the runs, not the default",
			docxOf(wPara("tr-TR", turkishText)+wPara("tr-TR", turkishText)+wPara("", "OK"), wDefaults(`<w:lang w:val="en-US" w:eastAsia="en-US" w:bidi="ar-SA"/>`), "", ""), "tr-TR"},
		{"Turkish Word, one English quotation: the default, not the first marked run",
			docxOf(wPara("en-US", "Quote.")+wPara("", turkishText), wDefaults(`<w:lang w:val="tr-TR"/>`), "", ""), "tr-TR"},
		{"Arabic text reads the complex-script (bidi) language",
			docxOf(wPara("", "هذه الوثيقة مكتوبة باللغة العربية من البداية إلى النهاية"), wDefaults(`<w:lang w:val="en-US" w:eastAsia="zh-CN" w:bidi="ar-EG"/>`), "", ""), "ar-EG"},
		{"Japanese text reads the East Asian language",
			docxOf(wPara("", "この文書は最初から最後まで日本語で書かれています"), wDefaults(`<w:lang w:val="en-US" w:eastAsia="ja-JP" w:bidi="ar-SA"/>`), "", ""), "ja-JP"},
		{"a paragraph style's language, through basedOn",
			docxOf(`<w:p><w:pPr><w:pStyle w:val="Body"/></w:pPr><w:r><w:t>`+germanText+`</w:t></w:r></w:p>`,
				wDefaults("")+`<w:style w:type="paragraph" w:styleId="Base"><w:rPr><w:lang w:val="de-DE"/></w:rPr></w:style><w:style w:type="paragraph" w:styleId="Body"><w:basedOn w:val="Base"/></w:style>`, "", ""), "de-DE"},
		{"the default paragraph style's language",
			docxOf(wPara("", germanText), `<w:style w:type="paragraph" w:default="1" w:styleId="Normal"><w:rPr><w:lang w:val="de-AT"/></w:rPr></w:style>`, "", ""), "de-AT"},
		{"nothing marked: the theme-font language",
			docxOf(wPara("", englishText), "", `<w:themeFontLang w:val="nl-NL"/>`, ""), "nl-NL"},
		{"one marked word in unmarked text names nothing",
			docxOf(wPara("fr-FR", "Oui")+wPara("", englishText), "", "", ""), "und"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := toEPUB(t, "docx", c.docx, ""); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestEPUBLanguageFromOpenDocument(t *testing.T) {
	const ns = `xmlns:office="o" xmlns:text="t" xmlns:style="s" xmlns:fo="f"`
	odt := func(content, styles, meta string) []byte {
		files := map[string]string{"content.xml": `<office:document-content ` + ns + `>` + content + `</office:document-content>`}
		if styles != "" {
			files["styles.xml"] = `<office:document-styles ` + ns + `><office:styles>` + styles + `</office:styles></office:document-styles>`
		}
		if meta != "" {
			files["meta.xml"] = `<office:document-meta xmlns:office="o" xmlns:dc="http://purl.org/dc/elements/1.1/"><office:meta>` + meta + `</office:meta></office:document-meta>`
		}
		return zipOf(files)
	}
	body := func(p string) string { return `<office:body><office:text>` + p + `</office:text></office:body>` }
	defaults := func(lang, country string) string {
		return `<style:default-style style:family="paragraph"><style:text-properties fo:language="` + lang + `" fo:country="` + country + `"/></style:default-style>`
	}
	cases := []struct {
		name string
		odt  []byte
		want string
	}{
		{"meta.xml dc:language wins", odt(body(`<text:p>`+englishText+`</text:p>`), defaults("en", "US"), `<dc:title>T</dc:title><dc:language>sv-SE</dc:language>`), "sv-SE"},
		{"the default paragraph style", odt(body(`<text:p>`+turkishText+`</text:p>`), defaults("fr", "CA"), ""), "fr-CA"},
		{"a style without a country", odt(body(`<text:p>`+turkishText+`</text:p>`), defaults("tr", "none"), ""), "tr"},
		{"spans marked German outweigh an English default",
			odt(`<office:automatic-styles><style:style style:name="T1" style:family="text"><style:text-properties fo:language="de" fo:country="DE"/></style:style></office:automatic-styles>`+
				body(`<text:p><text:span text:style-name="T1">`+germanText+`</text:span> OK</text:p>`), defaults("en", "US"), ""), "de-DE"},
		{"a paragraph style inherits its parent's language",
			odt(`<office:automatic-styles><style:style style:name="P1" style:family="paragraph" style:parent-style-name="Body"/></office:automatic-styles>`+
				body(`<text:p text:style-name="P1">`+germanText+`</text:p>`),
				`<style:style style:name="Body" style:family="paragraph"><style:text-properties fo:language="de" fo:country="CH"/></style:style>`, ""), "de-CH"},
		{"nothing declared", odt(body(`<text:p>`+englishText+`</text:p>`), "", ""), "und"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := toEPUB(t, "odt", c.odt, ""); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestEPUBLanguageFromPowerPoint(t *testing.T) {
	slide := func(lang, text string) string {
		rpr := `<a:rPr/>`
		if lang != "" {
			rpr = `<a:rPr lang="` + lang + `"/>`
		}
		return `<p:sld xmlns:a="a" xmlns:p="p"><p:cSld><p:spTree><p:sp><p:txBody><a:p><a:r>` + rpr + `<a:t>` + text + `</a:t></a:r></a:p></p:txBody></p:sp></p:spTree></p:cSld></p:sld>`
	}
	cases := []struct {
		name  string
		files map[string]string
		want  string
	}{
		{"the runs' lang", map[string]string{"ppt/slides/slide1.xml": slide("es-ES", "Esta presentación está escrita en español"), "ppt/slides/slide2.xml": slide("en-US", "Short")}, "es-ES"},
		{"core.xml wins", map[string]string{"ppt/slides/slide1.xml": slide("es-ES", "Hola a todos"), "docProps/core.xml": `<cp:coreProperties xmlns:cp="c" xmlns:dc="d"><dc:language>ca-ES</dc:language></cp:coreProperties>`}, "ca-ES"},
		{"unmarked runs take the presentation default", map[string]string{"ppt/slides/slide1.xml": slide("", "Dit is een Nederlandse presentatie"),
			"ppt/presentation.xml": `<p:presentation xmlns:a="a" xmlns:p="p"><p:defaultTextStyle><a:defPPr><a:defRPr/></a:defPPr><a:lvl1pPr><a:defRPr lang="nl-NL"/></a:lvl1pPr></p:defaultTextStyle></p:presentation>`}, "nl-NL"},
		{"nothing declared", map[string]string{"ppt/slides/slide1.xml": slide("", "No language here")}, "und"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := toEPUB(t, "pptx", zipOf(c.files), ""); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestEPUBLanguageFromRTF(t *testing.T) {
	for _, c := range []struct{ name, rtf, want string }{
		{"\\deflang", `{\rtf1\ansi\deflang1031 ` + germanText + `\par}`, "de-DE"},
		{"\\lang runs outweigh \\deflang", `{\rtf1\ansi\ansicpg1254\deflang1033{\lang1055 Bu belge baştan sona Türkçe yazılmış uzun bir paragraftır.}\par OK\par}`, "tr-TR"},
		{"\\plain returns to \\deflang", `{\rtf1\ansi\deflang1036{\lang1033 Hi\plain Ceci est un paragraphe en français, du début à la fin.}\par}`, "fr-FR"},
		{"an unknown id is not a guess", `{\rtf1\ansi\deflang9999 ` + englishText + `\par}`, "und"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := toEPUB(t, "rtf", []byte(c.rtf), ""); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
}

func TestEPUBLanguageFromWebAndMarkdown(t *testing.T) {
	for _, c := range []struct{ name, from, src, want string }{
		{"<html lang>", "html", `<html lang="de-CH"><body><h1>Titel</h1><p>Text</p></body></html>`, "de-CH"},
		{"Content-Language", "html", `<html><head><meta http-equiv="Content-Language" content="fr"></head><body><p>Texte</p></body></html>`, "fr"},
		{"front matter lang", "md", "---\ntitle: Informe\nlang: es-MX\n---\n# Ventas\n\nTexto.\n", "es-MX"},
		{"front matter language", "md", "---\nlanguage: el\n---\nΚείμενο.\n", "el"},
		{"plain text says nothing", "txt", "Just some text.", "und"},
		{"Markdown without front matter says nothing", "md", "# Title\n\nBody.\n", "und"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if got := toEPUB(t, c.from, []byte(c.src), ""); got != c.want {
				t.Errorf("got %q, want %q", got, c.want)
			}
		})
	}
	// Front matter is metadata: the book is titled by it and does not
	// carry it as text.
	epub, err := Convert("md", "epub", []byte("---\ntitle: Informe anual\nlang: es\n---\n# Ventas\n\nTexto.\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	opf, ch := zipFile(t, epub, "OEBPS/content.opf"), zipFile(t, epub, "OEBPS/chapter1.xhtml")
	if !strings.Contains(opf, "<dc:title>Informe anual</dc:title>") {
		t.Errorf("title:\n%s", opf)
	}
	if strings.Contains(ch, "lang:") || strings.Contains(ch, "<hr") {
		t.Errorf("front matter became content:\n%s", ch)
	}
}

func TestEPUBLanguageFromAnEPUB(t *testing.T) {
	book := func(opfLang, chapterLang string) []byte {
		meta := ""
		if opfLang != "" {
			meta = `<dc:language>` + opfLang + `</dc:language>`
		}
		attr := ""
		if chapterLang != "" {
			attr = ` lang="` + chapterLang + `"`
		}
		return zipOf(map[string]string{
			"META-INF/container.xml": `<container><rootfiles><rootfile full-path="c.opf"/></rootfiles></container>`,
			"c.opf":                  `<package xmlns:dc="http://purl.org/dc/elements/1.1/"><metadata><dc:title>B</dc:title>` + meta + `</metadata><manifest><item id="a" href="a.xhtml" media-type="application/xhtml+xml"/></manifest><spine><itemref idref="a"/></spine></package>`,
			"a.xhtml":                `<html` + attr + `><body><p>Κείμενο στα ελληνικά.</p></body></html>`,
		})
	}
	for _, c := range []struct{ name, opf, chapter, want string }{
		{"dc:language", "el", "", `lang="el"`},
		{"dc:language wins over the chapter", "el-GR", "en", `lang="el-GR"`},
		{"a chapter's lang when the OPF has none", "", "el", `lang="el"`},
		{"nothing declared", "", "", `<html>`},
	} {
		t.Run(c.name, func(t *testing.T) {
			out, err := Convert("epub", "html", book(c.opf, c.chapter), nil)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), c.want) {
				t.Errorf("epub→html lacks %s:\n%.200s", c.want, out)
			}
		})
	}
}

// The source says nothing → the language of the person who ran the job;
// the source says something → the source, whoever runs it.
func TestTheJobLocaleIsTheFallback(t *testing.T) {
	if got := toEPUB(t, "txt", []byte("Hallo Welt."), "de"); got != "de" {
		t.Errorf("txt with locale de: %q", got)
	}
	if got := toEPUB(t, "md", []byte("# Başlık\n\nMetin.\n"), "tr"); got != "tr" {
		t.Errorf("md with locale tr: %q", got)
	}
	if got := toEPUB(t, "docx", docxOf(wPara("", englishText), "", "", ""), "fr_FR"); got != "fr-FR" {
		t.Errorf("unmarked docx with locale fr_FR: %q", got)
	}
	if got := toEPUB(t, "html", []byte(`<html lang="en"><body><p>English.</p></body></html>`), "tr"); got != "en" {
		t.Errorf("the source's own language must win over the locale: %q", got)
	}
	if got := toEPUB(t, "txt", []byte("x"), `"><script>`); got != "und" {
		t.Errorf("a malformed locale is not a language: %q", got)
	}
}

// ⚠ The defect itself: no writer names a language nobody declared.
func TestNoWriterHardCodesALanguage(t *testing.T) {
	epub, err := Convert("md", "epub", []byte("# Report\n\nEnglish text.\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"OEBPS/content.opf", "OEBPS/chapter1.xhtml", "OEBPS/nav.xhtml"} {
		f := zipFile(t, epub, name)
		if strings.Contains(f, `"tr"`) || strings.Contains(f, ">tr<") || strings.Contains(f, "tr-TR") {
			t.Errorf("%s still declares Turkish:\n%s", name, f)
		}
		if name == "OEBPS/content.opf" && !strings.Contains(f, `xml:lang="und"`) {
			t.Errorf("unknown must be und:\n%s", f)
		}
	}

	docx, err := Convert("md", "docx", []byte("# Report\n\nEnglish text.\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if styles := zipFile(t, docx, "word/styles.xml"); strings.Contains(styles, "<w:lang") {
		t.Errorf("an unknown language must leave w:lang out (Word uses the reader's):\n%s", styles)
	}
	if core := zipFile(t, docx, "docProps/core.xml"); strings.Contains(core, "dc:language") {
		t.Errorf("core.xml names a language nobody declared:\n%s", core)
	}

	docx, err = Convert("md", "docx", []byte("---\nlang: de-DE\n---\nDeutscher Text.\n"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if styles := zipFile(t, docx, "word/styles.xml"); !strings.Contains(styles, `<w:lang w:val="de-DE" w:eastAsia="de-DE" w:bidi="de-DE"/>`) {
		t.Errorf("styles.xml:\n%s", styles)
	}
	if core := zipFile(t, docx, "docProps/core.xml"); !strings.Contains(core, "<dc:language>de-DE</dc:language>") {
		t.Errorf("core.xml:\n%s", core)
	}
	// …and it survives the round trip: the written DOCX reads back German.
	if got := toEPUB(t, "docx", docx, "tr"); got != "de-DE" {
		t.Errorf("docx round trip: %q", got)
	}
}

func TestHTMLPagesDeclareTheLanguage(t *testing.T) {
	for _, c := range []struct{ name, from, src, locale, want string }{
		{"md→html takes the front matter", "md", "---\ntitle: Bericht\nlang: de\n---\n# Umsatz\n", "tr", `<html lang="de">`},
		{"md→html falls back to the locale", "md", "# Title\n", "es", `<html lang="es">`},
		{"md→html without either", "md", "# Title\n", "", "<html>"},
		{"txt→html takes the locale", "txt", "Hello.", "fr", `<html lang="fr">`},
		{"docx→html takes the Word marking", "docx", string(docxOf(wPara("", germanText), wDefaults(`<w:lang w:val="de-DE"/>`), "", "")), "tr", `<html lang="de-DE">`},
		{"rtf→html without either", "rtf", `{\rtf1\ansi Text.\par}`, "", "<html>"},
	} {
		t.Run(c.name, func(t *testing.T) {
			var opts options.Options
			if c.locale != "" {
				opts = options.Options{JobLocale: c.locale}
			}
			out, err := Convert(c.from, "html", []byte(c.src), opts)
			if err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(string(out), c.want) {
				t.Errorf("lacks %s:\n%.300s", c.want, out)
			}
		})
	}
	// md→html: front matter is the head's title, not the body's first line.
	out, _ := Convert("md", "html", []byte("---\ntitle: Bericht\nlang: de\n---\n# Umsatz\n"), nil)
	if !strings.Contains(string(out), "<title>Bericht</title>") || strings.Contains(string(out), "lang: de") || strings.Contains(string(out), "<hr") {
		t.Errorf("md→html front matter:\n%s", out)
	}
}
