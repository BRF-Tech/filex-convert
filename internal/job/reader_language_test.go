package job

// What a job says to a person is in that person's language, all of it.
//
// Two ways it was not (2026-09-26, the Turkish embedded filex):
//
//   - a failure's reason was followed by the engine's or the Go decoder's own
//     words, in English: "foto.png: dönüşüm başarısız oldu (png: invalid
//     format: not a PNG file)", "dönüştürücü başarısız oldu (ffmpeg exit 1:
//     Conversion failed!)";
//   - the tray line is ONE string, picked for the job's locale — and the
//     job's locale is the host's guess, which an embedded filex gets wrong:
//     the Turkish tray read "notes.md → Plain text (.txt)".

import (
	"regexp"
	"strings"
	"testing"

	"github.com/brf-tech/filex/backend/pkg/pluginkit"
	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"

	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/graph"
)

var letters = regexp.MustCompile(`\p{L}+`)

// sharedWords lists the words of tr that en has too and that are not names
// (format names, engine names, the file names in play).
func sharedWords(tr, en string, names ...string) []string {
	ok := map[string]bool{}
	for _, f := range formats.All {
		for _, s := range append([]string{f.ID, f.Label}, f.Ext...) {
			if s == f.Label && f.Name("en") != f.Name("tr") {
				continue // a description, not a name
			}
			for _, w := range letters.FindAllString(s, -1) {
				ok[strings.ToLower(w)] = true
			}
		}
	}
	for _, e := range graph.Engines {
		ok[strings.ToLower(e)] = true
		ok[strings.ToLower(graph.EngineName(e))] = true
	}
	for _, n := range names {
		for _, w := range letters.FindAllString(n, -1) {
			ok[strings.ToLower(w)] = true
		}
	}
	enWords := map[string]bool{}
	for _, w := range letters.FindAllString(en, -1) {
		enWords[strings.ToLower(w)] = true
	}
	var out []string
	for _, w := range letters.FindAllString(tr, -1) {
		lw := strings.ToLower(w)
		if len([]rune(lw)) >= 3 && enWords[lw] && !ok[lw] {
			out = append(out, w)
		}
	}
	return out
}

func TestAFailureIsReadInTheReadersLanguage(t *testing.T) {
	for _, c := range []struct {
		name    string
		file    string
		data    string
		target  string
		engines map[string]bool
		engine  func(n int, req pluginkit.EngineRequest) (*pluginkit.EngineResult, error)
	}{
		{name: "a decoder refuses the file", file: "photo.png", data: "not a png at all", target: "jpg"},
		{name: "the engine exits non-zero", file: "clip.avi", data: "x", target: "mp4", engines: map[string]bool{graph.FFmpeg: true},
			engine: func(int, pluginkit.EngineRequest) (*pluginkit.EngineResult, error) {
				return &pluginkit.EngineResult{Exit: 1, StderrTail: "Invalid data found when processing input\nConversion failed!"}, nil
			}},
		{name: "the engine produced nothing", file: "clip.avi", data: "x", target: "mp4", engines: map[string]bool{graph.FFmpeg: true},
			engine: func(int, pluginkit.EngineRequest) (*pluginkit.EngineResult, error) {
				return &pluginkit.EngineResult{StderrTail: "Output file is empty, nothing was encoded"}, nil
			}},
		{name: "the engine went away", file: "clip.avi", data: "x", target: "mp4", engines: map[string]bool{graph.FFmpeg: true},
			engine: func(int, pluginkit.EngineRequest) (*pluginkit.EngineResult, error) {
				return nil, &pluginkit.HostError{Code: wire.ErrUnavailable, Message: "engine ffmpeg is not installed on this host"}
			}},
		{name: "the engine is missing", file: "clip.avi", data: "x", target: "mp4"},
	} {
		t.Run(c.name, func(t *testing.T) {
			h := newFake()
			h.input("in:0", c.data)
			h.engineFn = c.engine
			out := run(t, h, c.target, c.engines, nil, c.file)
			if out.OK {
				t.Fatalf("this conversion was meant to fail: %+v", out)
			}
			tr, en := out.Message["tr"], out.Message["en"]
			if tr == "" || tr == en {
				t.Fatalf("the failure has no Turkish of its own: tr %q, en %q", tr, en)
			}
			if leak := sharedWords(tr, en, c.file); len(leak) > 0 {
				t.Errorf("the Turkish message %q carries the English words %v", tr, leak)
			}
			// …and the administrator still finds the technical line in the log.
			if len(h.logs) == 0 {
				t.Errorf("the failure's detail must reach the plugin's log")
			}
		})
	}
}

// The tray line is one string: it must read the same to a Turkish and an
// English reader, so it names the format by a NAME, never by a description
// ("Plain text (.txt)" / "Düz metin (.txt)").
func TestTheTrayLineReadsTheSameInEveryLanguage(t *testing.T) {
	tried := 0
	for _, f := range formats.All {
		if f.Name("en") == f.Name("tr") {
			continue // a name: the same for everybody already
		}
		src := ""
		for _, s := range formats.All {
			if s.ID != f.ID && s.Category != formats.Archive {
				if _, err := graph.Route(s.ID, f.ID, graph.AllEngines()); err == nil {
					src = s.ID
					break
				}
			}
		}
		if src == "" {
			continue
		}
		s, _ := formats.ByID(src)
		name := "a." + s.Primary()
		lines := map[string]string{}
		for _, locale := range []string{"en", "tr"} {
			h := newFake()
			h.input("in:0", "x")
			in := &wire.ActionRunInput{JobID: "j", ActionID: "convert", Locale: locale, Params: map[string]any{ParamTarget: f.ID},
				Engines: graph.AllEngines(), Inputs: []wire.FileRef{{Ref: "in:0", Name: name, Size: 1}}}
			if _, err := Run(h, in); err != nil {
				t.Fatal(err)
			}
			if len(h.progress) == 0 {
				t.Fatalf("%s → %s reported no progress", src, f.ID)
			}
			lines[locale] = h.progress[0]
		}
		tried++
		if lines["en"] != lines["tr"] {
			t.Errorf("%s: the tray line is %q for one reader and %q for another; it is one string, so it must be both", f.ID, lines["en"], lines["tr"])
		}
		for _, lang := range []string{"en", "tr"} {
			if strings.Contains(lines["en"], f.Name(lang)) {
				t.Errorf("%s: the tray line %q is the %s description %q", f.ID, lines["en"], lang, f.Name(lang))
			}
		}
	}
	if tried == 0 {
		t.Fatal("no format with a description was reachable; this test measured nothing")
	}
}
