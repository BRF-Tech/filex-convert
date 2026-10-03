// Package graph is the conversion graph: formats are nodes, a way to turn
// one into another is an edge that names the engine that does it (or "" for
// pure Go inside the sandbox), a cost and whether it loses information.
// Route finds the cheapest path with a weighted Dijkstra search bounded to
// MaxSteps hops; RouteExcluding retries without an edge that failed.
//
// The graph is written from the plugin's own specification: base hop cost
// 1, lossy edges weighted ×LossyFactor, at most 3 steps. It shares no code
// with any other converter.
package graph

import (
	"container/heap"
	"errors"
	"fmt"
	"sort"

	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/i18n"
	"github.com/brf-tech/filex-convert/internal/options"
)

// MaxSteps is the longest route the planner accepts.
const MaxSteps = 3

// LossyFactor multiplies the cost of an edge that loses information.
const LossyFactor = 1.4

// Engine names, matching the host's `engines:<name>` permissions. PureGo is
// the sandbox itself.
const (
	PureGo      = ""
	FFmpeg      = "ffmpeg"
	ImageMagick = "imagemagick"
	// Office is the office engine: since filex 0.50 the ONLYOFFICE Document
	// Server filex is connected to. Until 0.50 it was LibreOffice, asked for
	// as `libreoffice`; to the host the two names are one engine and one
	// grant, and both read the soffice command line engines.soffice writes.
	Office      = "office"
	Ghostscript = "ghostscript"
	Poppler     = "poppler"
	RSVG        = "rsvg"
)

// Engines lists every engine the graph may name, in permission order.
var Engines = []string{FFmpeg, ImageMagick, Office, Ghostscript, Poppler, RSVG}

// Edge is one conversion step.
type Edge struct {
	From, To string   // format ids
	Engine   string   // PureGo or an engine name
	Cost     float64  // base cost, 1 = an ordinary hop
	Lossy    bool     // information is lost (re-encoding, rasterising)
	Options  []string // option keys this step honours
	// Variant selects a choice list for select knobs ("video"/"pdf" preset).
	Variant string
	// Multi says the step may produce several files (pdf pages → images).
	Multi bool
	// Terminal says a route must end with this edge: what it produces is not
	// a faithful stand-in for the source (an animated GIF of a video, the
	// text of a PDF), so chaining further conversions after it would be
	// nonsense the planner must not offer.
	Terminal bool
	// Initial says a route must start with this edge: the step only makes
	// sense on an original of that kind (an animated GIF to video, the text
	// of a real PDF), not on something another step just produced.
	Initial bool
}

// Key identifies an edge for RouteExcluding: from→to via engine.
func (e Edge) Key() string { return e.From + ">" + e.To + "@" + e.Engine }

// Weight is the cost the planner uses.
func (e Edge) Weight() float64 {
	if e.Lossy {
		return e.Cost * LossyFactor
	}
	return e.Cost
}

// Plan is an ordered list of edges from a source to a target.
type Plan []Edge

// Cost is the summed weight.
func (r Plan) Cost() float64 {
	c := 0.0
	for _, e := range r {
		c += e.Weight()
	}
	return c
}

// Engines lists the distinct engines a route needs (PureGo excluded).
func (r Plan) Engines() []string {
	seen := map[string]bool{}
	var out []string
	for _, e := range r {
		if e.Engine != PureGo && !seen[e.Engine] {
			seen[e.Engine] = true
			out = append(out, e.Engine)
		}
	}
	return out
}

// Options is the union of option keys along the route, in options.Order.
func (r Plan) Options() []string {
	seen := map[string]bool{}
	for _, e := range r {
		for _, k := range e.Options {
			seen[k] = true
		}
	}
	var out []string
	for _, k := range options.Order {
		if seen[k] {
			out = append(out, k)
		}
	}
	return out
}

// Variant answers the first non-empty edge variant (for the preset knob).
func (r Plan) Variant() string {
	for _, e := range r {
		if e.Variant != "" {
			return e.Variant
		}
	}
	return ""
}

// EngineName is an engine's name as a person reads it: the product's own
// spelling ("ImageMagick", not "imagemagick"). The ids stay what they are —
// they are permission names (`engines:<id>`) and the host's vocabulary.
func EngineName(id string) string {
	switch id {
	case FFmpeg:
		return "FFmpeg"
	case ImageMagick:
		return "ImageMagick"
	case Office:
		// The project's own spelling, the one filex uses for it.
		return "ONLYOFFICE"
	case Ghostscript:
		return "Ghostscript"
	case Poppler:
		return "Poppler"
	case RSVG:
		return "librsvg"
	}
	return id
}

// EngineNames spells a list of engine ids for a person, in the given order.
func EngineNames(ids []string) []string {
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		out = append(out, EngineName(id))
	}
	return out
}

// Words renders the route for a person: format names, and for each step
// who does it — the engine's product name, or "built in" for a step the
// plugin does itself.
//
// ⚠ Not String(). String is the log line ("jpg → pdf (go)"): ids and the
// word "go", which is this plugin's implementation language, not something
// the person asked about. The v0.43.0 sweep found exactly that line on the
// Turkish review screen ("İzlenecek yol jpg → pdf (go)").
func (r Plan) Words(name func(id string) string, locale string) string {
	if len(r) == 0 {
		return ""
	}
	s := name(r[0].From)
	for _, e := range r {
		by := EngineName(e.Engine)
		if e.Engine == PureGo {
			by = i18n.S(locale, "route.built_in")
		}
		s += " → " + name(e.To) + " (" + by + ")"
	}
	return s
}

// String renders "docx → pdf (office)" — the log line; see Words for
// the one a person reads.
func (r Plan) String() string {
	if len(r) == 0 {
		return ""
	}
	s := r[0].From
	for _, e := range r {
		eng := e.Engine
		if eng == PureGo {
			eng = "go"
		}
		s += fmt.Sprintf(" → %s (%s)", e.To, eng)
	}
	return s
}

// Errors the planner answers.
var (
	ErrUnknownFormat = errors.New("unknown format")
	ErrNoRoute       = errors.New("no route")
	ErrSameFormat    = errors.New("source and target are the same format and no re-encode edge exists")
)

// NoRouteError says why a route was refused.
type NoRouteError struct {
	From, To string
	// MissingEngines is non-empty when a route would exist with these engines.
	MissingEngines []string
}

func (e *NoRouteError) Error() string {
	if len(e.MissingEngines) > 0 {
		return fmt.Sprintf("no route from %s to %s without engine(s) %v", e.From, e.To, e.MissingEngines)
	}
	return fmt.Sprintf("no route from %s to %s", e.From, e.To)
}

// Unwrap lets errors.Is(err, ErrNoRoute) hold.
func (e *NoRouteError) Unwrap() error { return ErrNoRoute }

// AllEngines is the availability map with every engine present.
func AllEngines() map[string]bool {
	m := map[string]bool{}
	for _, e := range Engines {
		m[e] = true
	}
	return m
}

// usable reports whether an edge can run with the given engines.
func (e Edge) usable(engines map[string]bool) bool {
	return e.Engine == PureGo || engines[e.Engine]
}

// Route plans the cheapest path from one format id to another with the
// engines that are present. from == to only succeeds through a direct
// re-encode edge (pdf → pdf compression).
func Route(from, to string, engines map[string]bool) (Plan, error) {
	return RouteExcluding(from, to, engines, nil)
}

// RouteExcluding plans like Route but never uses the excluded edges (by
// Key). It is the retry after an edge failed at run time.
func RouteExcluding(from, to string, engines map[string]bool, excluded []Edge) (Plan, error) {
	if _, ok := formats.ByID(from); !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownFormat, from)
	}
	if _, ok := formats.ByID(to); !ok {
		return nil, fmt.Errorf("%w: %s", ErrUnknownFormat, to)
	}
	skip := map[string]bool{}
	for _, e := range excluded {
		skip[e.Key()] = true
	}
	if r := dijkstra(from, to, engines, skip); r != nil {
		return r, nil
	}
	nre := &NoRouteError{From: from, To: to}
	if r := dijkstra(from, to, AllEngines(), skip); r != nil {
		for _, eng := range r.Engines() {
			if !engines[eng] {
				nre.MissingEngines = append(nre.MissingEngines, eng)
			}
		}
	}
	return nil, nre
}

// state is a search node: the format reached and how many steps it took.
// Steps are part of the state so a cheaper-but-longer prefix cannot hide a
// route that fits within MaxSteps.
type state struct {
	format string
	steps  int
}

type item struct {
	st   state
	cost float64
	prev *item
	via  *Edge
}

type pq []*item

func (p pq) Len() int           { return len(p) }
func (p pq) Less(i, j int) bool { return p[i].cost < p[j].cost }
func (p pq) Swap(i, j int)      { p[i], p[j] = p[j], p[i] }
func (p *pq) Push(x any)        { *p = append(*p, x.(*item)) }
func (p *pq) Pop() any {
	old := *p
	n := len(old)
	it := old[n-1]
	*p = old[:n-1]
	return it
}

func dijkstra(from, to string, engines map[string]bool, skip map[string]bool) Plan {
	if from == to {
		// Only a direct self edge counts; the cheapest usable one wins.
		var best *Edge
		for i := range edges {
			e := &edges[i]
			if e.From == from && e.To == to && e.usable(engines) && !skip[e.Key()] {
				if best == nil || e.Weight() < best.Weight() {
					best = e
				}
			}
		}
		if best == nil {
			return nil
		}
		return Plan{*best}
	}
	best := map[state]float64{}
	q := &pq{}
	heap.Init(q)
	start := &item{st: state{from, 0}}
	heap.Push(q, start)
	best[start.st] = 0
	for q.Len() > 0 {
		cur := heap.Pop(q).(*item)
		if cur.cost > best[cur.st] {
			continue
		}
		if cur.st.format == to {
			return unwind(cur)
		}
		if cur.st.steps >= MaxSteps {
			continue
		}
		for _, e := range outgoing(cur.st.format) {
			if e.From == e.To || !e.usable(engines) || skip[e.Key()] {
				continue
			}
			if e.Terminal && e.To != to {
				continue
			}
			if e.Initial && cur.st.steps > 0 {
				continue
			}
			// Never pass through the source again; cycles waste steps.
			if e.To == from {
				continue
			}
			next := state{e.To, cur.st.steps + 1}
			c := cur.cost + e.Weight()
			if old, ok := best[next]; ok && old <= c {
				continue
			}
			best[next] = c
			heap.Push(q, &item{st: next, cost: c, prev: cur, via: e})
		}
	}
	return nil
}

func unwind(it *item) Plan {
	var r Plan
	for it != nil && it.via != nil {
		r = append(Plan{*it.via}, r...)
		it = it.prev
	}
	return r
}

// Reachable answers every target reachable from a source with the given
// engines, with the cheapest route to each. The source itself is included
// only when a re-encode edge exists.
func Reachable(from string, engines map[string]bool) map[string]Plan {
	out := map[string]Plan{}
	for _, id := range formats.IDs() {
		if r, err := Route(from, id, engines); err == nil {
			out[id] = r
		}
	}
	return out
}

// Targets lists the ids reachable from every one of the sources (the
// intersection), sorted for stable output.
func Targets(sources []string, engines map[string]bool) []string {
	if len(sources) == 0 {
		return nil
	}
	count := map[string]int{}
	seen := map[string]bool{}
	for _, s := range sources {
		if seen[s] {
			continue
		}
		seen[s] = true
		for id := range Reachable(s, engines) {
			count[id]++
		}
	}
	var out []string
	for id, n := range count {
		if n == len(seen) {
			out = append(out, id)
		}
	}
	sort.Strings(out)
	return out
}

// ── adjacency ──

var outgoingIdx map[string][]*Edge

func init() {
	outgoingIdx = map[string][]*Edge{}
	for i := range edges {
		e := &edges[i]
		outgoingIdx[e.From] = append(outgoingIdx[e.From], e)
	}
}

func outgoing(from string) []*Edge { return outgoingIdx[from] }

// Edges exposes the table for tests and documentation tools.
func Edges() []Edge { return append([]Edge(nil), edges...) }

// Direct answers the direct edges between two formats (any engine).
func Direct(from, to string) []Edge {
	var out []Edge
	for _, e := range outgoing(from) {
		if e.To == to {
			out = append(out, *e)
		}
	}
	return out
}
