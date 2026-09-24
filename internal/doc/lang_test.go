package doc

import (
	"strings"
	"testing"
)

func TestNormalizeLang(t *testing.T) {
	for in, want := range map[string]string{
		"en":          "en",
		"EN-us":       "en-US",
		"en_US":       "en-US",
		" tr-TR ":     "tr-TR",
		"zh-hant-tw":  "zh-Hant-TW",
		"az-latn-AZ":  "az-Latn-AZ",
		"es-419":      "es-419",
		"":            "",
		"und":         "",
		"zxx":         "",
		"mul":         "",
		"x-none":      "",
		"none":        "",
		"english":     "",
		"e":           "",
		"tr TR":       "",
		"tr-":         "",
		`"><script>`:  "",
		"de-DE-1996":  "de-DE-1996",
		"sr-Cyrl-RS ": "sr-Cyrl-RS",
	} {
		if got := NormalizeLang(in); got != want {
			t.Errorf("NormalizeLang(%q) = %q, want %q", in, got, want)
		}
	}
	if got := FirstLang("", "und", "??", "fr_ca", "de"); got != "fr-CA" {
		t.Errorf("FirstLang = %q", got)
	}
	if got := (&Document{}).LangOrUnd(); got != "und" {
		t.Errorf("no language must read und, got %q", got)
	}
	if got := (&Document{Lang: "pt_br"}).LangOrUnd(); got != "pt-BR" {
		t.Errorf("LangOrUnd = %q", got)
	}
	if HTMLLangAttr("") != "" || HTMLLangAttr("und") != "" || HTMLLangAttr("de") != ` lang="de"` {
		t.Error("HTMLLangAttr: a page without a known language gets no lang attribute")
	}
}

func TestHTMLDeclaresItsLanguage(t *testing.T) {
	for src, want := range map[string]string{
		`<!DOCTYPE html><html lang="de"><body><p>Hallo</p></body></html>`:                              "de",
		`<html class="x" LANG='pt-br'><p>Olá</p></html>`:                                               "pt-BR",
		`<html xmlns="http://www.w3.org/1999/xhtml" xml:lang="fr"><body><p>Bonjour</p></body></html>`:  "fr",
		`<html><head><meta http-equiv="Content-Language" content="es"></head><body>Hola</body></html>`: "es",
		`<html lang="de"><head><meta http-equiv="content-language" content="es"></head></html>`:        "de",
		`<html><body><p lang="it">Ciao</p></body></html>`:                                              "",
		`<html data-lang="it"><body>Ciao</body></html>`:                                                "",
		`<p>no html element at all</p>`:                                                                "",
		`<html lang=""><body>x</body></html>`:                                                          "",
		`<html lang="und"><body>x</body></html>`:                                                       "",
	} {
		if got := FromHTML([]byte(src)).Lang; got != want {
			t.Errorf("FromHTML(%s).Lang = %q, want %q", src, got, want)
		}
	}
}

func TestMarkdownFrontMatter(t *testing.T) {
	src := "---\ntitle: Quartalsbericht\nlang: de-de\ntags: [a, b]\n---\n# Umsatz\n\nText.\n"
	d := FromMarkdown([]byte(src))
	if d.Lang != "de-DE" || d.Title != "Quartalsbericht" {
		t.Errorf("lang %q title %q", d.Lang, d.Title)
	}
	md := string(ToMarkdown(d))
	if strings.Contains(md, "lang:") || strings.Contains(md, "title:") || strings.Contains(md, "---") {
		t.Errorf("front matter leaked into the body:\n%s", md)
	}
	if len(d.Blocks) != 2 || d.Blocks[0].Kind != Heading {
		t.Errorf("blocks %+v", d.Blocks)
	}

	// "language:" is the other common key; "..." may close the block; CRLF
	// and a byte-order mark are how Windows editors save it.
	d = FromMarkdown([]byte("\xef\xbb\xbf---\r\nlanguage: pt_BR\r\n...\r\nOlá.\r\n"))
	if d.Lang != "pt-BR" || len(d.Blocks) != 1 || d.Blocks[0].Spans[0].Text != "Olá." {
		t.Errorf("lang %q blocks %+v", d.Lang, d.Blocks)
	}

	// A document that merely OPENS with a rule is Markdown, not metadata.
	for _, src := range []string{
		"---\nJust a paragraph between rules.\n---\nMore.\n",
		"---\n- a list\n- of items\n---\n",
		"---\n\n---\n",
		"---\nno closing line: here\n",
		"Text first.\n---\nlang: de\n---\n",
	} {
		d := FromMarkdown([]byte(src))
		if d.Lang != "" || d.Title != "" {
			t.Errorf("%q read as front matter: lang %q title %q", src, d.Lang, d.Title)
		}
		if len(d.Blocks) == 0 {
			t.Errorf("%q lost its content", src)
		}
	}
	if d := FromMarkdown([]byte("---\nJust prose.\n---\n")); !strings.Contains(string(ToMarkdown(d)), "Just prose.") {
		t.Error("prose between rules was dropped")
	}

	// A key of the wrong shape does not throw the rest away.
	if d := FromMarkdown([]byte("---\ntitle: [x, y]\nlang: fr\n---\nSalut.\n")); d.Lang != "fr" {
		t.Errorf("lang %q", d.Lang)
	}
	// Without front matter, Markdown declares no language.
	if d := FromMarkdown([]byte("# Title\n\nBody.\n")); d.Lang != "" {
		t.Errorf("lang %q", d.Lang)
	}
}
