// Package i18n holds every word the converter shows a person, in every
// language it ships: English, Turkish, Spanish, German and French.
//
// # Why one catalogue
//
// The converter used to carry its text as English/Turkish pairs written
// inline wherever a screen, a knob or a job message was built — `text("Next",
// "İleri")`, a `LabelTR` on a format, `pick(locale, "all", "tümü")`. Adding a
// third language that way means touching every one of those sites and
// missing some, and nothing can tell which. The owner's decision (v0.43.0):
// the apps ship es, de and fr as well — so the words moved here, one key per
// sentence, one JSON file per language (catalogue/<lang>.json), and a test
// (i18n_test.go) refuses a catalogue with a missing key, an empty value or a
// placeholder that differs from the English one.
//
// # How a message is written
//
// A value may hold named placeholders — `{n} files: {list}` — filled by T and
// S from name/value pairs. Named, not positional: a translation may put them
// in any order ("{min} ile {max} arasında olmalı").
//
// Terminology and register follow the filex language packs' glossaries
// (github.com/brf-tech/filex-lang-es|de|fr): *usted*, *Sie*, *vous*; "convert"
// is *convertir* / *konvertieren* / *convertir*; an engine is *motor* /
// *Engine* / *moteur*. French carries its typography (a no-break space before
// `:`, a narrow one before `;` `?` `!`, the typographic apostrophe).
//
// ⚠ Format names that are names (PDF, JPEG, MP4, Word) are not in here at
// all: they read the same in every language. Only a format label that
// DESCRIBES ("Plain text (.txt)") has a `format.<id>` key.
package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"
)

// Langs are the languages the converter ships, English first: it is what
// every other language falls back to, and the manifest's `languages` list.
var Langs = []string{"en", "tr", "es", "de", "fr"}

//go:embed catalogue/*.json
var files embed.FS

// catalogue is lang → key → text.
var catalogue = map[string]map[string]string{}

func init() {
	for _, lang := range Langs {
		raw, err := files.ReadFile("catalogue/" + lang + ".json")
		if err != nil {
			panic("i18n: catalogue/" + lang + ".json missing: " + err.Error())
		}
		m := map[string]string{}
		if err := json.Unmarshal(raw, &m); err != nil {
			panic("i18n: catalogue/" + lang + ".json is not valid: " + err.Error())
		}
		catalogue[lang] = m
	}
}

// Norm maps a locale as a host sends it ("de-DE", "tr_TR", "FR") to one of
// Langs; anything else is English.
//
// ⚠ Until the host stops collapsing every non-Turkish language to `en`
// before calling apps (filex feat/043-srvtext), a Spanish, German or French
// reader still arrives here as `en`. The catalogues are complete regardless;
// the tests call with the locale set directly.
func Norm(locale string) string {
	l := strings.ToLower(strings.TrimSpace(locale))
	if i := strings.IndexAny(l, "-_"); i > 0 {
		l = l[:i]
	}
	for _, x := range Langs {
		if x == l {
			return x
		}
	}
	return "en"
}

// Has reports whether key is in the catalogue.
func Has(key string) bool {
	_, ok := catalogue["en"][key]
	return ok
}

// S is one message in one language, placeholders filled from name/value
// pairs. A value may be a string, a number, or a wire.Text (whose text in
// that language is used). An unknown key answers the key itself, loudly
// wrong on screen rather than silently blank — and the tests refuse it.
func S(locale, key string, args ...any) string {
	lang := Norm(locale)
	s, ok := catalogue[lang][key]
	if !ok || s == "" {
		s, ok = catalogue["en"][key]
	}
	if !ok {
		return key
	}
	return fill(s, lang, args)
}

// T is a message in every language — what a surface, a manifest-shaped
// label or a job message carries.
func T(key string, args ...any) wire.Text {
	out := make(wire.Text, len(Langs))
	for _, lang := range Langs {
		out[lang] = S(lang, key, args...)
	}
	return out
}

// M is T as a plain map, for the knob catalogue's map-typed labels.
func M(key string, args ...any) map[string]string { return map[string]string(T(key, args...)) }

// Each builds a Text by asking for every language in turn — for words put
// together from parts that are themselves translated (a route line).
func Each(f func(lang string) string) wire.Text {
	out := make(wire.Text, len(Langs))
	for _, lang := range Langs {
		out[lang] = f(lang)
	}
	return out
}

// Same is words that read the same in every language (a file name, a
// number). It still carries every language: a Text missing one is what an
// install check refuses.
func Same(s string) wire.Text {
	return Each(func(string) string { return s })
}

// Pick is t in locale, falling back to English.
func Pick(locale string, t map[string]string) string {
	if s := t[Norm(locale)]; s != "" {
		return s
	}
	return t["en"]
}

// Join joins per-language texts with sep, language by language.
func Join(parts []wire.Text, sep string) wire.Text {
	return Each(func(lang string) string {
		ss := make([]string, 0, len(parts))
		for _, p := range parts {
			ss = append(ss, Pick(lang, p))
		}
		return strings.Join(ss, sep)
	})
}

var placeholder = regexp.MustCompile(`\{[a-z_]+\}`)

func fill(s, lang string, args []any) string {
	if len(args) == 0 {
		return s
	}
	vals := map[string]string{}
	for i := 0; i+1 < len(args); i += 2 {
		name, _ := args[i].(string)
		vals[name] = valueIn(args[i+1], lang)
	}
	return placeholder.ReplaceAllStringFunc(s, func(p string) string {
		if v, ok := vals[p[1:len(p)-1]]; ok {
			return v
		}
		return p
	})
}

func valueIn(v any, lang string) string {
	switch x := v.(type) {
	case string:
		return x
	case wire.Text:
		return Pick(lang, x)
	case map[string]string:
		return Pick(lang, x)
	case int:
		return strconv.Itoa(x)
	case int64:
		return strconv.FormatInt(x, 10)
	}
	return fmt.Sprint(v)
}

// Keys lists every key, sorted (for the tests).
func Keys() []string {
	out := make([]string, 0, len(catalogue["en"]))
	for k := range catalogue["en"] {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// Catalogue returns one language's catalogue (for the tests).
func Catalogue(lang string) map[string]string { return catalogue[lang] }

// Placeholders lists the {names} a message holds, sorted (for the tests).
func Placeholders(s string) []string {
	ps := placeholder.FindAllString(s, -1)
	sort.Strings(ps)
	return ps
}
