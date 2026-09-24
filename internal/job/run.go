// Package job runs the `convert` action: for every input it plans a route
// to the chosen target, walks it step by step (pure-Go steps read the
// previous ref and write a new output; engine steps hand the previous ref
// to the host's engine and take the artefact it produced), names the
// results, reports progress per file and collects per-file failures so one
// bad file never sinks the others.
package job

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/brf-tech/filex/backend/pkg/pluginkit"
	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"

	"github.com/brf-tech/filex-convert/internal/engines"
	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/i18n"
	"github.com/brf-tech/filex-convert/internal/options"
	"github.com/brf-tech/filex-convert/internal/purego"
)

// ParamTarget is the params key that names the target format id.
const ParamTarget = "target"

// Result is what one action run produced.
type Result struct {
	Outputs []wire.OutputRef
	Done    int
	Failed  int
	Errors  []*Error
	Routes  []string // one line per input, for the log
}

// Run executes the action. It never returns an error for a file's failure;
// those are reported in the message so the person sees which file and why.
// It returns an error only when the request itself is unusable.
func Run(h Host, in *wire.ActionRunInput) (*wire.ActionRunOutput, error) {
	params := options.Options(in.Params)
	target := params.String(ParamTarget)
	if _, ok := formats.ByID(target); !ok {
		e := &Error{Code: CodeNoTarget, Detail: target}
		return &wire.ActionRunOutput{OK: false, Message: e.Text()}, nil
	}
	if len(in.Inputs) == 0 {
		return &wire.ActionRunOutput{OK: false, Message: i18n.T("job.no_input")}, nil
	}
	if params.Bool(options.Merge) && len(in.Inputs) > 1 {
		return runMerged(h, in, target, params), nil
	}
	res := &Result{}
	total := int64(len(in.Inputs))
	for i, f := range in.Inputs {
		h.Progress(int64(i), total, progressLine(f.Name, target, in.Locale))
		fileParams := options.Options{}
		for k, v := range params {
			fileParams[k] = v
		}
		fileParams[purego.SourceName] = f.Name
		fileParams[purego.JobLocale] = in.Locale
		outs, err := convertOne(h, f, target, fileParams, in.Engines, res)
		if err != nil {
			err.File = f.Name
			res.Failed++
			res.Errors = append(res.Errors, err)
			h.Log("warn", f.Name+": "+err.Error())
			continue
		}
		res.Done++
		res.Outputs = append(res.Outputs, outs...)
	}
	h.Progress(total, total, "")
	return &wire.ActionRunOutput{
		OK:      res.Done > 0,
		Outputs: res.Outputs,
		Message: summary(res, target, len(in.Inputs)),
	}, nil
}

// progressLine is the tray message while a file converts, in the job's
// language: the arrow reads the same everywhere, a format's label does not
// ("Plain text (.txt)" is "Düz metin (.txt)" on a Turkish tray).
func progressLine(name, target, locale string) string {
	return name + " → " + labelIn(target, locale)
}

// labelIn is a format id's label in one language (the id when unknown).
func labelIn(id, locale string) string {
	if f, ok := formats.ByID(id); ok {
		return f.Name(locale)
	}
	return id
}

// convertOne plans and runs one input, retrying once on a different route
// when an engine step fails.
func convertOne(h Host, f wire.FileRef, target string, params options.Options, engines map[string]bool, res *Result) ([]wire.OutputRef, *Error) {
	src, stem, ok := formats.Detect(f.Name)
	if !ok {
		return nil, &Error{Code: CodeUnknownFormat}
	}
	route, err := graph.Route(src.ID, target, engines)
	if err != nil {
		return nil, routeError(err)
	}
	if errs := params.Validate(route.Options(), route.Variant()); len(errs) > 0 {
		keys := make([]string, 0, len(errs))
		for k := range errs {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		return nil, &Error{Code: CodeBadOptions, Detail: strings.Join(keys, ", ")}
	}
	res.Routes = append(res.Routes, f.Name+": "+route.String())
	outs, failed, cerr := execute(h, f, stem, route, params)
	if cerr == nil {
		return outs, nil
	}
	if failed == nil || cerr.Code != CodeEngineFailed {
		return nil, cerr
	}
	// One retry over a route that avoids the edge that just failed.
	alt, err := graph.RouteExcluding(src.ID, target, engines, []graph.Edge{*failed})
	if err != nil {
		return nil, cerr
	}
	h.Log("info", f.Name+": retrying via "+alt.String())
	res.Routes = append(res.Routes, f.Name+": retry "+alt.String())
	outs, _, cerr2 := execute(h, f, stem, alt, params)
	if cerr2 != nil {
		return nil, cerr
	}
	return outs, nil
}

func routeError(err error) *Error {
	var nre *graph.NoRouteError
	if errors.As(err, &nre) && len(nre.MissingEngines) > 0 {
		return &Error{Code: CodeEngineMissing, Detail: strings.Join(nre.MissingEngines, ", ")}
	}
	if errors.Is(err, graph.ErrUnknownFormat) {
		return &Error{Code: CodeUnknownFormat, Detail: err.Error()}
	}
	return &Error{Code: CodeNoRoute, Detail: err.Error()}
}

// item is a file travelling through a route: its ref and a name hint.
type item struct {
	ref  string
	name string
	page int // 1-based page number for multi-output steps, 0 otherwise
}

// execute walks the route. On failure it answers the failed edge (when an
// engine step failed) so the caller can plan around it.
func execute(h Host, f wire.FileRef, stem string, route graph.Plan, params options.Options) ([]wire.OutputRef, *graph.Edge, *Error) {
	items := []item{{ref: f.Ref, name: f.Name}}
	for i, e := range route {
		last := i == len(route)-1
		var next []item
		for _, it := range items {
			produced, err := step(h, it, e, i, params)
			if err != nil {
				edge := e
				if e.Engine != graph.PureGo {
					return nil, &edge, err
				}
				return nil, nil, err
			}
			next = append(next, produced...)
		}
		items = next
		if last {
			break
		}
	}
	return finalNames(items, stem, route, params), nil, nil
}

// step runs one edge on one item and answers what came out.
func step(h Host, it item, e graph.Edge, idx int, params options.Options) ([]item, *Error) {
	if e.Engine == graph.PureGo {
		if size := h.InputSize(it.ref); size > purego.MaxInput {
			return nil, &Error{Code: CodeTooLarge, Detail: fmt.Sprintf("%d bytes > %d", size, purego.MaxInput)}
		}
		data, err := h.ReadInput(it.ref)
		if err != nil {
			return nil, &Error{Code: CodeReadFailed, Detail: err.Error()}
		}
		out, err := purego.Convert(e.From, e.To, data, params)
		if err != nil {
			if errors.Is(err, purego.ErrTooLarge) {
				return nil, &Error{Code: CodeTooLarge, Detail: err.Error()}
			}
			return nil, &Error{Code: CodeConvertFailed, Detail: err.Error()}
		}
		name := "step" + strconv.Itoa(idx) + "." + primaryExt(e.To)
		ref, err := h.WriteOutput(name, out)
		if err != nil {
			return nil, &Error{Code: CodeWriteFailed, Detail: err.Error()}
		}
		return []item{{ref: ref.Ref, name: ref.Name, page: it.page}}, nil
	}

	inv, err := engines.Build(e, engines.InputName(e.From), "out", params)
	if err != nil {
		return nil, &Error{Code: CodeConvertFailed, Detail: err.Error()}
	}
	res, err := h.EngineRun(pluginkit.EngineRequest{
		Engine:   inv.Engine,
		Args:     inv.Args,
		Inputs:   map[string]string{inv.Input: it.ref},
		Outputs:  inv.Outputs,
		TimeoutS: inv.TimeoutS,
	})
	if err != nil {
		var he *pluginkit.HostError
		if errors.As(err, &he) {
			switch he.Code {
			case wire.ErrUnavailable, wire.ErrPermissionDenied:
				return nil, &Error{Code: CodeEngineMissing, Detail: e.Engine + ": " + he.Message}
			case wire.ErrTimeout:
				return nil, &Error{Code: CodeCancelled, Detail: he.Message}
			case wire.ErrTooLarge:
				return nil, &Error{Code: CodeTooLarge, Detail: he.Message}
			}
		}
		return nil, &Error{Code: CodeEngineFailed, Detail: e.Engine + ": " + err.Error()}
	}
	if res.Exit != 0 {
		return nil, &Error{Code: CodeEngineFailed, Detail: fmt.Sprintf("%s exit %d: %s", e.Engine, res.Exit, lastLine(res.StderrTail))}
	}
	if len(res.Outputs) == 0 {
		return nil, &Error{Code: CodeEngineFailed, Detail: e.Engine + " produced no file: " + lastLine(res.StderrTail)}
	}
	if !inv.Multi {
		pick := res.Outputs[0]
		if len(inv.Outputs) > 0 {
			for _, o := range res.Outputs {
				if o.Name == inv.Outputs[0] {
					pick = o
					break
				}
			}
		}
		return []item{{ref: pick.Ref, name: pick.Name, page: it.page}}, nil
	}
	// Several files (pages): keep them all in page order.
	outs := append([]wire.OutputRef(nil), res.Outputs...)
	sort.SliceStable(outs, func(a, b int) bool { return pageNo(outs[a].Name) < pageNo(outs[b].Name) })
	var items []item
	for _, o := range outs {
		items = append(items, item{ref: o.Ref, name: o.Name, page: pageNo(o.Name)})
	}
	return items, nil
}

var pageRe = regexp.MustCompile(`-(\d+)\.[A-Za-z0-9]+$`)

// pageNo pulls the page number out of "out-03.png" style names.
func pageNo(name string) int {
	m := pageRe.FindStringSubmatch(name)
	if m == nil {
		return 0
	}
	n, _ := strconv.Atoi(m[1])
	return n
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndexAny(s, "\r\n"); i >= 0 {
		s = strings.TrimSpace(s[i+1:])
	}
	return clip(s, 200)
}

func primaryExt(format string) string {
	if f, ok := formats.ByID(format); ok {
		return f.Primary()
	}
	return "bin"
}

// finalNames gives the produced refs the names the person expects:
// <stem>.<ext>, <stem>-<page>.<ext> for page sets, and <stem>.compressed /
// <stem>.pdfa when the format did not change.
func finalNames(items []item, stem string, route graph.Plan, params options.Options) []wire.OutputRef {
	target := route[len(route)-1].To
	ext := primaryExt(target)
	base := stem
	if route[0].From == target {
		if params.Bool(options.PDFA) {
			base += ".pdfa"
		} else {
			base += ".compressed"
		}
	}
	multi := len(items) > 1 || (len(items) == 1 && items[0].page > 0)
	out := make([]wire.OutputRef, 0, len(items))
	for i, it := range items {
		name := base + "." + ext
		if multi {
			n := it.page
			if n == 0 {
				n = i + 1
			}
			name = base + "-" + strconv.Itoa(n) + "." + ext
		}
		out = append(out, wire.OutputRef{Ref: it.ref, Name: name})
	}
	return out
}

// summary is the job's final message in every language.
func summary(res *Result, target string, total int) wire.Text {
	format := labels(target)
	var msg wire.Text
	switch {
	case res.Failed > 0:
		msg = i18n.T("job.converted_some", "done", res.Done, "total", total, "format", format)
	case total == 1:
		msg = i18n.T("job.converted_one", "format", format)
	default:
		msg = i18n.T("job.converted_all", "n", total, "format", format)
	}
	if res.Failed > 0 {
		var list []wire.Text
		for i, e := range res.Errors {
			if i == 3 {
				list = append(list, i18n.T("job.more", "n", len(res.Errors)-3))
				break
			}
			list = append(list, e.Text())
		}
		errs := i18n.Join(list, "; ")
		if res.Done == 0 {
			msg = i18n.T("job.nothing_converted", "errors", errs)
		} else {
			msg = i18n.T("job.some_failed", "summary", msg, "errors", errs)
		}
	}
	return msg
}

// labels is a format id's label in every language.
func labels(id string) wire.Text {
	return i18n.Each(func(lang string) string { return labelIn(id, lang) })
}
