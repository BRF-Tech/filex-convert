package app_test

// A Turkish reader reads Turkish — whatever language the CALL was made in.
//
// filex hands a view event the host's guess at the reader's language: the
// account's saved language first, the request's Accept-Language second. An
// embedded filex (the web component, mounted in a host page with
// `locale: "tr"`) is where that guess is wrong: the account was saved as
// English, the window is Turkish, and every string the plugin picked for the
// call came out English between the Turkish words filex picked itself —
// "Image", "Plain text (.txt)", "Quality", "1–100; higher is larger and
// sharper", "Combine into one file" on a screen whose steps, headings and
// buttons were Turkish (Burak, 2026-09-26: "converter'da bazı yerler
// İngilizce kalıyor").
//
// So this draws every step of the wizard with the call in one language and
// READS it in the other, the way filex's renderer reads it (a Text in the
// reader's language, a plain string as it is), and refuses:
//
//   - a string a person reads that is the same for a Turkish and an English
//     reader, unless it is only names (PNG, FFmpeg, photo.png) and numbers;
//   - a Turkish string that carries an English word the English one has.
//
// Drawn with the call in Turkish too: a screen that is right for the call's
// language only is wrong for the English reader of a Turkish account.

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/brf-tech/filex/backend/pkg/pluginkit/plugintest"
	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"

	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/i18n"
)

// namedScreen is one drawn screen and what it is.
type namedScreen struct {
	name string
	s    *wire.Surface
}

// fixture names used below; a file name reads the same in every language.
var readerFixtures = []string{"photo.png", "b.jpg", "clip.mp4", "song.mp3", "doc.pdf", "report.docx", "mystery.zzz", "a.avif"}

func file(name string, readOnly bool) plugintest.File {
	data := []byte("x")
	if strings.HasSuffix(name, ".png") {
		data = pngFixture()
	}
	return plugintest.File{Name: name, Data: data, ReadOnly: readOnly}
}

// everyScreen walks the wizard through every step and every kind of knob,
// with the call made in `call`.
func everyScreen(t *testing.T, call string) []namedScreen {
	t.Helper()
	var out []namedScreen
	add := func(name string, s *wire.Surface) {
		t.Helper()
		if s == nil {
			t.Fatalf("%s: no surface", name)
		}
		out = append(out, namedScreen{name, s})
	}
	h := func(engines ...string) *plugintest.Harness {
		x := harness(engines...)
		x.Locale = call
		return x
	}
	// walkTo opens files, presses target and walks Next to the review,
	// drawing every screen on the way.
	walkTo := func(label string, x *plugintest.Harness, target string, answers map[string]map[string]any, files ...plugintest.File) {
		t.Helper()
		w := opened(t, x, files...)
		add(label+": format", w.s)
		add(label+": chosen "+target, w.choose(target))
		for i := 0; w.step() != "review"; i++ {
			if i > 6 {
				t.Fatalf("%s: the wizard never reached the review", label)
			}
			s := w.next(answers[w.step()])
			if s.Job != nil {
				t.Fatalf("%s: a job was queued before the review", label)
			}
			add(label+": "+w.step(), s)
		}
	}

	walkTo("png, no engine (the grey list)", h(), "jpg", nil, file("photo.png", false))
	walkTo("png → ico", h(graph.Engines...), "ico", nil, file("photo.png", false))
	walkTo("png → pdf (settings not needed)", h(graph.Engines...), "pdf", nil, file("photo.png", false))
	walkTo("mp4 → webm", h(graph.Engines...), "webm", nil, file("clip.mp4", false))
	walkTo("mp4 → png (frames)", h(graph.Engines...), "png", nil, file("clip.mp4", false))
	walkTo("mp3 → png (picture)", h(graph.Engines...), "png", nil, file("song.mp3", false))
	walkTo("mp3 → ogg (bitrate)", h(graph.Engines...), "ogg", nil, file("song.mp3", false))
	walkTo("pdf → png (pages)", h(graph.Engines...), "png", nil, file("doc.pdf", false))
	walkTo("pdf → pdf (re-encode, PDF/A)", h(graph.Engines...), "pdf", nil, file("doc.pdf", false))
	walkTo("docx → pdf", h(graph.Engines...), "pdf", nil, file("report.docx", false))
	walkTo("two images → one gif", h(graph.Engines...), "gif", map[string]map[string]any{"combine": {"merge": true}},
		file("photo.png", false), file("b.jpg", false))
	walkTo("two images → pdf, one each", h(graph.Engines...), "pdf", nil, file("photo.png", false), file("b.jpg", false))
	walkTo("read-only source", h(), "pdf", map[string]map[string]any{"where": {"dest": "depo://"}}, file("photo.png", true))

	// A refused answer, a Next with nothing picked, a Where with no folder.
	x := h()
	w := opened(t, x, file("photo.png", false))
	w.choose("jpg")
	w.next(nil)
	add("a refused answer", w.next(map[string]any{"quality": "abc"}))
	w = opened(t, x, file("photo.png", false))
	add("Next with nothing picked", w.next(nil))
	w = opened(t, x, file("photo.png", true))
	w.choose("pdf")
	for i := 0; w.step() != "where" && i < 6; i++ {
		w.next(nil)
	}
	add("Where with no folder", w.next(nil))

	// The screens with no wizard in them.
	x = h()
	x.Select()
	s, err := x.Open("options")
	if err != nil {
		t.Fatal(err)
	}
	add("nothing selected", s)
	s, err = x.Open("options", file("mystery.zzz", false))
	if err != nil {
		t.Fatal(err)
	}
	add("an unknown file type", s)
	s, err = x.Open("options", file("a.avif", false))
	if err != nil {
		t.Fatal(err)
	}
	add("an avif with no engine", s)
	return out
}

// readAs is the surface as filex's renderer shows it to a reader in lang:
// every Text in that language, every plain string a person reads as it is.
func readAs(t *testing.T, s *wire.Surface, lang string) map[string]string {
	t.Helper()
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	var generic any
	if err := json.Unmarshal(raw, &generic); err != nil {
		t.Fatal(err)
	}
	out := map[string]string{}
	// parent is the key of the map v sits in: a list row's cells are what
	// the person reads, whatever each column is called.
	var walk func(path, key, parent string, v any)
	walk = func(path, key, parent string, v any) {
		if tx, ok := plugintest.AsText(v, i18n.Langs); ok {
			out[path] = tx.Get(lang)
			return
		}
		switch x := v.(type) {
		case map[string]any:
			if path == "surface.state" || strings.HasSuffix(path, ".values") {
				return // answers and state, never drawn as words
			}
			for k, e := range x {
				walk(path+"."+k, k, key, e)
			}
		case []any:
			for i, e := range x {
				walk(fmt.Sprintf("%s[%d]", path, i), key, parent, e)
			}
		case string:
			if plugintest.HumanKeys[key] || parent == "cells" {
				out[path] = x
			}
		}
	}
	walk("surface", "", "", generic)
	return out
}

var wordRe = regexp.MustCompile(`\p{L}+`)

// nameWords are the words that are the same in every language on purpose:
// format names, engine names, the fixtures' file names, units.
func nameWords() map[string]bool {
	ok := map[string]bool{}
	add := func(s string) {
		for _, w := range wordRe.FindAllString(s, -1) {
			ok[strings.ToLower(w)] = true
		}
	}
	for _, f := range formats.All {
		add(f.ID)
		for _, e := range f.Ext {
			add(e)
		}
		if f.Name("en") == f.Name("tr") {
			add(f.Label) // a name, not a description
		}
	}
	for _, e := range graph.Engines {
		add(e)
		add(graph.EngineName(e))
	}
	for _, n := range readerFixtures {
		add(n)
	}
	// Words Turkish shares with English on purpose: "Video" is Turkish, a
	// favicon is a favicon, and units are units. "OpenDocument" is a name.
	for _, w := range []string{"video", "favicon", "crf", "dpi", "px", "kbit", "s", "opendocument", "depo"} {
		ok[w] = true
	}
	return ok
}

func onlyNames(s string, names map[string]bool) bool {
	for _, w := range wordRe.FindAllString(s, -1) {
		if !names[strings.ToLower(w)] {
			return false
		}
	}
	return true
}

// englishIn lists the words of the Turkish string that are the English
// string's words and not names (three letters and up: "a" and "s" are not
// evidence).
func englishIn(tr, en string, names map[string]bool) []string {
	enWords := map[string]bool{}
	for _, w := range wordRe.FindAllString(en, -1) {
		enWords[strings.ToLower(w)] = true
	}
	var out []string
	for _, w := range wordRe.FindAllString(tr, -1) {
		lw := strings.ToLower(w)
		if len([]rune(lw)) >= 3 && enWords[lw] && !names[lw] {
			out = append(out, w)
		}
	}
	return out
}

func TestTheScreenIsTheReadersLanguageNotTheCalls(t *testing.T) {
	names := nameWords()
	for _, call := range []string{"en", "tr"} {
		for _, sc := range everyScreen(t, call) {
			t.Run("call "+call+"/"+sc.name, func(t *testing.T) {
				tr, en := readAs(t, sc.s, "tr"), readAs(t, sc.s, "en")
				paths := make([]string, 0, len(tr))
				for p := range tr {
					paths = append(paths, p)
				}
				sort.Strings(paths)
				for _, p := range paths {
					got, eng := tr[p], en[p]
					if strings.TrimSpace(got) == "" {
						continue
					}
					if got == eng && !onlyNames(got, names) {
						t.Errorf("%s: a Turkish reader and an English reader both read %q — one of them is reading the other's language", p, got)
						continue
					}
					if leak := englishIn(got, eng, names); len(leak) > 0 {
						t.Errorf("%s: the Turkish %q carries the English words %v", p, got, leak)
					}
				}
			})
		}
	}
}
