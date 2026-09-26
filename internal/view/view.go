// Package view builds the Convert screen: the wizard a person walks to say
// what their files should become.
//
// ⚠ It is a WIZARD, not a form. One surface carrying a target picker, the
// knobs of whatever route was picked, a route note, an engine note and a
// grey list asks four things at once — "formlar karışık, adım adım
// ilerlemiyor". So the screen has a spine (the `steps` node) and every step
// asks ONE thing, with one primary button plus Back:
//
//	format    what should it become — every reachable format as a button
//	          under its own category heading; the category is the heading,
//	          not a prefix glued into thirty labels
//	combine   one output for the whole selection or one per file — asked
//	          only when several files were picked and the target merges
//	settings  the knobs this route honours, all of them: there is no
//	          "advanced" section and there never will be one
//	review    what is about to happen, and the button that starts it
//
// ⚠⚠ Pressing a format button ONLY selects it. It used to carry the wizard
// on by itself, and the v0.43.0 sweep measured what that does to a person:
// they pick PDF, reach for "Next" where it was, and the button under the
// pointer is by then the review step's "Convert" — jpg→PDF, jpg→WebP and
// mp3→WAV were queued without Convert ever being pressed on purpose. So a
// pick is an answer the step shows ("Chosen: PDF"), Next moves on, and the
// job starts only from the review step's own button.
//
// ⚠ The spine does not change under the pointer either. Which steps there
// are is decided by the SELECTION (several files that can be combined →
// Combine; any reachable route with knobs → Settings), never by the format
// picked: picking used to shrink the strip from three steps to two while
// the person was looking at it. A step this conversion does not need is
// still on the strip, walked past and labelled "not needed", so no screen
// is one a person has to press Next through with nothing on it.
//
// It is pure: no host function is called, everything comes from the event
// (the selection, the engine map, the echoed state and the form's values),
// so it is unit-tested on the host like the graph.
//
// ⚠⚠ Not the locale. Every word on these screens travels as a Text — every
// language at once — and the reader's own filex picks one, INCLUDING the
// words a field carries: a field's label, help and placeholder and an
// option's label are written as {en, tr, …} maps (wire.Field.I18n,
// wire.FieldOption.LabelI18n), never as one string chosen for the call.
// The call's locale is the host's GUESS at the reader's language (the
// account's saved language first, the request's Accept-Language second), and
// an embedded filex is where the guess is wrong: a web component mounted
// with `locale: "tr"` on an account still saved as English drew this wizard
// with Turkish steps, headings and buttons around English category headings,
// format names, knob labels and help ("Image", "Plain text (.txt)",
// "Quality — 1–100; higher is larger and sharper"), because those were the
// strings picked for `en` (Burak, 2026-09-26: "converter'da bazı yerler
// İngilizce kalıyor"). TestTheScreenIsTheReadersLanguageNotTheCalls
// (internal/app) draws every step with the call in one language and reads it
// in the other.
package view

import (
	"sort"
	"strconv"
	"strings"

	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"

	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/i18n"
	"github.com/brf-tech/filex-convert/internal/job"
	"github.com/brf-tech/filex-convert/internal/options"
)

// ActionID is the action a submit queues.
const ActionID = "convert"

// FieldTarget is the key the chosen format travels under: in the wizard's
// state and in the job params. The format step has no field by this name —
// it has one button group per category (CategoryField), because a wire
// select has no groups of its own.
const FieldTarget = "target"

// groupSep joins a category group's key to the answer it was drawn around;
// see CategoryField.
const groupSep = "__"

// StateStep is the state key that says which step the person is on.
const StateStep = "step"

// The steps, in the order they are walked.
const (
	StepFormat   = "format"
	StepCombine  = "combine"
	StepSettings = "settings"
	StepWhere    = "where"
	StepReview   = "review"
)

// FieldDest is the folder the result goes to when it cannot go beside its
// source: the `file-chooser` node's id on the Where step, and the state key
// the answer is kept under.
const FieldDest = "dest"

// Footer button ids.
//
// ⚠ The primary is "submit" on every step, and that is not sloppiness: the
// renderer posts `submit` for a primary button and `action` for every other
// one, so the primary IS the step's submit. Which submit it is — "next" or
// "convert" — is the step the state carries, never the button's id.
const (
	ActionSubmit = "submit"
	ActionBack   = "back"
	ActionCancel = "cancel"
)

// CategoryField is the key of one category's button group on the format
// step, drawn while `chosen` is the answer ("" before the first pick).
//
// ⚠⚠ The key carries the answer, and that is not decoration. The browser
// keeps what the person pressed over what a `change` answer echoes (they
// may have typed on while the request was out), so a group the person
// pressed keeps its highlight even after the plugin redraws it empty: pick
// PNG under Image, then PDF under Document, and both buttons stay lit —
// two answers on a step that has one. Keyed by the answer, every pick
// redraws the groups under NEW keys; the stale highlights belong to keys no
// field carries any more, and the one lit button is the one the state says.
func CategoryField(c formats.Category, chosen string) string {
	if chosen == "" {
		return FieldTarget + "_" + string(c)
	}
	return FieldTarget + "_" + string(c) + groupSep + chosen
}

// nameOf is a format id's label in the given language.
func nameOf(id, locale string) string {
	if f, ok := formats.ByID(id); ok {
		return f.Name(locale)
	}
	return id
}

// Every word on these screens comes from internal/i18n, in every language
// the converter ships (en, tr, es, de, fr), and reaches the screen as a Text:
// i18n.T for a message, i18n.Each for words put together from translated
// parts, and field/option for the texts a form carries. Nothing here picks
// one language (see the package comment for why).

// names is a format id's label in every language.
func names(id string) wire.Text {
	return i18n.Each(func(lang string) string { return nameOf(id, lang) })
}

// field fills a form field's words in every language: label, help and
// placeholder travel as maps the reader's filex resolves (Label, Help and
// Placeholder keep the English, for code that reads them as strings).
func field(f wire.Field, label, help, placeholder wire.Text) wire.Field {
	f.Label, f.Help, f.Placeholder = label["en"], help["en"], placeholder["en"]
	f.I18n = &wire.FieldI18n{Label: label, Help: help, Placeholder: placeholder}
	return f
}

// option is one button of a choice, its label in every language.
func option(value string, label wire.Text) wire.FieldOption {
	return wire.FieldOption{Value: value, Label: label["en"], LabelI18n: label}
}

// selection is what the event says about the files.
type selection struct {
	sources []string          // distinct source format ids, catalogue order
	counts  map[string]int    // per source id
	unknown []string          // names with an unknown extension
	engines map[string]bool   // present engines
	targets []string          // reachable by every source with present engines
	blocked map[string]string // targets reachable only with missing engines → engine list
	merge   []string          // targets that accept the whole selection as one output
	// admin: the person could install what is missing. Only they are told
	// which engines the server lacks and which formats that costs.
	admin bool
	// readOnly: a selected file is on a storage that takes no writes, so
	// the result cannot go beside it and the wizard asks where it goes
	// (filex offers the action there because the manifest's output says
	// `elsewhere`). home is filex's default for that answer.
	readOnly bool
	home     string
}

func analyse(in *wire.ViewEventInput) selection {
	sel := selection{counts: map[string]int{}, engines: in.Context.Engines, blocked: map[string]string{}, admin: canInstallEngines(in.Context.Actor),
		home: in.Context.Home}
	for _, f := range in.Context.Inputs {
		sel.readOnly = sel.readOnly || f.ReadOnly
	}
	if sel.engines == nil {
		sel.engines = map[string]bool{}
	}
	seen := map[string]bool{}
	for _, f := range in.Context.Inputs {
		fm, _, ok := formats.Detect(f.Name)
		if !ok {
			sel.unknown = append(sel.unknown, f.Name)
			continue
		}
		sel.counts[fm.ID]++
		if !seen[fm.ID] {
			seen[fm.ID] = true
			sel.sources = append(sel.sources, fm.ID)
		}
	}
	if len(sel.unknown) > 0 || len(sel.sources) == 0 {
		return sel
	}
	sel.targets = graph.Targets(sel.sources, sel.engines)
	have := map[string]bool{}
	for _, t := range sel.targets {
		have[t] = true
	}
	for _, t := range graph.Targets(sel.sources, graph.AllEngines()) {
		if have[t] {
			continue
		}
		need := map[string]bool{}
		for _, s := range sel.sources {
			if _, err := graph.Route(s, t, sel.engines); err != nil {
				if nre, ok := err.(*graph.NoRouteError); ok {
					for _, e := range nre.MissingEngines {
						need[e] = true
					}
				}
			}
		}
		var list []string
		for e := range need {
			list = append(list, e)
		}
		sort.Strings(list)
		sel.blocked[t] = strings.Join(graph.EngineNames(list), ", ")
	}
	if len(in.Context.Inputs) > 1 {
		for _, t := range job.MergeTargets(sel.sources) {
			ok := true
			for _, eng := range job.MergeEngines(t) {
				if !sel.engines[eng] {
					ok = false
				}
			}
			if ok {
				sel.merge = append(sel.merge, t)
			}
		}
	}
	return sel
}

// mergeKnobs are the knobs a merged conversion honours.
func mergeKnobs(target string) []string {
	switch target {
	case "mp4", "webm", "mkv":
		return []string{options.Duration, options.CRF}
	case "gif":
		return []string{options.Duration, options.MaxHeight}
	}
	return nil
}

// values merges the form's current values over the echoed state.
func values(in *wire.ViewEventInput) map[string]any {
	out := map[string]any{}
	for k, v := range in.State {
		out[k] = v
	}
	if in.Data != nil {
		if vals, ok := in.Data["values"].(map[string]any); ok {
			for k, v := range vals {
				out[k] = v
			}
		}
	}
	return out
}

// routeFor answers the union of knobs and the variant for the target over
// every source, and the route in words per language ("JPEG → PDF (built
// in)", "PNG → AVIF (ImageMagick)").
func routeFor(sel selection, target string) (keys []string, variant string, route []wire.Text) {
	seen := map[string]bool{}
	for _, s := range sel.sources {
		r, err := graph.Route(s, target, sel.engines)
		if err != nil {
			continue
		}
		route = append(route, i18n.Each(func(lang string) string {
			return r.Words(func(id string) string { return nameOf(id, lang) }, lang)
		}))
		for _, k := range r.Options() {
			seen[k] = true
		}
		if variant == "" {
			variant = r.Variant()
		}
	}
	for _, k := range options.Order {
		if seen[k] {
			keys = append(keys, k)
		}
	}
	return keys, variant, route
}

// ── the wizard's state ─────────────────────────────────────────────────

// wizard is what the screen carries between steps (Surface.State): the step
// the person is on, and every answer given so far.
//
// ⚠ The answers live HERE, not in the form's values: a footer press
// replaces the client's value map with whatever the next step declares, so
// an answer kept only in a field is gone the moment the person presses
// Next — and gone twice over when they press Back.
type wizard struct {
	Step   string
	Target string
	Merge  bool
	Knobs  map[string]any
	// Dest is the chosen folder (adapter-qualified) when the result cannot
	// go beside its source; filex's default for the person until they pick.
	Dest string
}

// read rebuilds the wizard from the event: the echoed state, the form's
// values over it, and the button just pressed over both.
func read(in *wire.ViewEventInput, sel selection) wizard {
	all := values(in)
	w := wizard{Knobs: map[string]any{}}
	w.Step, _ = all[StateStep].(string)
	w.Target, _ = all[FieldTarget].(string)
	if t := picked(in, sel, w.Target); t != "" {
		w.Target = t
	}
	if !contains(sel.targets, w.Target) {
		// an engine went away, or the selection changed under the screen
		w.Target = ""
	}
	w.Merge = w.Target != "" && contains(sel.merge, w.Target) && options.Options(all).Bool(options.Merge)
	for _, k := range options.Order {
		if k == options.Merge {
			continue
		}
		if v, ok := all[k]; ok && v != nil && v != "" {
			w.Knobs[k] = v
		}
	}
	if w.Step == "" {
		w.Step = StepFormat
	}
	if sel.readOnly {
		w.Dest, _ = all[FieldDest].(string)
		if w.Dest == "" {
			w.Dest = sel.home
		}
	}
	return w
}

// picked is a NEW press on the format step: a group drawn around the
// current answer (CategoryField(c, current)) holding a reachable format
// other than that answer. The answer itself coming back is not a press —
// it is the chosen button still lit.
//
// ⚠⚠ Exactly ONE new value is a press. Several at once are not an answer
// and change nothing: a browser sends one per press, so several means a
// client that filled every group — measured 2026-09-21, when filex's e2e
// helper `autoFill` pressed the first button of each group on this step and
// the wizard took "the first new one", which was MP4: a 1-pixel PNG went to
// ffmpeg ("exit 3752568763: Conversion failed!") instead of becoming a JPEG.
// Keeping the answer the step already shows is the only choice that does
// not decide something out of sight.
func picked(in *wire.ViewEventInput, sel selection, current string) string {
	vals, _ := in.Data["values"].(map[string]any)
	found := ""
	for _, c := range formats.Categories {
		if s, _ := vals[CategoryField(c, current)].(string); s != "" && s != current && contains(sel.targets, s) {
			if found != "" && found != s {
				return ""
			}
			found = s
		}
	}
	return found
}

// state is the wizard as the surface carries it back.
func (w wizard) state() map[string]any {
	st := map[string]any{StateStep: w.Step, FieldTarget: w.Target, options.Merge: w.Merge}
	for k, v := range w.Knobs {
		st[k] = v
	}
	if w.Dest != "" {
		st[FieldDest] = w.Dest
	}
	return st
}

// steps is the spine: every step THIS SELECTION can need, decided before
// any format is picked and never changed by picking one.
//
//   - Combine, when several files are selected and at least one reachable
//     target can hold them all;
//   - Settings, when at least one reachable route honours a knob.
//
// ⚠ Not "the steps this target needs". That was the spine until v0.43.0,
// and it redrew itself under the pointer: a JPEG opened on Format ·
// Settings · Review, and a press on PDF (no knobs) turned it into Format ·
// Review while the person was reading it. What a target does not need is
// answered by `needed`, and the strip says it in words instead of dropping
// the step.
func (w wizard) steps(sel selection) []string {
	out := []string{StepFormat}
	if len(sel.merge) > 0 {
		out = append(out, StepCombine)
	}
	if anyKnobs(sel) {
		out = append(out, StepSettings)
	}
	// A read-only source: the result has to go somewhere else, and the
	// person says where (Burak, 2026-09-22: "Dönüştür…" on a read-only
	// storage asks for a destination). Decided by the selection, like
	// every other step, so the strip never changes under the pointer.
	if sel.readOnly {
		out = append(out, StepWhere)
	}
	return append(out, StepReview)
}

// anyKnobs: does any target this selection can reach ask anything on the
// settings step — its own route's knobs, or a merge's?
func anyKnobs(sel selection) bool {
	for _, t := range sel.targets {
		if keys, _, _ := routeFor(sel, t); len(keys) > 0 {
			return true
		}
		if contains(sel.merge, t) && len(mergeKnobs(t)) > 0 {
			return true
		}
	}
	return false
}

// needed says whether the answers given so far leave a step anything to
// ask. Format and Review always have something.
func (w wizard) needed(step string, sel selection) bool {
	switch step {
	case StepCombine:
		return w.Target != "" && contains(sel.merge, w.Target)
	case StepSettings:
		keys, _, _ := w.route(sel)
		return w.Target != "" && len(keys) > 0
	}
	return true
}

func (w wizard) at(sel selection) int {
	for i, id := range w.steps(sel) {
		if id == w.Step {
			return i
		}
	}
	return 0
}

// next and prev walk the spine past the steps this conversion does not
// need, so nobody lands on a question that does not apply to them.
func (w wizard) next(sel selection) string {
	ids := w.steps(sel)
	for i := w.at(sel) + 1; i < len(ids); i++ {
		if w.needed(ids[i], sel) {
			return ids[i]
		}
	}
	return StepReview
}

func (w wizard) prev(sel selection) string {
	ids := w.steps(sel)
	for i := w.at(sel) - 1; i > 0; i-- {
		if w.needed(ids[i], sel) {
			return ids[i]
		}
	}
	return StepFormat
}

// route answers the knobs, the preset variant and the route note for the
// answers given so far.
func (w wizard) route(sel selection) (keys []string, variant string, route []wire.Text) {
	if w.Target == "" {
		return nil, "", nil
	}
	if w.Merge {
		// The note carries EVERY language, never the one the call asked for
		// pasted into all slots: a Text whose `en` holds Turkish words is
		// exactly the half-translated screen this plugin's tests look for.
		return mergeKnobs(w.Target), "", []wire.Text{i18n.T("route.merged", "format", names(w.Target))}
	}
	return routeFor(sel, w.Target)
}

// params is every knob of the route, taken from the answers and filled
// with the route's own default wherever the person never touched it.
func (w wizard) params(keys []string, variant string) options.Options {
	p := options.Options{}
	for _, k := range keys {
		if v, ok := w.Knobs[k]; ok && v != nil && v != "" {
			p[k] = v
			continue
		}
		p[k] = options.DefaultFor(k, variant)
	}
	if w.Merge {
		p[options.Merge] = true
	}
	return p
}

// ── the event ──────────────────────────────────────────────────────────

// Handle answers a view event for the Convert wizard.
func Handle(in *wire.ViewEventInput) (*wire.Surface, error) {
	if in.Event == "action" && in.ActionID == ActionCancel {
		return &wire.Surface{Done: true}, nil
	}
	sel := analyse(in)
	if s := nothingToConvert(sel, in); s != nil {
		return s, nil
	}
	w := read(in, sel)

	switch {
	case in.Event == "action" && in.ActionID == ActionBack:
		w.Step = w.prev(sel)

	case in.Event == "submit" && w.Step == StepReview:
		if w.Target == "" {
			w.Step = StepFormat
			return screen(w, sel, in, nil, noTarget()), nil
		}
		keys, variant, _ := w.route(sel)
		params := w.params(keys, variant)
		if errs := params.Validate(keys, variant); len(errs) > 0 {
			w.Step = StepSettings
			return screen(w, sel, in, fieldErrors(errs), nil), nil
		}
		if sel.readOnly && w.Dest == "" {
			w.Step = StepWhere
			return screen(w, sel, in, nil, i18n.T("view.where.choose")), nil
		}
		params[job.ParamTarget] = w.Target
		req := &wire.JobRequest{ActionID: ActionID, Params: map[string]any(params)}
		if sel.readOnly {
			// Into the chosen folder: filex checks it (the person must be
			// able to write there) before the job is queued.
			req.Output = &wire.Output{Mode: "folder", Dir: w.Dest}
		}
		return &wire.Surface{Job: req}, nil

	case in.Event == "submit" && w.Step == StepWhere && w.Dest == "":
		return screen(w, sel, in, nil, i18n.T("view.where.choose")), nil

	case in.Event == "submit" && w.Step == StepFormat && w.Target == "":
		// Next is disabled until something is picked; a submit that arrives
		// anyway is answered with the reason, not with the next step.
		return screen(w, sel, in, nil, noTarget()), nil

	case in.Event == "submit" && w.Step == StepSettings:
		keys, variant, _ := w.route(sel)
		if errs := w.params(keys, variant).Validate(keys, variant); len(errs) > 0 {
			return screen(w, sel, in, fieldErrors(errs), nil), nil
		}
		w.Step = w.next(sel)

	case in.Event == "submit":
		w.Step = w.next(sel)

		// ⚠ No `change` case. A press on a format button arrives as a change,
		// `read` has already taken it as the answer, and the step stays where
		// it is. The package comment says what moving on by itself did.
	}
	return screen(w, sel, in, nil, nil), nil
}

func noTarget() wire.Text {
	return i18n.T("view.choose_target")
}

func fieldErrors(errs map[string]map[string]string) map[string]wire.Text {
	out := map[string]wire.Text{}
	for k, m := range errs {
		out[k] = wire.Text(m)
	}
	return out
}

// nothingToConvert is the screen for a selection with no wizard in it at
// all: nothing selected, a file whose type is unknown, or files with no
// target in common. It answers nil when there is work to do.
func nothingToConvert(sel selection, in *wire.ViewEventInput) *wire.Surface {
	s := &wire.Surface{Title: i18n.T("view.convert"), Size: "md", State: map[string]any{FieldTarget: ""}}
	closeOnly := []wire.SurfaceAction{{ID: ActionCancel, Label: i18n.T("view.close")}}
	switch {
	case len(in.Context.Inputs) == 0:
		s.Nodes = append(s.Nodes, textNode(i18n.T("view.select_file"), "danger"))
		s.Actions = closeOnly
		return s
	case len(sel.unknown) > 0:
		s.Nodes = append(s.Nodes,
			textNode(selectionLine(sel, in.Context.Inputs), ""),
			textNode(i18n.T("view.unknown_type", "names", strings.Join(sel.unknown, ", ")), "danger"))
		s.Actions = closeOnly
		return s
	case len(sel.targets) == 0:
		msg := i18n.T("view.no_common_target")
		if len(sel.blocked) > 0 {
			msg = i18n.T("view.no_engines")
		}
		s.Nodes = append(s.Nodes, textNode(selectionLine(sel, in.Context.Inputs), ""), textNode(msg, "danger"))
		s.Nodes = append(s.Nodes, missingEnginesNote(sel)...)
		s.Actions = closeOnly
		return s
	}
	return nil
}

// ── the screens ────────────────────────────────────────────────────────

// screen draws the step the wizard is on. `errs` marks the fields the
// validator refused; `nudge` is the one sentence a step says when a press
// could not be honoured.
func screen(w wizard, sel selection, in *wire.ViewEventInput, errs map[string]wire.Text, nudge wire.Text) *wire.Surface {
	// Nothing but the first step can be answered without a target, so a
	// wizard whose answer went stale (an engine left, the selection
	// changed) is back where it started rather than on a settings step
	// for a conversion nobody asked for.
	if w.Target == "" || !contains(w.steps(sel), w.Step) || !w.needed(w.Step, sel) {
		w.Step = StepFormat
	}
	s := &wire.Surface{
		Title:  i18n.T("view.convert"),
		Size:   "md",
		State:  w.state(),
		Errors: errs,
		Nodes:  []wire.Node{stepStrip(w, sel)},
	}
	switch w.Step {
	case StepFormat:
		s.Nodes = append(s.Nodes, textNode(selectionLine(sel, in.Context.Inputs), ""))
		if nudge != nil {
			s.Nodes = append(s.Nodes, textNode(nudge, "danger"))
		}
		s.Nodes = append(s.Nodes, headingNode(i18n.T("view.format_heading")))
		chosen := map[string]any{}
		if w.Target != "" {
			s.Nodes = append(s.Nodes, textNode(i18n.T("view.chosen", "format", names(w.Target)), "muted"))
			// The chosen button is lit, under the key its group is drawn
			// with now (CategoryField says why that key moves).
			chosen[CategoryField(formats.CategoryOf(w.Target), w.Target)] = w.Target
		}
		s.Nodes = append(s.Nodes, formNode(targetFields(sel, w.Target), chosen))
		s.Nodes = append(s.Nodes, missingEnginesNote(sel)...)
		s.Nodes = append(s.Nodes, blockedList(sel)...)
		s.Actions = []wire.SurfaceAction{
			{ID: ActionCancel, Label: i18n.T("view.cancel")},
			{ID: ActionSubmit, Label: i18n.T("view.next"), Primary: true, Disabled: w.Target == ""},
		}

	case StepCombine:
		n := len(in.Context.Inputs)
		s.Nodes = append(s.Nodes,
			headingNode(i18n.T("view.combine_heading")),
			textNode(i18n.T("view.combine_body", "n", n, "format", names(w.Target)), ""),
			formNode([]wire.Field{knobField(options.Merge, "")}, map[string]any{options.Merge: w.Merge}),
		)
		s.Actions = backNext()

	case StepSettings:
		keys, variant, route := w.route(sel)
		fields := make([]wire.Field, 0, len(keys))
		vals := map[string]any{}
		for _, k := range keys {
			fields = append(fields, knobField(k, variant))
			if v, ok := w.Knobs[k]; ok && v != nil && v != "" {
				vals[k] = v
			} else {
				vals[k] = options.DefaultFor(k, variant)
			}
		}
		s.Nodes = append(s.Nodes,
			headingNode(i18n.T("view.settings_heading")),
			formNode(fields, vals))
		if len(route) > 0 {
			s.Nodes = append(s.Nodes, textNode(i18n.Join(route, " · "), "muted"))
		}
		s.Actions = backNext()

	case StepWhere:
		s.Nodes = append(s.Nodes, headingNode(i18n.T("view.where.heading")))
		if nudge != nil {
			s.Nodes = append(s.Nodes, textNode(nudge, "danger"))
		}
		s.Nodes = append(s.Nodes,
			textNode(i18n.T("view.where.why"), ""),
			wire.Node{ID: FieldDest, Type: "file-chooser", Props: map[string]any{"kind": "dir", "value": w.Dest}})
		s.Actions = backNext()

	default: // StepReview
		s.Nodes = append(s.Nodes,
			headingNode(i18n.T("view.review_heading")),
			reviewList(w, sel, in))
		s.Actions = []wire.SurfaceAction{
			{ID: ActionBack, Label: i18n.T("view.back")},
			{ID: ActionSubmit, Label: i18n.T("view.convert"), Primary: true, Disabled: w.Target == ""},
		}
	}
	return s
}

// backNext is the footer of a middle step: one primary, plus Back. Nothing
// else belongs there — one step asks one thing.
func backNext() []wire.SurfaceAction {
	return []wire.SurfaceAction{
		{ID: ActionBack, Label: i18n.T("view.back")},
		{ID: ActionSubmit, Label: i18n.T("view.next"), Primary: true},
	}
}

// stepStrip is the spine: where the person is, and what is still coming.
//
// A step this conversion does not need keeps its place and says so in
// words — "Settings (not needed)" — once the person has left the format
// step. On the format step itself every label stays plain: the answer can
// still change there, and the strip must not rewrite itself under the
// pointer as a format is pressed.
func stepStrip(w wizard, sel selection) wire.Node {
	ids := w.steps(sel)
	at := w.at(sel)
	items := make([]map[string]any, 0, len(ids))
	for i, id := range ids {
		state := "todo"
		switch {
		case i < at:
			state = "done"
		case i == at:
			state = "active"
		}
		label := stepLabel(id)
		if w.Step != StepFormat && !w.needed(id, sel) {
			label = i18n.T("view.step.not_needed", "step", label)
		}
		items = append(items, map[string]any{"id": id, "label": label, "state": state})
	}
	return wire.Node{Type: "steps", Props: map[string]any{"items": items}}
}

func stepLabel(id string) wire.Text {
	switch id {
	case StepFormat:
		return i18n.T("view.step.format")
	case StepCombine:
		return i18n.T("view.step.combine")
	case StepSettings:
		return i18n.T("view.step.settings")
	case StepWhere:
		return i18n.T("view.step.where")
	}
	return i18n.T("view.step.review")
}

// reviewList is the last step's whole point: the job in a table, so the
// person reads what is about to happen before it happens.
func reviewList(w wizard, sel selection, in *wire.ViewEventInput) wire.Node {
	keys, variant, route := w.route(sel)
	params := w.params(keys, variant)
	rows := []map[string]any{
		{"id": "files", "cells": map[string]any{
			"what": i18n.T("view.review.files"), "value": selectionLine(sel, in.Context.Inputs)}},
		{"id": FieldTarget, "cells": map[string]any{
			"what": i18n.T("view.review.target"), "value": names(w.Target)}},
	}
	if contains(sel.merge, w.Target) {
		answer := i18n.T("option.merge.no")
		if w.Merge {
			answer = i18n.T("option.merge.yes")
		}
		rows = append(rows, map[string]any{"id": options.Merge, "cells": map[string]any{
			"what": i18n.T("view.step.combine"), "value": answer}})
	}
	for _, k := range keys {
		rows = append(rows, map[string]any{"id": k, "cells": map[string]any{
			"what": wire.Text(options.Defs[k].Label), "value": knobWords(k, variant, params[k])}})
	}
	if len(route) > 0 {
		rows = append(rows, map[string]any{"id": "route", "cells": map[string]any{
			"what": i18n.T("view.review.route"), "value": i18n.Join(route, " · ")}})
	}
	if sel.readOnly {
		rows = append(rows, map[string]any{"id": FieldDest, "cells": map[string]any{
			"what": i18n.T("view.review.where"), "value": i18n.Same(w.Dest)}})
	}
	return wire.Node{Type: "list", Props: map[string]any{
		"columns": []map[string]any{
			{"key": "what", "label": i18n.T("view.review.what")},
			{"key": "value", "label": i18n.T("view.review.value")},
		},
		"rows": rows,
	}}
}

// knobWords is a knob's answer in words — the button the person pressed,
// not the value behind it ("Medium (default)", not "medium").
func knobWords(key, variant string, v any) wire.Text {
	d := options.Defs[key]
	o := options.Options{key: v}
	switch d.Type {
	case "bool":
		want := strconv.FormatBool(o.Bool(key))
		for _, c := range options.Choices(key, variant) {
			if c.Value == want {
				return wire.Text(c.Label)
			}
		}
		if o.Bool(key) {
			return i18n.T("view.yes")
		}
		return i18n.T("view.no")
	case "select":
		for _, c := range options.Choices(key, variant) {
			if c.Value == o.String(key) {
				return wire.Text(c.Label)
			}
		}
		return i18n.Same(o.String(key))
	case "int":
		return i18n.Same(strconv.Itoa(o.Int(key)))
	}
	if s := o.String(key); s != "" {
		return i18n.Same(s)
	}
	return i18n.T("view.all")
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

func textNode(t wire.Text, tone string) wire.Node {
	props := map[string]any{"text": t}
	if tone != "" {
		props["tone"] = tone
	}
	return wire.Node{Type: "text", Props: props}
}

func headingNode(t wire.Text) wire.Node {
	return wire.Node{Type: "text", Props: map[string]any{"text": t, "heading": true}}
}

func formNode(fields []wire.Field, vals map[string]any) wire.Node {
	return wire.Node{Type: "form", Props: map[string]any{"fields": fields, "values": vals}}
}

// selectionLine renders "3 files: 2 × PNG, 1 × JPEG" / "1 file: report.docx (Word)".
func selectionLine(sel selection, inputs []wire.FileRef) wire.Text {
	if len(inputs) == 1 {
		f, _, ok := formats.Detect(inputs[0].Name)
		if !ok {
			return i18n.Same(inputs[0].Name + " (?)")
		}
		return i18n.Each(func(lang string) string { return inputs[0].Name + " (" + f.Name(lang) + ")" })
	}
	list := func(lang string) string {
		var parts []string
		for _, s := range sel.sources {
			parts = append(parts, strconv.Itoa(sel.counts[s])+" × "+nameOf(s, lang))
		}
		if len(sel.unknown) > 0 {
			parts = append(parts, strconv.Itoa(len(sel.unknown))+" × ?")
		}
		return strings.Join(parts, ", ")
	}
	return i18n.T("view.selection_many", "n", len(inputs), "list", i18n.Each(list))
}

// targetFields is the format step: ONE button group per category, with the
// category's own name as its heading.
//
// ⚠ One select carrying all thirty targets is the wall this replaces, and
// the only way it could say which category a format was in was to glue the
// category into every label ("Görsel · PNG"). A group per category says it
// once, at the top, and every option stays readable without a click.
//
// Every group is keyed around `chosen`, the answer the step is drawn with
// (CategoryField says why); the caller lights that one button.
func targetFields(sel selection, chosen string) []wire.Field {
	var out []wire.Field
	for _, c := range formats.Categories {
		var opts []wire.FieldOption
		for _, f := range formats.InCategory(c) {
			if !contains(sel.targets, f.ID) {
				continue
			}
			label := wire.Text(f.Names())
			if len(sel.sources) == 1 && sel.sources[0] == f.ID {
				label = i18n.T("view.reencode", "format", label)
			}
			opts = append(opts, option(f.ID, label))
		}
		if len(opts) == 0 {
			continue
		}
		out = append(out, field(wire.Field{
			Key:     CategoryField(c, chosen),
			Type:    "select",
			Options: opts,
		}, wire.Text(formats.CategoryLabel(c)), nil, nil))
	}
	return out
}

func knobField(key, variant string) wire.Field {
	d := options.Defs[key]
	f := wire.Field{Key: key}
	var placeholder wire.Text
	switch d.Type {
	case "int":
		f.Type = "int"
		mn, mx := d.Min, d.Max
		f.Min, f.Max = &mn, &mx
		f.Default = d.Default
	case "select":
		f.Type = "select"
		for _, c := range options.Choices(key, variant) {
			f.Options = append(f.Options, option(c.Value, wire.Text(c.Label)))
		}
		f.Default = options.DefaultFor(key, variant)
	case "bool":
		f.Type = "bool"
		f.Default = d.Default
		// ⚠ Without Style the client GUESSES how to draw a bool, and its
		// guess for a plain one is a tickbox — "Combine into one file" with
		// an empty box beside it, which is not a question anybody reads.
		// `choice` draws both answers as buttons, in the plugin's own words.
		f.Style = "choice"
		for _, c := range options.Choices(key, variant) {
			f.Options = append(f.Options, option(c.Value, wire.Text(c.Label)))
		}
	default:
		f.Type = "string"
		placeholder = i18n.T("view.all")
	}
	return field(f, wire.Text(d.Label), wire.Text(d.Help), placeholder)
}

// canInstallEngines says whether the person on the screen is the one who
// could put a missing engine on this server: filex's administrator.
//
// ⚠⚠ The owner's rule for anything that depends on what the server has
// installed or configured: greyed WITH THE REASON for an administrator,
// NOT SHOWN AT ALL to everybody else (filex lesson #292, `gateOnService`).
// "Not installed on this server: ImageMagick, LibreOffice, librsvg" and a
// table of formats each needing one were drawn for every account (v0.43.0
// wave 2, 2026-09-22): a person who can do nothing about it read a list of
// server programs they had never heard of, under the formats they CAN have.
// No actor at all (a host older than the actor field) keeps the old,
// complete screen.
func canInstallEngines(a *wire.Actor) bool {
	return a == nil || a.Role == "admin"
}

// blockedList renders the targets the catalogue knows for this selection
// but this host cannot reach, each with the engine it needs, as a greyed
// list under the button groups (a button group cannot grey an option out,
// so the list is how a missing format stays visible BY NAME).
func blockedList(sel selection) []wire.Node {
	if len(sel.blocked) == 0 || !sel.admin {
		return nil
	}
	var rows []map[string]any
	for _, c := range formats.Categories {
		for _, f := range formats.InCategory(c) {
			need, ok := sel.blocked[f.ID]
			if !ok {
				continue
			}
			category, name := formats.CategoryLabel(c), f
			rows = append(rows, map[string]any{
				"id": f.ID,
				"cells": map[string]any{
					"format": i18n.Each(func(lang string) string { return i18n.Pick(lang, category) + " · " + name.Name(lang) }),
					"needs":  i18n.T("view.blocked.needs", "engine", need),
				},
			})
		}
	}
	return []wire.Node{
		textNode(i18n.T("view.blocked.intro"), "muted"),
		{Type: "list", Props: map[string]any{
			"columns": []map[string]any{
				{"key": "format", "label": i18n.T("view.blocked.format")},
				{"key": "needs", "label": i18n.T("view.blocked.needs_col")},
			},
			"rows": rows,
		}},
	}
}

// missingEnginesNote says which engines are absent and what each would
// unlock, so the person knows why a format is not in the list.
func missingEnginesNote(sel selection) []wire.Node {
	var missing []string
	for _, e := range graph.Engines {
		if !sel.engines[e] {
			missing = append(missing, e)
		}
	}
	if len(missing) == 0 || len(sel.blocked) == 0 || !sel.admin {
		return nil
	}
	engines := strings.Join(graph.EngineNames(missing), ", ")
	return []wire.Node{textNode(i18n.T("view.missing_engines", "engines", engines), "muted")}
}
