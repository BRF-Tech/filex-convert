package view

import (
	"fmt"
	"strings"
	"testing"

	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"

	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/i18n"
	"github.com/brf-tech/filex-convert/internal/options"
)

func event(ev string, engines map[string]bool, locale string, names ...string) *wire.ViewEventInput {
	in := &wire.ViewEventInput{ViewID: "options", Event: ev, Context: wire.CallContext{Engines: engines, Locale: locale}}
	for i, n := range names {
		in.Context.Inputs = append(in.Context.Inputs, wire.FileRef{Ref: "in:" + string(rune('0'+i)), Name: n})
	}
	return in
}

// wiz drives the wizard the way the client does: every event carries the
// state the LAST surface answered with, plus whatever the person did on
// this step. An answer the wizard kept only in a form field would be lost
// here, exactly as it is lost in a browser.
type wiz struct {
	t       *testing.T
	engines map[string]bool
	locale  string
	names   []string
	last    *wire.Surface
	// ctx, when set, adjusts every event's context (a read-only input, the
	// person's home folder) the way a host would send it.
	ctx func(*wire.ViewEventInput)
}

func open(t *testing.T, engines map[string]bool, locale string, names ...string) *wiz {
	t.Helper()
	w := &wiz{t: t, engines: engines, locale: locale, names: names}
	w.send("open", "", nil)
	return w
}

func (w *wiz) send(ev, actionID string, vals map[string]any) *wire.Surface {
	w.t.Helper()
	in := event(ev, w.engines, w.locale, w.names...)
	if w.ctx != nil {
		w.ctx(in)
	}
	in.ActionID = actionID
	if w.last != nil {
		in.State = w.last.State
	}
	if vals != nil {
		in.Data = map[string]any{"values": vals}
	}
	s, err := Handle(in)
	if err != nil {
		w.t.Fatal(err)
	}
	w.last = s
	return s
}

// choose presses a format button in its own category's group, under the
// key that group is drawn with NOW — the key carries the current answer
// (CategoryField). A press only selects; Next moves on.
func (w *wiz) choose(target string) *wire.Surface {
	w.t.Helper()
	f, ok := formats.ByID(target)
	if !ok {
		w.t.Fatalf("no such format %q", target)
	}
	return w.send("change", "", map[string]any{CategoryField(f.Category, w.target()): target})
}

// pick is choose + Next: the two presses that leave the format step.
func (w *wiz) pick(target string) *wire.Surface {
	w.t.Helper()
	w.choose(target)
	return w.next(nil)
}

// toReview presses Next until the review step, and fails — rather than
// spinning — when the wizard never gets there or queues a job on the way:
// a broken wizard is a red test, not a hung one.
func (w *wiz) toReview() *wire.Surface {
	w.t.Helper()
	for i := 0; w.step() != StepReview; i++ {
		if i > 6 {
			w.t.Fatalf("the wizard never reached the review step (on %q)", w.step())
		}
		if s := w.next(nil); s.Job != nil {
			w.t.Fatalf("a job was queued before the review step: %+v", s.Job)
		}
	}
	return w.last
}

func (w *wiz) target() string {
	if w.last == nil {
		return ""
	}
	s, _ := w.last.State[FieldTarget].(string)
	return s
}

func (w *wiz) next(vals map[string]any) *wire.Surface { return w.send("submit", ActionSubmit, vals) }
func (w *wiz) back() *wire.Surface                    { return w.send("action", ActionBack, nil) }

func (w *wiz) step() string {
	s, _ := w.last.State[StateStep].(string)
	return s
}

func formOf(t *testing.T, s *wire.Surface) (fields []wire.Field, vals map[string]any) {
	t.Helper()
	for _, n := range s.Nodes {
		if n.Type == "form" {
			return n.Props["fields"].([]wire.Field), n.Props["values"].(map[string]any)
		}
	}
	t.Fatalf("no form node in %+v", s.Nodes)
	return nil, nil
}

// targetOptions is every format button on the format step, read across the
// category groups in the order the person sees them.
func targetOptions(t *testing.T, s *wire.Surface) []string {
	t.Helper()
	var out []string
	for _, f := range targetGroups(t, s) {
		for _, o := range f.Options {
			out = append(out, o.Value)
		}
	}
	return out
}

func targetGroups(t *testing.T, s *wire.Surface) []wire.Field {
	t.Helper()
	fields, _ := formOf(t, s)
	var out []wire.Field
	for _, f := range fields {
		if !strings.HasPrefix(f.Key, FieldTarget+"_") {
			t.Fatalf("the format step carries a field that is not a category group: %q", f.Key)
		}
		if f.Type != "select" {
			t.Fatalf("group %q is a %q, and a choice must be a select (which filex draws as buttons)", f.Key, f.Type)
		}
		out = append(out, f)
	}
	return out
}

func texts(s *wire.Surface) []string {
	var out []string
	for _, n := range s.Nodes {
		if n.Type == "text" {
			out = append(out, n.Props["text"].(wire.Text)["en"]+" | "+n.Props["text"].(wire.Text)["tr"])
		}
	}
	return out
}

// listRows reads the grey list: its cells are the plain strings a list
// draws (the review table's cells are Texts, and reviewRows reads those).
func listRows(s *wire.Surface) []string {
	var out []string
	for _, n := range s.Nodes {
		if n.Type != "list" {
			continue
		}
		for _, r := range n.Props["rows"].([]map[string]any) {
			cells := r["cells"].(map[string]any)
			format, ok := cells["format"].(string)
			if !ok {
				continue
			}
			out = append(out, format+" — "+cells["needs"].(string))
		}
	}
	return out
}

// reviewRows reads the last step's table as {row id: "en | tr"}.
func reviewRows(t *testing.T, s *wire.Surface) map[string]string {
	t.Helper()
	out := map[string]string{}
	for _, n := range s.Nodes {
		if n.Type != "list" {
			continue
		}
		for _, r := range n.Props["rows"].([]map[string]any) {
			cells := r["cells"].(map[string]any)
			v, ok := cells["value"].(wire.Text)
			if !ok {
				continue
			}
			out[r["id"].(string)] = v["en"] + " | " + v["tr"]
		}
	}
	if len(out) == 0 {
		t.Fatalf("no review table in %+v", s.Nodes)
	}
	return out
}

// spineLabels reads the steps node's labels in one language.
func spineLabels(t *testing.T, s *wire.Surface, lang string) []string {
	t.Helper()
	var out []string
	for _, n := range s.Nodes {
		if n.Type != "steps" {
			continue
		}
		for _, it := range n.Props["items"].([]map[string]any) {
			out = append(out, it["label"].(wire.Text)[lang])
		}
	}
	return out
}

// spine reads the steps node as "format:done", "settings:active", …
func spine(t *testing.T, s *wire.Surface) []string {
	t.Helper()
	var out []string
	for _, n := range s.Nodes {
		if n.Type != "steps" {
			continue
		}
		for _, it := range n.Props["items"].([]map[string]any) {
			out = append(out, it["id"].(string)+":"+it["state"].(string))
		}
	}
	if len(out) == 0 {
		t.Fatalf("this screen has no step spine: %+v", s.Nodes)
	}
	return out
}

func active(t *testing.T, s *wire.Surface) string {
	t.Helper()
	found := ""
	for _, it := range spine(t, s) {
		if strings.HasSuffix(it, ":active") {
			if found != "" {
				t.Fatalf("two steps are active at once: %v", spine(t, s))
			}
			found = strings.TrimSuffix(it, ":active")
		}
	}
	if found == "" {
		t.Fatalf("no step is active: %v", spine(t, s))
	}
	return found
}

func primaries(s *wire.Surface) []wire.SurfaceAction {
	var out []wire.SurfaceAction
	for _, a := range s.Actions {
		if a.Primary {
			out = append(out, a)
		}
	}
	return out
}

func fieldByKey(t *testing.T, s *wire.Surface, key string) wire.Field {
	t.Helper()
	fields, _ := formOf(t, s)
	for _, f := range fields {
		if f.Key == key {
			return f
		}
	}
	t.Fatalf("no field %q on this step (%v)", key, fields)
	return wire.Field{}
}

// ── the format step ────────────────────────────────────────────────────

func TestOpenPureGoOnly(t *testing.T) {
	w := open(t, nil, "en", "photo.png")
	s := w.last
	if s.Title["tr"] != "Dönüştür" {
		t.Errorf("title %v", s.Title)
	}
	if got := active(t, s); got != StepFormat {
		t.Errorf("a screen opens on its first step, not %q", got)
	}
	opts := targetOptions(t, s)
	for _, want := range []string{"jpg", "gif", "bmp", "tiff", "webp", "ico", "qoi", "pnm", "tga", "pdf", "svg", "txt", "zip", "tar.gz"} {
		if want == "tar.gz" {
			want = "tgz"
		}
		if !contains(opts, want) {
			t.Errorf("png without engines should offer %s: %v", want, opts)
		}
	}
	for _, no := range []string{"avif", "mp4", "docx", "heic"} {
		if contains(opts, no) {
			t.Errorf("png without engines must not offer %s", no)
		}
	}
	// the note names the missing engines and the list says what each unlocks
	joined := strings.Join(texts(s), "\n")
	if !strings.Contains(joined, "Not installed on this server: FFmpeg, ImageMagick, LibreOffice, Ghostscript, Poppler, librsvg") {
		t.Errorf("missing-engine note absent:\n%s", joined)
	}
	if !strings.Contains(joined, "Bu sunucuda kurulu değil") {
		t.Errorf("tr note missing:\n%s", joined)
	}
	rows := strings.Join(listRows(s), "\n")
	for _, want := range []string{"Image · AVIF — needs ImageMagick", "Video · MP4 — needs FFmpeg", "Image · Photoshop (.psd) — needs ImageMagick"} {
		if !strings.Contains(rows, want) {
			t.Errorf("disabled list should carry %q:\n%s", want, rows)
		}
	}
	if strings.Contains(rows, "Word (.docx)") {
		t.Errorf("a png never becomes docx, even with engines:\n%s", rows)
	}
	if len(s.Actions) != 2 || s.Actions[1].ID != ActionSubmit || !s.Actions[1].Primary || !s.Actions[1].Disabled {
		t.Errorf("actions %+v (the primary must be disabled until a target is picked)", s.Actions)
	}
}

// Every format is a button under its category's own heading — the category
// is said ONCE, as the group's label, instead of being glued into all
// thirty option labels.
func TestTheFormatStepGroupsByCategory(t *testing.T) {
	w := open(t, graph.AllEngines(), "tr", "photo.png")
	groups := targetGroups(t, w.last)
	if len(groups) < 3 {
		t.Fatalf("a png reaches several categories; got %d group(s)", len(groups))
	}
	want := map[string]string{
		CategoryField(formats.Image, ""):    "Görsel",
		CategoryField(formats.Video, ""):    "Video",
		CategoryField(formats.Document, ""): "Belge",
	}
	seen := map[string]bool{}
	for _, g := range groups {
		seen[g.Key] = true
		if lbl, ok := want[g.Key]; ok && g.Label != lbl {
			t.Errorf("group %q is headed %q, not %q", g.Key, g.Label, lbl)
		}
		if len(g.Options) == 0 {
			t.Errorf("group %q has no buttons; it should not be on the screen at all", g.Key)
		}
		for _, o := range g.Options {
			if strings.Contains(o.Label, "·") {
				t.Errorf("button %q still carries the category glue; the group's heading says it", o.Label)
			}
			if o.Label == "" || o.Value == "" {
				t.Errorf("a button with no label or value: %+v", o)
			}
		}
	}
	for key := range want {
		if !seen[key] {
			t.Errorf("no group %q on the screen", key)
		}
	}
	// nothing is pre-selected: the first press is the answer
	if _, vals := formOf(t, w.last); len(vals) != 0 {
		t.Errorf("the format step pre-selects %v; a press is what answers it", vals)
	}
}

func TestOpenWithEngines(t *testing.T) {
	w := open(t, graph.AllEngines(), "tr", "photo.png")
	opts := targetOptions(t, w.last)
	want := map[string]bool{"jpg": true, "webp": true, "avif": true, "ico": true, "pdf": true, "mp4": true}
	for x := range want {
		if !contains(opts, x) {
			t.Errorf("png with every engine should offer %s: %v", x, opts)
		}
	}
	if contains(opts, "docx") || contains(opts, "mp3") {
		t.Errorf("png must not offer office/audio targets: %v", opts)
	}
	for _, g := range targetGroups(t, w.last) {
		for _, o := range g.Options {
			if o.Value == "jpg" && (g.Label != "Görsel" || o.Label != "JPEG") {
				t.Errorf("jpg is %q under %q", o.Label, g.Label)
			}
			if o.Value == "pdf" && (g.Label != "Belge" || o.Label != "PDF") {
				t.Errorf("pdf is %q under %q", o.Label, g.Label)
			}
		}
	}
	// all engines present: no missing-engine note, no grey list
	for _, tx := range texts(w.last) {
		if strings.Contains(tx, "Not installed") {
			t.Error("no note expected when every engine is present")
		}
	}
	if len(listRows(w.last)) != 0 {
		t.Error("no grey list expected when every engine is present")
	}
}

func TestMultiSelectionIntersection(t *testing.T) {
	w := open(t, graph.AllEngines(), "en", "a.docx", "b.xlsx", "c.png")
	opts := targetOptions(t, w.last)
	if !contains(opts, "pdf") {
		t.Errorf("docx+xlsx+png all reach pdf: %v", opts)
	}
	if contains(opts, "docx") || contains(opts, "xlsx") || contains(opts, "mp4") {
		t.Errorf("only common targets: %v", opts)
	}
	if tx := texts(w.last)[0]; !strings.HasPrefix(tx, "3 files: 1 × Word (.docx), 1 × Excel (.xlsx), 1 × PNG | 3 dosya:") {
		t.Errorf("selection line %q", tx)
	}
}

func TestUnknownSourceBlocks(t *testing.T) {
	w := open(t, graph.AllEngines(), "en", "a.png", "weird.xyz")
	for _, n := range w.last.Nodes {
		if n.Type == "form" {
			t.Error("no form when a file cannot be converted")
		}
		if n.Type == "steps" {
			t.Error("nothing to walk through, so no step spine either")
		}
	}
	joined := strings.Join(texts(w.last), "\n")
	if !strings.Contains(joined, "unknown file type — weird.xyz") || !strings.Contains(joined, "bilinmeyen dosya türü — weird.xyz") {
		t.Errorf("texts %q", joined)
	}
	if len(w.last.Actions) != 1 || w.last.Actions[0].ID != ActionCancel {
		t.Errorf("actions %+v", w.last.Actions)
	}
}

func TestNoCommonTarget(t *testing.T) {
	// archives are reachable from anything, so two unrelated files still
	// share them; with engines an mp3 and a docx even share png
	w := open(t, graph.AllEngines(), "en", "a.mp3", "b.docx")
	opts := targetOptions(t, w.last)
	if !contains(opts, "zip") || !contains(opts, "png") || contains(opts, "docx") || contains(opts, "mp4") {
		t.Errorf("mp3+docx targets %v", opts)
	}
	// an AVIF without ImageMagick can only be packed; the list says why
	w = open(t, nil, "en", "a.avif")
	opts = targetOptions(t, w.last)
	for _, o := range opts {
		if o != "zip" && o != "tar" && o != "tgz" && o != "tzst" && o != "txz" {
			t.Errorf("avif without engines should only reach archives: %v", opts)
		}
	}
	if rows := strings.Join(listRows(w.last), "\n"); !strings.Contains(rows, "Image · PNG — needs ImageMagick") {
		t.Errorf("rows %s", rows)
	}
}

func TestEmptySelection(t *testing.T) {
	w := open(t, nil, "tr")
	if !strings.Contains(texts(w.last)[0], "Dönüştürmek için bir dosya seçin.") {
		t.Errorf("texts %v", texts(w.last))
	}
}

// ── walking the steps ──────────────────────────────────────────────────

// ⚠⚠ Pressing a format button only SELECTS it. It used to move the wizard
// on by itself, and in the v0.43.0 sweep that queued jpg→PDF, jpg→WebP and
// mp3→WAV without Convert ever being pressed: the person picked, reached
// for Next where it had been, and the review step's Convert was under the
// pointer by then. A press is an answer the step shows; Next moves on; the
// job starts only from the review step.
func TestAPressOnlySelects(t *testing.T) {
	for _, c := range []struct {
		engines map[string]bool
		file    string
		target  string
	}{
		{nil, "landscape.jpg", "pdf"},
		{nil, "landscape.jpg", "webp"},
		{map[string]bool{graph.FFmpeg: true}, "song.mp3", "wav"},
		{graph.AllEngines(), "clip.avi", "mp4"},
	} {
		w := open(t, c.engines, "tr", c.file)
		before := spine(t, w.last)
		s := w.choose(c.target)
		if s.Job != nil {
			t.Fatalf("%s → %s: a press queued a job: %+v", c.file, c.target, s.Job)
		}
		if got := active(t, s); got != StepFormat {
			t.Fatalf("%s → %s: a press moved the wizard to %q; it only selects", c.file, c.target, got)
		}
		if s.State[FieldTarget] != c.target {
			t.Fatalf("%s → %s: the answer is not kept: %v", c.file, c.target, s.State)
		}
		if ps := primaries(s); len(ps) != 1 || ps[0].Disabled || ps[0].Label["tr"] != "İleri" {
			t.Fatalf("%s → %s: the primary must be an enabled Next, not %+v", c.file, c.target, s.Actions)
		}
		if after := spine(t, s); strings.Join(after, ",") != strings.Join(before, ",") {
			t.Fatalf("%s → %s: the spine changed under the pointer:\nbefore %v\nafter  %v", c.file, c.target, before, after)
		}
		// Next leaves the format step and never queues…
		if s := w.next(nil); s.Job != nil || active(t, s) == StepFormat {
			t.Fatalf("%s → %s: Next from the format step must move on and queue nothing: %+v", c.file, c.target, s)
		}
		// …and only the review step's own button does.
		w.toReview()
		if a := primaries(w.last); len(a) != 1 || a[0].Label["tr"] != "Dönüştür" {
			t.Fatalf("the review's primary is Convert: %+v", w.last.Actions)
		}
		if s := w.next(nil); s.Job == nil {
			t.Fatalf("%s → %s: Convert on the review step queues the job", c.file, c.target)
		}
	}
}

// The spine is the selection's, not the format's: a route with nothing to
// set keeps its Settings step on the strip — labelled "not needed" once the
// person has left the format step — instead of the strip shrinking.
func TestTheSpineKeepsAStepItDoesNotNeed(t *testing.T) {
	w := open(t, nil, "tr", "landscape.jpg")
	before := spine(t, w.last)
	if strings.Join(before, ",") != "format:active,settings:todo,review:todo" {
		t.Fatalf("a jpg opens on %v", before)
	}
	w.choose("pdf")
	if got := strings.Join(spineLabels(t, w.last, "tr"), ","); got != "Biçim,Ayarlar,Gözden geçir" {
		t.Fatalf("on the format step the labels stay plain while a format is pressed: %v", got)
	}
	s := w.next(nil)
	if got := strings.Join(spine(t, s), ","); got != "format:done,settings:done,review:active" {
		t.Fatalf("jpg → pdf has no knobs: Next lands on the review with the same three steps, got %v", got)
	}
	labels := spineLabels(t, s, "tr")
	if labels[1] != "Ayarlar (gerekmiyor)" {
		t.Fatalf("the step it walked past must say why, got %q", labels[1])
	}
	if en := spineLabels(t, s, "en")[1]; en != "Settings (not needed)" {
		t.Fatalf("en label %q", en)
	}
	// Back from the review skips it again, to the format step
	if got := active(t, w.back()); got != StepFormat {
		t.Fatalf("Back from the review is the format step, got %q", got)
	}
}

// A second press in a DIFFERENT group leaves one button lit, not two: the
// browser keeps what the person pressed, so the groups are redrawn under
// keys that carry the new answer (CategoryField).
func TestASecondPickLightsOneButton(t *testing.T) {
	w := open(t, nil, "en", "photo.png")
	first := w.choose("jpg")
	_, vals := formOf(t, first)
	if len(vals) != 1 || vals[CategoryField(formats.Image, "jpg")] != "jpg" {
		t.Fatalf("after the first press exactly JPEG is lit: %v", vals)
	}
	oldKeys := map[string]bool{}
	for _, g := range targetGroups(t, first) {
		oldKeys[g.Key] = true
	}
	second := w.choose("pdf")
	if second.State[FieldTarget] != "pdf" {
		t.Fatalf("the second press is the answer: %v", second.State)
	}
	_, vals = formOf(t, second)
	if len(vals) != 1 || vals[CategoryField(formats.Document, "pdf")] != "pdf" {
		t.Fatalf("after the second press exactly PDF is lit: %v", vals)
	}
	for _, g := range targetGroups(t, second) {
		if oldKeys[g.Key] {
			t.Fatalf("group %q kept its key, so the browser keeps the old highlight in it", g.Key)
		}
	}
	// the stale value the browser still holds for the old key is not a press
	in := event("change", nil, "en", "photo.png")
	in.State = second.State
	in.Data = map[string]any{"values": map[string]any{
		CategoryField(formats.Image, "jpg"):    "jpg",
		CategoryField(formats.Document, "pdf"): "pdf",
	}}
	s, err := Handle(in)
	if err != nil {
		t.Fatal(err)
	}
	if s.State[FieldTarget] != "pdf" {
		t.Fatalf("an old highlight under an old key changed the answer: %v", s.State)
	}
}

// Several new answers in one event are not an answer: a browser sends one
// per press, so several means a client that filled every group (filex's
// e2e `autoFill` did, and the wizard took MP4 for a PNG → JPEG).
func TestSeveralAnswersAtOnceChangeNothing(t *testing.T) {
	everyGroup := func(s *wire.Surface) map[string]any {
		vals := map[string]any{}
		for _, g := range targetGroups(t, s) {
			vals[g.Key] = g.Options[0].Value
		}
		return vals
	}
	w := open(t, graph.AllEngines(), "en", "photo.png")
	w.choose("jpg")
	all := everyGroup(w.last)
	all[CategoryField(formats.Image, "jpg")] = "jpg" // the lit one, as drawn
	s := w.next(all)
	if s.State[FieldTarget] != "jpg" {
		t.Fatalf("every group's first button at once replaced the answer: %v", s.State)
	}
	if active(t, s) == StepFormat {
		t.Fatalf("Next with the answer kept moves on")
	}

	// With no answer yet, several at once are still none: the step says so.
	w = open(t, graph.AllEngines(), "en", "photo.png")
	s = w.next(everyGroup(w.last))
	if s.Job != nil || active(t, s) != StepFormat || s.State[FieldTarget] != "" {
		t.Fatalf("several answers at once were taken as one: %v", s.State)
	}
}

// A format's label is a NAME ("PNG") or a description ("Plain text"); a
// description is translated. And the route is in words: no engine ids and
// no "(go)", which is this plugin's implementation language.
func TestTheTurkishScreenSaysItInTurkish(t *testing.T) {
	w := open(t, nil, "tr", "notes.md")
	var labels []string
	for _, g := range targetGroups(t, w.last) {
		for _, o := range g.Options {
			labels = append(labels, o.Label)
		}
	}
	joined := strings.Join(labels, " | ")
	for _, english := range []string{"Plain text", "Icon", "Animated", "Rich Text", "Spreadsheet", "Presentation", "OpenDocument Text"} {
		if strings.Contains(joined, english) {
			t.Errorf("an English description on the Turkish format step: %q in %s", english, joined)
		}
	}
	if !strings.Contains(joined, "Düz metin (.txt)") {
		t.Errorf("txt should read Düz metin (.txt): %s", joined)
	}
	w.pick("pdf")
	s := w.toReview()
	route := reviewRows(t, s)["route"]
	if route == "" {
		t.Fatalf("the review has no route row: %v", reviewRows(t, s))
	}
	if strings.Contains(route, "(go)") || strings.Contains(route, "md →") {
		t.Errorf("the route must be in words, not ids and the implementation language: %q", route)
	}
	if !strings.Contains(route, "| Markdown → PDF (yerleşik)") {
		t.Errorf("tr route %q", route)
	}
	if !strings.HasPrefix(route, "Markdown → PDF (built in) |") {
		t.Errorf("en route %q", route)
	}
}

// The owner's decision (v0.43.0): the converter ships Spanish, German and
// French too. Every string the wizard picks for ONE language — a group
// heading, a format button, a knob's label and help, a placeholder, a list
// column — is that language's, in the register the filex language packs use.
//
// ⚠ The host still collapses every locale but Turkish to `en` before it
// calls an app (filex feat/043-srvtext fixes that); this calls with the
// locale set directly.
func TestTheWizardSpeaksSpanishGermanAndFrench(t *testing.T) {
	cases := []struct {
		lang, txt, image, next, builtIn, quality, pages string
	}{
		{"es", "Texto sin formato (.txt)", "Imagen", "Siguiente", "integrado", "Calidad", "todas"},
		{"de", "Reiner Text (.txt)", "Bild", "Weiter", "integriert", "Qualität", "alle"},
		{"fr", "Texte brut (.txt)", "Image", "Suivant", "intégré", "Qualité", "toutes"},
	}
	for _, c := range cases {
		t.Run(c.lang, func(t *testing.T) {
			// A Markdown file: the text group offers .txt, described in words.
			w := open(t, nil, c.lang, "notes.md")
			var labels []string
			for _, g := range targetGroups(t, w.last) {
				for _, o := range g.Options {
					labels = append(labels, o.Label)
				}
			}
			if joined := strings.Join(labels, " | "); !strings.Contains(joined, c.txt) || strings.Contains(joined, "Plain text") {
				t.Errorf("the .txt button should read %q: %s", c.txt, joined)
			}
			if ps := primaries(w.last); len(ps) != 1 || ps[0].Label[c.lang] != c.next {
				t.Errorf("the footer's primary should read %q: %+v", c.next, ps)
			}
			w.pick("pdf")
			s := w.toReview()
			if route := i18n.Pick(c.lang, reviewText(t, s, "route")); !strings.Contains(route, "("+c.builtIn+")") {
				t.Errorf("the route should say %q: %q", c.builtIn, route)
			}

			// A photo: the image group's heading and a knob's label and help.
			w = open(t, graph.AllEngines(), c.lang, "photo.png")
			if g, ok := findGroup(targetGroups(t, w.last), CategoryField(formats.Image, "")); !ok || g.Label != c.image {
				t.Errorf("the image group should be headed %q: %+v", c.image, g)
			}
			s = w.pick("jpg")
			q := fieldByKey(t, s, "quality")
			if q.Label != c.quality || q.Help == "" || q.Help == i18n.S("en", "option.quality.help") {
				t.Errorf("the quality knob should be labelled %q with its help in %s: %+v", c.quality, c.lang, q)
			}

			// A PDF page range: the placeholder is a word in the language.
			w = open(t, graph.AllEngines(), c.lang, "doc.pdf")
			s = w.pick("png")
			if p := fieldByKey(t, s, "pages"); p.Placeholder != c.pages {
				t.Errorf("the pages placeholder should be %q: %q", c.pages, p.Placeholder)
			}
		})
	}
}

// reviewText is one review row's value, all languages.
func reviewText(t *testing.T, s *wire.Surface, id string) wire.Text {
	t.Helper()
	for _, n := range s.Nodes {
		if n.Type != "list" {
			continue
		}
		rows, _ := n.Props["rows"].([]map[string]any)
		for _, r := range rows {
			if r["id"] == id {
				v, _ := r["cells"].(map[string]any)["value"].(wire.Text)
				return v
			}
		}
	}
	t.Fatalf("no %q row in the review", id)
	return nil
}

func findGroup(groups []wire.Field, key string) (wire.Field, bool) {
	for _, g := range groups {
		if g.Key == key {
			return g, true
		}
	}
	return wire.Field{}, false
}

// After Next, the settings step carries the route's knobs.
func TestTheSettingsStepCarriesTheRoutesKnobs(t *testing.T) {
	w := open(t, graph.AllEngines(), "en", "clip.avi")
	s := w.pick("mp4")
	if got := active(t, s); got != StepSettings {
		t.Fatalf("Next after picking mp4 is the settings step, not %q", got)
	}
	fields, vals := formOf(t, s)
	keys := []string{}
	for _, f := range fields {
		keys = append(keys, f.Key)
	}
	if strings.Join(keys, ",") != "preset,crf,max_height" {
		t.Errorf("fields %v", keys)
	}
	if vals["preset"] != "medium" || vals["crf"] != 23 {
		t.Errorf("values %v", vals)
	}
	if s.State[FieldTarget] != "mp4" || s.State[StateStep] != StepSettings {
		t.Errorf("state %v", s.State)
	}
	if ps := primaries(s); len(ps) != 1 || ps[0].Disabled {
		t.Errorf("one primary, enabled: %+v", s.Actions)
	}
	// the preset select carries the video variant choices
	for _, f := range fields {
		if f.Key == "preset" {
			if len(f.Options) != 5 || f.Options[0].Value != "copy" {
				t.Errorf("preset options %v", f.Options)
			}
		}
		if f.Key == "crf" && (f.Min == nil || *f.Max != 51) {
			t.Errorf("crf range %v %v", f.Min, f.Max)
		}
	}
	if !strings.Contains(strings.Join(texts(s), "\n"), "AVI → MP4 (FFmpeg)") {
		t.Errorf("route line missing: %v", texts(s))
	}
}

func TestPdfSelfTargetKnobs(t *testing.T) {
	w := open(t, graph.AllEngines(), "en", "big.pdf")
	for _, g := range targetGroups(t, w.last) {
		for _, o := range g.Options {
			if o.Value == "pdf" && !strings.HasSuffix(o.Label, "(re-encode)") {
				t.Errorf("self target label %q", o.Label)
			}
		}
	}
	s := w.pick("pdf")
	fields, vals := formOf(t, s)
	found := false
	for _, f := range fields {
		if f.Key == "preset" {
			found = true
			if len(f.Options) != 4 || f.Options[1].Value != "ebook" {
				t.Errorf("pdf preset choices %v", f.Options)
			}
		}
	}
	if !found || vals["preset"] != "ebook" {
		t.Errorf("pdf preset default: found=%v vals=%v", found, vals)
	}
}

// The answers live in the state, so a step drawn from the state alone — no
// values echoed at all — still knows what was chosen.
func TestStateSurvivesWithoutValues(t *testing.T) {
	in := event("change", graph.AllEngines(), "en", "a.png")
	in.State = map[string]any{StateStep: StepSettings, FieldTarget: "jpg"}
	s, err := Handle(in)
	if err != nil {
		t.Fatal(err)
	}
	if _, vals := formOf(t, s); vals["quality"] != 85 {
		t.Errorf("values %v", vals)
	}
	if s.State[FieldTarget] != "jpg" {
		t.Errorf("state %v", s.State)
	}
}

// Back walks the same steps backwards, and the answers survive the trip.
func TestBackWalksTheStepsBackwards(t *testing.T) {
	w := open(t, graph.AllEngines(), "en", "a.png")
	w.pick("jpg")
	if got := active(t, w.last); got != StepSettings {
		t.Fatalf("after a press and Next: %q", got)
	}
	w.next(map[string]any{"quality": float64(60)})
	if got := active(t, w.last); got != StepReview {
		t.Fatalf("after Next: %q", got)
	}
	if rows := reviewRows(t, w.last); !strings.HasPrefix(rows["quality"], "60 |") {
		t.Errorf("the review must show what was answered: %v", rows)
	}
	s := w.back()
	if got := active(t, s); got != StepSettings {
		t.Fatalf("Back from the review is the settings step, not %q", got)
	}
	if _, vals := formOf(t, s); vals["quality"] != float64(60) {
		t.Errorf("the answer did not survive Back: %v", vals)
	}
	s = w.back()
	if got := active(t, s); got != StepFormat {
		t.Fatalf("Back again is the format step, not %q", got)
	}
	if !strings.Contains(strings.Join(texts(s), "\n"), "Chosen: JPEG") {
		t.Errorf("the format step should say what is chosen: %v", texts(s))
	}
	// …and the primary is live again, because the answer is still there
	if ps := primaries(s); len(ps) != 1 || ps[0].Disabled {
		t.Errorf("primary %+v", s.Actions)
	}
}

// Every step: a spine with exactly one active step, and at most one
// primary button — one step asks one thing.
func TestEveryStepHasASpineAndOneQuestion(t *testing.T) {
	w := open(t, graph.AllEngines(), "tr", "a.png", "b.png")
	var seen []string
	for {
		s := w.last
		here := active(t, s)
		seen = append(seen, here)
		if ps := primaries(s); len(ps) != 1 {
			t.Fatalf("step %q has %d primary buttons: %+v", here, len(ps), s.Actions)
		}
		for _, a := range s.Actions {
			if !a.Primary && a.ID != ActionBack && a.ID != ActionCancel {
				t.Fatalf("step %q carries a third button %q", here, a.ID)
			}
		}
		if len(s.Actions) > 2 {
			t.Fatalf("step %q has %d buttons; one primary plus Back is the whole footer", here, len(s.Actions))
		}
		if here == StepReview {
			break
		}
		if len(seen) > 6 {
			t.Fatalf("the wizard does not end: %v", seen)
		}
		if here == StepFormat {
			w.pick("mp4")
			continue
		}
		w.next(nil)
	}
	if strings.Join(seen, ">") != "format>combine>settings>review" {
		t.Errorf("steps walked: %v", seen)
	}
}

// ── combining ──────────────────────────────────────────────────────────

// "One file or one for each" is a step of its own, and it is two buttons:
// a bool with no Style is drawn as a lone tickbox, which is not a question
// anybody reads.
func TestCombineIsAStepOfItsOwnWithTwoButtons(t *testing.T) {
	w := open(t, nil, "en", "a.png", "b.jpg")
	s := w.pick("pdf")
	if got := active(t, s); got != StepCombine {
		t.Fatalf("two files and a target that merges: the next step is combine, not %q", got)
	}
	f := fieldByKey(t, s, options.Merge)
	if f.Type != "bool" || f.Style != "choice" {
		t.Fatalf("merge is %q/%q; a decision is two buttons (style choice)", f.Type, f.Style)
	}
	if len(f.Options) != 2 || f.Options[0].Value != "true" || f.Options[1].Value != "false" {
		t.Fatalf("both answers must be written out: %+v", f.Options)
	}
	for _, o := range f.Options {
		if o.Label == "" {
			t.Fatalf("an answer with no words: %+v", o)
		}
	}
	// a single file never sees the step
	w = open(t, nil, "en", "a.png")
	if got := active(t, w.pick("pdf")); got == StepCombine {
		t.Error("one file, nothing to combine")
	}
	for _, it := range spine(t, w.last) {
		if strings.HasPrefix(it, StepCombine+":") {
			t.Errorf("one file: Combine is not on the spine at all: %v", spine(t, w.last))
		}
	}
}

func TestCombineAnswerReachesTheJob(t *testing.T) {
	w := open(t, map[string]bool{graph.FFmpeg: true}, "en", "a.png", "b.jpg")
	w.pick("gif")
	if got := active(t, w.last); got != StepCombine {
		t.Fatalf("step %q", got)
	}
	w.next(map[string]any{options.Merge: true})
	if got := active(t, w.last); got != StepSettings {
		t.Fatalf("step %q", got)
	}
	if rows := reviewRows(t, w.next(map[string]any{"duration": float64(2)})); rows[options.Merge] == "" {
		t.Errorf("the review must say which way it was answered: %v", rows)
	}
	s := w.next(nil)
	if s.Job == nil || s.Job.Params[options.Merge] != true || s.Job.Params["duration"] != float64(2) {
		t.Fatalf("job %+v", s.Job)
	}
	// …and answering "one for each" keeps merge out of the job
	w = open(t, nil, "en", "a.png", "b.jpg")
	if got := active(t, w.pick("pdf")); got != StepCombine {
		t.Fatalf("pdf merges in pure Go: %q", got)
	}
	w.next(map[string]any{options.Merge: false})
	w.toReview()
	if s := w.next(nil); s.Job == nil || s.Job.Params[options.Merge] != nil {
		t.Fatalf("answered 'one for each', so the job must not carry merge: %+v", s.Job)
	}
}

// ── the last step ──────────────────────────────────────────────────────

func TestTheReviewSaysWhatIsAboutToHappen(t *testing.T) {
	w := open(t, graph.AllEngines(), "tr", "clip.avi")
	w.pick("mp4")
	rows := reviewRows(t, w.next(map[string]any{"preset": "slow", "crf": float64(20), "max_height": float64(720)}))
	for id, want := range map[string]string{
		"files":      "clip.avi (AVI)",
		"target":     "MP4",
		"preset":     "Yavaş (en küçük dosya)",
		"crf":        "20",
		"max_height": "720",
		"route":      "AVI → MP4 (FFmpeg)",
	} {
		if !strings.Contains(rows[id], want) {
			t.Errorf("the review's %q row is %q, and should carry %q", id, rows[id], want)
		}
	}
	if a := w.last.Actions[len(w.last.Actions)-1]; a.Label["tr"] != "Dönüştür" || !a.Primary {
		t.Errorf("the last step's primary is Convert: %+v", a)
	}
}

func TestSubmitQueuesJobFromTheLastStep(t *testing.T) {
	w := open(t, graph.AllEngines(), "en", "a.png", "b.bmp")
	w.pick("jpg")
	w.next(map[string]any{"quality": float64(70)})
	if got := active(t, w.last); got != StepReview {
		t.Fatalf("step %q", got)
	}
	s := w.next(nil)
	if s.Job == nil || s.Job.ActionID != ActionID {
		t.Fatalf("no job in %+v", s)
	}
	if s.Job.Params["target"] != "jpg" || s.Job.Params["quality"] != float64(70) {
		t.Errorf("params %v", s.Job.Params)
	}
	if _, leaked := s.Job.Params["crf"]; leaked {
		t.Error("only the route's knobs go into params")
	}
}

// A submit is the step's Next everywhere but the last step: nothing is
// queued from the middle of the wizard.
func TestNoJobBeforeTheLastStep(t *testing.T) {
	w := open(t, graph.AllEngines(), "en", "a.png")
	if s := w.choose("jpg"); s.Job != nil {
		t.Fatalf("choosing a format must not queue anything: %+v", s.Job)
	}
	if s := w.next(nil); s.Job != nil {
		t.Fatalf("Next from the format step must not queue anything: %+v", s.Job)
	}
	if s := w.next(nil); s.Job != nil {
		t.Fatalf("the settings step must not queue anything: %+v", s.Job)
	}
	if active(t, w.last) != StepReview {
		t.Fatalf("step %q", active(t, w.last))
	}
	if s := w.next(nil); s.Job == nil {
		t.Fatalf("the last step queues: %+v", s)
	}
}

func TestSubmitWithoutTarget(t *testing.T) {
	in := event("submit", nil, "tr", "a.png")
	in.ActionID = ActionSubmit
	s, err := Handle(in)
	if err != nil {
		t.Fatal(err)
	}
	if s.Job != nil {
		t.Errorf("nothing was chosen, so nothing may be queued: %+v", s.Job)
	}
	if active(t, s) != StepFormat {
		t.Errorf("it must stay on the format step, not move to %q", active(t, s))
	}
	if !strings.Contains(strings.Join(texts(s), "\n"), "Bir hedef biçim seçin.") {
		t.Errorf("and it must say why: %v", texts(s))
	}
}

func TestSubmitInvalidKnob(t *testing.T) {
	w := open(t, nil, "en", "a.png")
	w.pick("jpg")
	s := w.next(map[string]any{"quality": "abc"})
	if s.Job != nil || s.Errors["quality"] == nil {
		t.Errorf("surface %+v", s)
	}
	if active(t, s) != StepSettings {
		t.Errorf("a refused answer keeps the person on the step that asked: %q", active(t, s))
	}
	if s.Errors["quality"]["tr"] != "Tam sayı girin." {
		t.Errorf("tr error %v", s.Errors["quality"])
	}
}

func TestStaleTargetIsDropped(t *testing.T) {
	// a target that is not reachable any more (engine map changed) is reset
	in := event("change", nil, "en", "a.png")
	in.State = map[string]any{StateStep: StepSettings, FieldTarget: "avif"}
	s, err := Handle(in)
	if err != nil {
		t.Fatal(err)
	}
	if s.State[FieldTarget] != "" {
		t.Errorf("state %v", s.State)
	}
	if active(t, s) != StepFormat {
		t.Errorf("with no answer left, the wizard is back at the first step, not %q", active(t, s))
	}
}

func TestCancel(t *testing.T) {
	in := event("action", nil, "en", "a.png")
	in.ActionID = ActionCancel
	s, _ := Handle(in)
	if !s.Done {
		t.Error("cancel should close")
	}
}

// ── the knobs ──────────────────────────────────────────────────────────

// Every bool says how it is drawn. A bool with no Style is left to the
// client's guess, and the guess is a tickbox.
func TestEveryBooleanKnobIsTwoButtons(t *testing.T) {
	bools := 0
	for key, d := range options.Defs {
		if d.Type != "bool" {
			continue
		}
		bools++
		f := knobField(key, "", "en")
		if f.Style != "choice" {
			t.Errorf("%s is drawn as %q; a decision is two buttons", key, f.Style)
		}
		if len(f.Options) != 2 {
			t.Errorf("%s offers %d answers; a bool has two, and both are written out", key, len(f.Options))
		}
		for _, o := range f.Options {
			if o.Label == "" || (o.Value != "true" && o.Value != "false") {
				t.Errorf("%s: %+v is not one of the two answers", key, o)
			}
		}
	}
	if bools == 0 {
		t.Fatal("no boolean knob in the catalogue — this test is measuring nothing")
	}
}

// A knob's label and its help travel in the language the call asked for
// (they are plain strings on the wire), and neither may be missing — in any
// of the languages the converter ships.
func TestEveryKnobIsExplainedInEveryLanguage(t *testing.T) {
	for _, key := range options.Order {
		for _, locale := range i18n.Langs {
			f := knobField(key, "", locale)
			if strings.TrimSpace(f.Label) == "" {
				t.Errorf("%s has no %s label", key, locale)
			}
			if strings.TrimSpace(f.Help) == "" {
				t.Errorf("%s has no %s help; every other knob explains itself", key, locale)
			}
		}
	}
}

// What the server lacks is the administrator's to fix, so only an
// administrator is told: the note naming the missing engines and the grey
// list of formats each would unlock. Everybody else sees the formats they
// CAN have and nothing about server programs (owner's rule for what depends
// on the server's setup: greyed with the reason for an admin, hidden for
// others — v0.43.0 wave 2, 2026-09-22).
func TestMissingEnginesAreTheAdministratorsBusiness(t *testing.T) {
	as := func(role string) *wire.Surface {
		in := event("open", map[string]bool{}, "en", "photo.png")
		in.Context.Actor = &wire.Actor{ID: 7, Role: role}
		s, err := Handle(in)
		if err != nil {
			t.Fatal(err)
		}
		return s
	}
	missingNote := func(s *wire.Surface) bool {
		for _, tx := range texts(s) {
			if strings.Contains(tx, "Not installed") || strings.Contains(tx, "engine missing") {
				return true
			}
		}
		return false
	}

	admin := as("admin")
	if !missingNote(admin) || len(listRows(admin)) == 0 {
		t.Fatalf("an administrator is told what is missing and what it costs:\n%v\n%v", texts(admin), listRows(admin))
	}
	for _, role := range []string{"user", "viewer"} {
		s := as(role)
		if missingNote(s) {
			t.Errorf("a %s account is not told which engines the server lacks:\n%v", role, texts(s))
		}
		if rows := listRows(s); len(rows) != 0 {
			t.Errorf("a %s account gets no grey list of formats it cannot have: %v", role, rows)
		}
		// ...and still gets every format it CAN have.
		if got, want := targetOptions(t, s), targetOptions(t, admin); strings.Join(got, ",") != strings.Join(want, ",") {
			t.Errorf("a %s account is offered %v, the administrator %v", role, got, want)
		}
	}
}

// Burak, 2026-09-22: "Dönüştür…" on a read-only storage asks where the
// result should go. The Where step is on the strip exactly when a selected
// file is read-only, starts at filex's default for the person (context.home),
// and the job names the chosen folder (Output{Mode: "folder"}) — filex checks
// it. A selection that can be written beside keeps the manifest's output.
func TestReadOnlySourceAsksWhereTheResultGoes(t *testing.T) {
	readOnly := func(home string) func(*wire.ViewEventInput) {
		return func(in *wire.ViewEventInput) {
			for i := range in.Context.Inputs {
				in.Context.Inputs[i].ReadOnly = true
			}
			in.Context.Home = home
		}
	}
	chooserOf := func(s *wire.Surface) (wire.Node, bool) {
		for _, n := range s.Nodes {
			if n.Type == "file-chooser" {
				return n, true
			}
		}
		return wire.Node{}, false
	}

	w := &wiz{t: t, engines: map[string]bool{}, locale: "en", names: []string{"photo.png"}, ctx: readOnly("depo://")}
	w.send("open", "", nil)
	if !strings.Contains(strings.Join(spine(t, w.last), ","), StepWhere) {
		t.Fatalf("a read-only source puts Where on the strip from the start: %v", spine(t, w.last))
	}
	w.pick("pdf")
	for w.step() != StepWhere {
		if w.step() == StepReview {
			t.Fatal("the wizard went past Where")
		}
		w.next(nil)
	}
	n, ok := chooserOf(w.last)
	if !ok || n.ID != FieldDest || n.Props["kind"] != "dir" || n.Props["value"] != "depo://" {
		t.Fatalf("Where asks for a folder, starting at the person's home: %+v", n)
	}
	// The person picks another folder.
	w.next(map[string]any{FieldDest: "arsiv2://reports"})
	if w.step() != StepReview {
		t.Fatalf("after Where comes the review, got %q", w.step())
	}
	if rows := fmt.Sprint(reviewRows(t, w.last)); !strings.Contains(rows, "arsiv2://reports") {
		t.Fatalf("the review says where the result goes:\n%s", rows)
	}
	s := w.next(nil)
	if s.Job == nil || s.Job.Output == nil || s.Job.Output.Mode != "folder" || s.Job.Output.Dir != "arsiv2://reports" {
		t.Fatalf("the job names the chosen folder: %+v", s.Job)
	}

	// No home and no choice: Where does not move on; it says why.
	w = &wiz{t: t, engines: map[string]bool{}, locale: "en", names: []string{"photo.png"}, ctx: readOnly("")}
	w.send("open", "", nil)
	w.pick("pdf")
	for w.step() != StepWhere {
		w.next(nil)
	}
	w.next(nil)
	if w.step() != StepWhere || !strings.Contains(strings.Join(texts(w.last), "\n"), "Choose a folder for the result.") {
		t.Fatalf("with no folder the step stays and says so (on %q): %v", w.step(), texts(w.last))
	}

	// A writable source: no Where, and the job keeps the manifest's output.
	w = open(t, map[string]bool{}, "en", "photo.png")
	if strings.Contains(strings.Join(spine(t, w.last), ","), StepWhere) {
		t.Fatalf("a writable source asks no Where: %v", spine(t, w.last))
	}
	w.pick("pdf")
	w.toReview()
	if s := w.next(nil); s.Job == nil || s.Job.Output != nil {
		t.Fatalf("beside the source, as the manifest says: %+v", s.Job)
	}
}
