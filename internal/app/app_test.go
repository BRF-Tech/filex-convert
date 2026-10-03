// The plugin, measured the way filex will run it.
//
// Everything here goes through pluginkit's own test kit (plugintest): the
// fake filex behind it enforces the manifest's permissions and the
// job-versus-screen rules, so a test cannot pass by doing something the
// real host would refuse. scripts/build.sh runs these BEFORE the wasm
// build, and refuses to produce a module when they fail.
package app_test

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"sort"
	"strings"
	"testing"

	"github.com/brf-tech/filex/backend/pkg/pluginkit"
	"github.com/brf-tech/filex/backend/pkg/pluginkit/plugintest"
	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"

	convert "github.com/brf-tech/filex-convert"
	"github.com/brf-tech/filex-convert/internal/app"
	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/i18n"
	"github.com/brf-tech/filex-convert/internal/job"
	"github.com/brf-tech/filex-convert/internal/view"
)

// harness builds the plugin over a fake filex with those engines present,
// driven by the administrator — the one person told which engines are
// missing (view.canInstallEngines). TestMissingEnginesAreTheAdministrators-
// Business (internal/view) measures what everybody else sees.
func harness(engines ...string) *plugintest.Harness {
	h := plugintest.NewFor(convert.Manifest(), func(host *plugintest.Host) *pluginkit.Plugin {
		return app.Plugin(host)
	})
	h.Host.InstallEngine(engines...)
	h.Actor.Role = "admin"
	return h
}

func pngFixture() []byte {
	img := image.NewNRGBA(image.Rect(0, 0, 4, 4))
	for i := 0; i < 16; i++ {
		img.Set(i%4, i/4, color.NRGBA{R: uint8(i * 16), G: 20, B: 200, A: 255})
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}

func photo() plugintest.File {
	return plugintest.File{Name: "photo.png", Data: pngFixture(), Mime: "image/png"}
}

// formatLabels are identical in every language on purpose — PNG is PNG in
// Turkish too. The locale comparison is told so it can spend its warnings
// on the strings that should have been translated.
func formatLabels() []string {
	var out []string
	for _, c := range formats.Categories {
		for _, f := range formats.InCategory(c) {
			out = append(out, f.Label, f.ID)
		}
	}
	return out
}

// walk is the wizard, driven the way a browser drives it: every event
// carries the state the LAST surface answered with, and only the values of
// the step being answered. Anything the wizard kept in a field instead of
// in its state would be lost here, exactly as it is lost on screen.
type walk struct {
	t *testing.T
	h *plugintest.Harness
	s *wire.Surface
}

func opened(t *testing.T, h *plugintest.Harness, files ...plugintest.File) *walk {
	t.Helper()
	s, err := h.Open("options", files...)
	if err != nil {
		t.Fatal(err)
	}
	return &walk{t: t, h: h, s: s}
}

// choose presses a format button in its own category's group, under the
// key the group is drawn with NOW (it carries the current answer, see
// view.CategoryField). A press only selects: the step stays the format
// step until Next.
func (w *walk) choose(target string) *wire.Surface {
	w.t.Helper()
	f, ok := formats.ByID(target)
	if !ok {
		w.t.Fatalf("no such format %q", target)
	}
	current, _ := w.s.State[view.FieldTarget].(string)
	return w.took(w.h.Change("options", w.s.State, map[string]any{view.CategoryField(f.Category, current): target}))
}

func (w *walk) next(vals map[string]any) *wire.Surface {
	w.t.Helper()
	return w.took(w.h.Submit("options", w.s.State, vals))
}

func (w *walk) step() string {
	s, _ := w.s.State[view.StateStep].(string)
	return s
}

func (w *walk) took(s *wire.Surface, err error) *wire.Surface {
	w.t.Helper()
	if err != nil {
		w.t.Fatal(err)
	}
	w.s = s
	return s
}

// targetGroups is the format step's button groups: one select per
// category, keyed `target_<category>`, headed with the category's name.
func targetGroups(t *testing.T, s *wire.Surface) []wire.Field {
	t.Helper()
	var out []wire.Field
	for _, n := range plugintest.Forms(s) {
		for _, f := range plugintest.FieldsOf(n) {
			if strings.HasPrefix(f.Key, view.FieldTarget+"_") {
				out = append(out, f)
			}
		}
	}
	return out
}

// targetButtons is every format the screen offers, across all the groups.
func targetButtons(t *testing.T, s *wire.Surface) []string {
	t.Helper()
	var out []string
	for _, g := range targetGroups(t, s) {
		for _, o := range g.Options {
			out = append(out, o.Value)
		}
	}
	return out
}

func groupByKey(groups []wire.Field, key string) (wire.Field, bool) {
	for _, g := range groups {
		if g.Key == key {
			return g, true
		}
	}
	return wire.Field{}, false
}

// activeSteps counts the steps the spine marks as the one the person is
// on, and checks each is named in every language on the way past.
func activeSteps(t *testing.T, n wire.Node) int {
	t.Helper()
	raw, err := json.Marshal(n.Props["items"])
	if err != nil {
		t.Fatal(err)
	}
	var items []struct {
		ID    string            `json:"id"`
		State string            `json:"state"`
		Label map[string]string `json:"label"`
	}
	if err := json.Unmarshal(raw, &items); err != nil {
		t.Fatalf("a steps node must be items: [{id, label, state}]: %v", err)
	}
	if len(items) < 2 {
		t.Fatalf("a spine of %d step(s) is not a spine", len(items))
	}
	n0 := 0
	for _, it := range items {
		if it.ID == "" {
			t.Fatal("a step with no id")
		}
		for _, lang := range i18n.Langs {
			if it.Label[lang] == "" {
				t.Fatalf("step %q is not named in %s: %v", it.ID, lang, it.Label)
			}
		}
		if it.State == "active" {
			n0++
		}
	}
	return n0
}

func report(r plugintest.Report) string {
	var b strings.Builder
	for _, f := range r {
		b.WriteString("  " + f.String() + "\n")
	}
	if b.Len() == 0 {
		return "  (nothing)"
	}
	return b.String()
}

// ── the manifest and what is registered behind it ──────────────────────

func TestManifestIsOneFilexWouldInstall(t *testing.T) {
	h := harness()
	plugintest.CheckManifest(t, h.Manifest())
	plugintest.CheckRegistered(t, h.Plugin)

	// en, tr, es, de, fr — the languages the catalogues carry (owner's
	// decision, v0.43.0: the apps ship Spanish, German and French too).
	if got, want := strings.Join(h.Languages(), ","), strings.Join(i18n.Langs, ","); got != want {
		t.Fatalf("the manifest must declare %s, got %s", want, got)
	}
}

func TestManifestSpeaksEveryLanguageItPromises(t *testing.T) {
	h := harness()
	r := plugintest.InspectManifestLanguages(h.Manifest(), plugintest.LangOpts{})
	if errs := r.Errors(); len(errs) > 0 {
		t.Fatalf("a manifest with a half-translated Text is refused at install:\n%s", report(errs))
	}
}

// ── the options screen ─────────────────────────────────────────────────

func TestOptionsScreenIsDrawable(t *testing.T) {
	h := harness("ffmpeg", "imagemagick")
	s, err := h.Open("options", photo())
	if err != nil {
		t.Fatal(err)
	}
	if r := plugintest.InspectSurface(h.Manifest(), s).Errors(); len(r) > 0 {
		t.Fatalf("the options screen must be drawable by filex's renderer:\n%s", report(r))
	}
	if a, ok := plugintest.PrimaryAction(s); !ok || a.ID != "submit" {
		t.Fatalf("one primary button, and it is Convert: %+v (%v)", a, ok)
	}
	if !a(s).Disabled {
		t.Fatalf("with no target chosen yet, Convert must be disabled")
	}
}

func a(s *wire.Surface) wire.SurfaceAction {
	act, _ := plugintest.PrimaryAction(s)
	return act
}

// The target picker is a select, and filex draws a select as a ROW OF
// BUTTONS — never a dropdown. Since the wizard it is one row PER CATEGORY,
// under that category's own heading, so the test reads it the way the
// person sees it: every reachable format is a button of its own, in the
// group where it belongs, and no label has to carry its category with it.
func TestTargetListReadsAsAButtonGroup(t *testing.T) {
	h := harness()
	s, err := h.Open("options", photo())
	if err != nil {
		t.Fatal(err)
	}
	groups := targetGroups(t, s)
	if len(groups) < 2 {
		t.Fatalf("a PNG reaches more than one category, so there is more than one group; got %d", len(groups))
	}
	seen := map[string]bool{}
	var got []string
	for _, g := range groups {
		if strings.TrimSpace(g.Label) == "" {
			t.Fatalf("group %q has no heading, and the heading is what says which category these are", g.Key)
		}
		if len(g.Options) == 0 {
			t.Fatalf("group %q has no buttons and should not be drawn at all", g.Key)
		}
		for _, o := range g.Options {
			if o.Value == "" || o.Label == "" {
				t.Fatalf("a button with no value or no label: %+v", o)
			}
			if seen[o.Value] {
				t.Fatalf("two buttons carry the value %q", o.Value)
			}
			if strings.Contains(o.Label, "·") {
				t.Fatalf("the button %q still carries its category; %q is the heading above it", o.Label, g.Label)
			}
			seen[o.Value] = true
			got = append(got, o.Value)
		}
	}
	if len(got) < 5 {
		t.Fatalf("a PNG reaches more than %d formats without any engine", len(got))
	}
	want := graph.Targets([]string{"png"}, map[string]bool{})
	sort.Strings(want)
	sorted := append([]string(nil), got...)
	sort.Strings(sorted)
	if strings.Join(want, ",") != strings.Join(sorted, ",") {
		t.Fatalf("the buttons must be exactly what this server can reach.\nwant %v\ngot  %v", want, sorted)
	}
}

// A format that needs an engine this server does not have is NOT offered
// as a button; it drops to the grey list under the picker, with the engine
// it would need. A dropdown could grey an option out — a button group
// cannot, so the list is how the person learns why the format is missing.
func TestAFormatWithoutItsEngineFallsToTheGreyList(t *testing.T) {
	bare := harness()
	s, err := bare.Open("options", photo())
	if err != nil {
		t.Fatal(err)
	}
	offered := map[string]bool{}
	for _, v := range targetButtons(t, s) {
		offered[v] = true
	}

	var blocked []string
	for _, tgt := range graph.Targets([]string{"png"}, graph.AllEngines()) {
		if !offered[tgt] {
			blocked = append(blocked, tgt)
		}
	}
	if len(blocked) == 0 {
		t.Skip("this catalogue reaches everything without an engine")
	}

	lists := plugintest.Nodes(s, "list")
	if len(lists) != 1 {
		t.Fatalf("the grey list should be one list node, got %d", len(lists))
	}
	rows := rowIDs(t, lists[0])
	for _, want := range blocked {
		if !rows[want] {
			t.Fatalf("%q is reachable only with a missing engine, so it belongs in the grey list (rows: %v)", want, keys(rows))
		}
	}
	for _, r := range rowCells(t, lists[0]) {
		if strings.TrimSpace(r["needs"]) == "" {
			t.Fatalf("a grey row must say which engine it needs: %v", r)
		}
	}

	// With the engine installed, the same format becomes a button.
	full := harness(graph.Engines...)
	s2, err := full.Open("options", photo())
	if err != nil {
		t.Fatal(err)
	}
	offered2 := map[string]bool{}
	for _, v := range targetButtons(t, s2) {
		offered2[v] = true
	}
	for _, want := range blocked {
		if !offered2[want] {
			t.Fatalf("with every engine installed, %q must be offered as a button", want)
		}
	}
	if len(plugintest.Nodes(s2, "list")) != 0 {
		t.Fatalf("nothing is missing, so there is no grey list to show")
	}
}

func rowIDs(t *testing.T, n wire.Node) map[string]bool {
	t.Helper()
	out := map[string]bool{}
	rows, _ := n.Props["rows"].([]map[string]any)
	if rows == nil {
		t.Fatalf("list rows are not [{id, cells}]: %T", n.Props["rows"])
	}
	for _, r := range rows {
		id, _ := r["id"].(string)
		out[id] = true
	}
	return out
}

func rowCells(t *testing.T, n wire.Node) []map[string]string {
	t.Helper()
	var out []map[string]string
	rows, _ := n.Props["rows"].([]map[string]any)
	for _, r := range rows {
		cells, _ := r["cells"].(map[string]any)
		m := map[string]string{}
		for k, v := range cells {
			switch x := v.(type) {
			case string:
				m[k] = x
			case wire.Text:
				m[k] = x["en"]
			}
		}
		out = append(out, m)
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

// ── languages ──────────────────────────────────────────────────────────

// Every Text the screen carries must speak both declared languages. This
// is the check that would have caught a signature pad saying "Çiz / Yaz /
// Yükle" with an English heading above it.
func TestOptionsScreenSpeaksEveryLanguage(t *testing.T) {
	h := harness("ffmpeg")
	two := func() []plugintest.File {
		return []plugintest.File{{Name: "a.png", Data: pngFixture()}, {Name: "b.png", Data: pngFixture()}}
	}
	for _, step := range []struct {
		name string
		draw func() (*wire.Surface, error)
	}{
		{"the format step", func() (*wire.Surface, error) { return h.Open("options", photo()) }},
		{"the format step with a format chosen", func() (*wire.Surface, error) {
			return opened(t, h, photo()).choose("jpg"), nil
		}},
		{"the settings step", func() (*wire.Surface, error) {
			w := opened(t, h, photo())
			w.choose("jpg")
			return w.next(nil), nil
		}},
		{"the review step", func() (*wire.Surface, error) {
			w := opened(t, h, photo())
			w.choose("jpg")
			w.next(nil)
			return w.next(nil), nil
		}},
		{"the review step past a step it did not need", func() (*wire.Surface, error) {
			w := opened(t, h, photo())
			w.choose("pdf")
			return w.next(nil), nil
		}},
		{"the combine step", func() (*wire.Surface, error) {
			w := opened(t, h, two()...)
			w.choose("pdf")
			return w.next(nil), nil
		}},
		{"a settings step reached with nothing typed", func() (*wire.Surface, error) {
			w := opened(t, h, two()...)
			w.choose("pdf")
			w.next(nil)
			return w.next(nil), nil
		}},
		{"nothing selected", func() (*wire.Surface, error) { h.Select(); return h.Open("options") }},
		{"an unknown file type", func() (*wire.Surface, error) {
			return h.Open("options", plugintest.File{Name: "mystery.zzz", Data: []byte("?")})
		}},
	} {
		t.Run(step.name, func(t *testing.T) {
			s, err := step.draw()
			if err != nil {
				t.Fatal(err)
			}
			r := plugintest.InspectLanguages(h.Manifest(), s, plugintest.LangOpts{SameAllowed: formatLabels()})
			if errs := r.Errors(); len(errs) > 0 {
				t.Fatalf("every string on this screen must carry every declared language:\n%s", report(errs))
			}
		})
	}
}

// The same screen, drawn once per language, must be the same screen: same
// nodes, same buttons, nothing blank in any language. The strings the plugin
// picks itself (a field label, an option label) are only visible this way —
// and with five languages (en, tr, es, de, fr) there are four ways for one
// of them to fall back to English unnoticed.
//
// ⚠ The host still collapses every locale but Turkish to `en` before it
// calls an app (filex feat/043-srvtext fixes that); plugintest calls with
// each declared locale set directly, which is what this measures.
func TestOptionsScreenHasTheSameShapeInEveryLanguage(t *testing.T) {
	h := harness("ffmpeg", "office")
	byLocale, err := h.OpenInLocales("options", photo())
	if err != nil {
		t.Fatal(err)
	}
	if len(byLocale) != len(i18n.Langs) {
		t.Fatalf("expected the screen in %v, got %d", i18n.Langs, len(byLocale))
	}
	r := plugintest.InspectLocaleParity(byLocale, plugintest.LangOpts{SameAllowed: formatLabels()})
	if errs := r.Errors(); len(errs) > 0 {
		t.Fatalf("the screen is not the same screen in every language:\n%s", report(errs))
	}
	for _, w := range r.Warnings() {
		t.Logf("%s", w)
	}

	// The strings that MUST be translated, checked by name rather than by
	// warning: the group headings, the buttons in them and the footer.
	en := byLocale["en"]
	gEN := targetGroups(t, en)
	if len(gEN) == 0 {
		t.Fatal("no target groups on the English screen")
	}
	for _, lang := range i18n.Langs[1:] {
		g := targetGroups(t, byLocale[lang])
		if len(gEN) != len(g) {
			t.Fatalf("%s draws a different number of groups: %d vs %d", lang, len(g), len(gEN))
		}
		for i := range gEN {
			if gEN[i].Key != g[i].Key {
				t.Fatalf("%s: group %d is a different category: %q vs %q", lang, i, g[i].Key, gEN[i].Key)
			}
			if len(gEN[i].Options) != len(g[i].Options) {
				t.Fatalf("%s: group %q offers a different number of buttons", lang, gEN[i].Key)
			}
			for j := range gEN[i].Options {
				if gEN[i].Options[j].Value != g[i].Options[j].Value {
					t.Fatalf("%s: button %d of %q is a different format", lang, j, gEN[i].Key)
				}
			}
		}
		// Every heading is the category's name in THAT language (the proof
		// the headings are translated at all — "Video" alone could not show
		// it).
		for _, c := range formats.Categories {
			grp, ok := groupByKey(g, view.CategoryField(c, ""))
			if !ok {
				continue
			}
			if want := i18n.S(lang, "category."+string(c)); grp.Localized(lang).Label != want {
				t.Fatalf("%s: the %s group is headed %q, want %q", lang, c, grp.Localized(lang).Label, want)
			}
		}
		a, _ := plugintest.PrimaryAction(byLocale[lang])
		if a.Label[lang] == "" || a.Label[lang] == a.Label["en"] {
			t.Fatalf("%s: the step's own button is not written in %s: %v", lang, lang, a.Label)
		}
	}
}

// A merged conversion writes its own route note. It used to be built for
// the call's locale and then pasted into BOTH slots of a Text, so a
// Turkish sentence travelled under the `en` key.
func TestTheMergeNoteIsWrittenInEveryLanguage(t *testing.T) {
	h := harness("ffmpeg")
	w := opened(t, h,
		plugintest.File{Name: "a.png", Data: pngFixture()},
		plugintest.File{Name: "b.png", Data: pngFixture()},
	)
	w.choose("mp4")
	s := w.next(nil)
	if _, ok := plugintest.Field(s, "merge"); !ok {
		t.Skip("this selection offers no merge")
	}
	// answering "one file for all of them" is what writes the note, and it
	// is written on the settings step and again in the review
	for _, s := range []*wire.Surface{w.next(map[string]any{"merge": true}), w.next(nil)} {
		for _, tx := range plugintest.SurfaceTexts(s, h.Languages()) {
			if tx.Text["en"] != "" && tx.Text["en"] == tx.Text["tr"] && strings.Contains(tx.Text["en"], "tüm dosyalar") {
				t.Fatalf("%s carries Turkish words under `en`: %q", tx.Where, tx.Text["en"])
			}
		}
		r := plugintest.InspectLanguages(h.Manifest(), s, plugintest.LangOpts{SameAllowed: formatLabels()})
		if errs := r.Errors(); len(errs) > 0 {
			t.Fatalf("the merge screens must speak every declared language:\n%s", report(errs))
		}
	}
}

// Every step of the wizard, measured against the renderer's own rules: a
// spine that says where the person is, exactly one step active on it, and
// a footer that asks one thing.
func TestEveryStepIsDrawableAndAsksOneThing(t *testing.T) {
	h := harness(graph.Engines...)
	w := opened(t, h,
		plugintest.File{Name: "a.png", Data: pngFixture()},
		plugintest.File{Name: "b.png", Data: pngFixture()},
	)
	var walked []string
	for i := 0; ; i++ {
		here := w.step()
		if r := plugintest.InspectSurface(h.Manifest(), w.s).Errors(); len(r) > 0 {
			t.Fatalf("step %q is not drawable by filex's renderer:\n%s", here, report(r))
		}
		spine := plugintest.Nodes(w.s, "steps")
		if len(spine) != 1 {
			t.Fatalf("step %q carries %d step spines; a wizard has exactly one", here, len(spine))
		}
		if n := activeSteps(t, spine[0]); n != 1 {
			t.Fatalf("%d steps are active on %q; exactly one is the step the person is on", n, here)
		}
		primary := 0
		for _, a := range w.s.Actions {
			if a.Primary {
				primary++
			}
		}
		if primary != 1 {
			t.Fatalf("step %q has %d primary buttons: %+v", here, primary, w.s.Actions)
		}
		if len(w.s.Actions) > 2 {
			t.Fatalf("step %q has %d buttons; one primary plus Back is the whole footer", here, len(w.s.Actions))
		}
		if len(plugintest.Forms(w.s)) > 1 {
			t.Fatalf("step %q draws %d forms; one step asks one thing", here, len(plugintest.Forms(w.s)))
		}
		walked = append(walked, here)
		if here == view.StepReview {
			break
		}
		if i > 6 {
			t.Fatalf("the wizard does not end: %v", walked)
		}
		if here == view.StepFormat {
			// a press only selects; Next is what moves on
			w.choose("mp4")
			if w.step() != view.StepFormat {
				t.Fatalf("pressing a format moved the wizard to %q; a press only selects", w.step())
			}
		}
		w.next(nil)
	}
	if strings.Join(walked, ">") != "format>combine>settings>review" {
		t.Fatalf("the steps walked were %v", walked)
	}
}

// ── a conversion, end to end, on the fake host ─────────────────────────

func TestAConversionRunsEndToEnd(t *testing.T) {
	h := harness()
	w := opened(t, h, photo())
	w.choose("jpg")  // a press only selects …
	w.next(nil)      // … Next leaves the format step
	w.next(nil)      // the settings step, left at its defaults
	s := w.next(nil) // the review step: this is the one that queues
	if s.Job == nil {
		t.Fatalf("submitting a chosen target must queue the job, got %+v", s)
	}
	if s.Job.ActionID != convert.ActionID || s.Job.Params["target"] != "jpg" {
		t.Fatalf("the queued job is wrong: %+v", s.Job)
	}

	out, err := h.Queue(s)
	if err != nil {
		t.Fatal(err)
	}
	if !out.OK || len(out.Outputs) != 1 {
		t.Fatalf("one file in, one file out: %+v", out)
	}
	if out.Outputs[0].Name != "photo.jpg" {
		t.Fatalf("the sibling is named after the source: %q", out.Outputs[0].Name)
	}
	body, ok := h.Host.Bytes(out.Outputs[0].Ref)
	if !ok || !bytes.HasPrefix(body, []byte{0xff, 0xd8}) {
		t.Fatalf("the output is not a JPEG: %x", first(body, 8))
	}
	for _, lang := range i18n.Langs {
		if out.Message[lang] == "" {
			t.Fatalf("the job's message must reach the person in %s: %v", lang, out.Message)
		}
	}
	if len(h.Host.ProgressLog) == 0 {
		t.Fatalf("a conversion must report progress")
	}
	if len(h.Host.Engines) != 0 {
		t.Fatalf("PNG → JPEG is pure Go; no engine should have been asked for: %v", h.Host.Engines)
	}
}

// Without the engine the route needs, the job fails with a message that
// names the engine — and it fails per file, not by trapping.
func TestAConversionThatNeedsAMissingEngineFailsKindly(t *testing.T) {
	h := harness()
	out, err := h.Do(convert.ActionID, map[string]any{"target": "mp4"}, photo())
	if err != nil {
		t.Fatal(err)
	}
	if out.OK && len(out.Outputs) > 0 {
		t.Fatalf("there is no ffmpeg on this server; the job cannot have produced a video: %+v", out)
	}
	for _, lang := range i18n.Langs {
		if out.Message[lang] == "" {
			t.Fatalf("the failure must be readable in %s: %v", lang, out.Message)
		}
	}
}

// The fake host holds the manifest's permissions, so a call the plugin
// never asked for is refused with the code the real host uses. The engine
// grant is in the manifest; the engine being INSTALLED is a separate
// question, and the two failures must not look alike.
func TestTheHostRefusesWhatWasNeverGranted(t *testing.T) {
	h := harness()
	h.Host.EnterJob()
	if _, err := h.Host.EngineRun(pluginkit.EngineRequest{Engine: "ffmpeg"}); !plugintest.IsCode(err, wire.ErrUnavailable) {
		t.Fatalf("a granted engine that is not installed is `unavailable`, got %q", plugintest.Code(err))
	}
	if err := h.Host.MailSend("a@b.test", "s", "b"); !plugintest.IsCode(err, wire.ErrPermissionDenied) {
		t.Fatalf("convert never asks for mail:send, so sending must be refused, got %q", plugintest.Code(err))
	}
	if _, err := h.Host.ShareCreate(pluginkit.PageCreate{PageID: "anything"}); !plugintest.IsCode(err, wire.ErrPermissionDenied) {
		t.Fatalf("convert opens no public links, so share_create must be refused, got %q", plugintest.Code(err))
	}
	h.Host.EnterScreen()
	if _, err := h.Host.WriteOutput("sneaky.txt", []byte("x")); !plugintest.IsCode(err, wire.ErrPermissionDenied) {
		t.Fatalf("the options SCREEN may not write files, got %q", plugintest.Code(err))
	}
}

func first(b []byte, n int) []byte {
	if len(b) < n {
		return b
	}
	return b[:n]
}

// ── golden screen ──────────────────────────────────────────────────────

// The options screen for a PNG on a server with no engines: the buttons,
// the knobs and the grey list, stored. A refactor that changes what the
// person sees shows up as a diff instead of as a surprise.
//
//	go test ./... -update    rewrite it, then review `git diff`
func TestGoldenOptionsScreen(t *testing.T) {
	h := harness()
	plugintest.Golden(t, "options-png-no-engines", opened(t, h, photo()).s)

	full := opened(t, harness(graph.Engines...), photo())
	plugintest.Golden(t, "options-png-jpg-chosen", full.choose("jpg"))
	plugintest.Golden(t, "options-png-jpg-settings", full.next(nil))
	plugintest.Golden(t, "options-png-jpg-review", full.next(nil))
}

// The SDK host is what the real module uses; it must still satisfy the
// same interface the fake one does, or the two drift apart silently.
var _ job.Host = job.SDKHost{}
var _ job.Host = (*plugintest.Host)(nil)
