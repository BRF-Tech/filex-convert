package job

import (
	"strconv"

	"github.com/brf-tech/filex/backend/pkg/pluginkit"
	"github.com/brf-tech/filex/backend/pkg/pluginkit/wire"

	"github.com/brf-tech/filex-convert/internal/engines"
	"github.com/brf-tech/filex-convert/internal/formats"
	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/i18n"
	"github.com/brf-tech/filex-convert/internal/options"
	"github.com/brf-tech/filex-convert/internal/pdfw"
	"github.com/brf-tech/filex-convert/internal/purego"
)

// Merge mode: the whole selection becomes one output. Three shapes exist:
// images → one PDF (a page each, pure Go), any files → one archive (pure
// Go), images → one video or animated GIF (a frame each, ffmpeg). The
// output is named after the first input's stem.

// MergeTargets answers the targets that can take a merged selection of
// these source ids, or nil when merging makes no sense for them.
func MergeTargets(sources []string) []string {
	if len(sources) == 0 {
		return nil
	}
	allImages := true
	for _, s := range sources {
		if !contains(graph.GoImages, s) {
			allImages = false
		}
	}
	out := append([]string(nil), graph.ArchiveWritable...)
	if allImages {
		out = append([]string{"pdf", "mp4", "webm", "mkv", "gif"}, out...)
	}
	return out
}

// CanMerge says whether a target accepts the merged selection.
func CanMerge(sources []string, target string) bool {
	return contains(MergeTargets(sources), target)
}

// MergeEngines names the engine a merged conversion into target needs.
func MergeEngines(target string) []string {
	switch target {
	case "mp4", "webm", "mkv", "gif":
		return []string{graph.FFmpeg}
	}
	return nil
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// runMerged converts the whole selection into one file.
func runMerged(h Host, in *wire.ActionRunInput, target string, params options.Options) *wire.ActionRunOutput {
	var sources []string
	var stems []string
	for _, f := range in.Inputs {
		src, stem, ok := formats.Detect(f.Name)
		if !ok {
			e := &Error{Code: CodeUnknownFormat, File: f.Name}
			return failed(h, e)
		}
		sources = append(sources, src.ID)
		stems = append(stems, stem)
	}
	if !CanMerge(sources, target) {
		e := &Error{Code: CodeNoRoute, Detail: "merge into " + target}
		return failed(h, e)
	}
	for _, eng := range MergeEngines(target) {
		if !in.Engines[eng] {
			e := &Error{Code: CodeEngineMissing, Detail: eng, Engines: []string{eng}}
			return failed(h, e)
		}
	}
	name := stems[0] + "." + primaryExt(target)
	total := int64(len(in.Inputs))
	var datas [][]byte
	var names []string
	for i, f := range in.Inputs {
		h.Progress(int64(i), total, progressLine(f.Name, target))
		if size := h.InputSize(f.Ref); size > purego.MaxInput {
			e := &Error{Code: CodeTooLarge, File: f.Name}
			return failed(h, e)
		}
		data, err := h.ReadInput(f.Ref)
		if err != nil {
			e := &Error{Code: CodeReadFailed, File: f.Name, Detail: err.Error()}
			return failed(h, e)
		}
		datas = append(datas, data)
		names = append(names, f.Name)
	}
	var out []byte
	var err error
	switch {
	case target == "pdf":
		var pics []*pdfw.Picture
		for i, d := range datas {
			pic, perr := purego.PictureOf(sources[i], d)
			if perr != nil {
				e := &Error{Code: CodeConvertFailed, File: names[i], Detail: perr.Error()}
				return failed(h, e)
			}
			pics = append(pics, pic)
		}
		out, err = pdfw.Pictures(pics, stems[0])
	case formats.CategoryOf(target) == formats.Archive:
		out, err = purego.PackEntries(target, names, datas)
	default:
		return mergedVideo(h, in, sources, names, datas, target, params, name)
	}
	if err != nil {
		e := &Error{Code: CodeConvertFailed, Detail: err.Error()}
		return failed(h, e)
	}
	ref, err := h.WriteOutput(name, out)
	if err != nil {
		e := &Error{Code: CodeWriteFailed, Detail: err.Error()}
		return failed(h, e)
	}
	h.Progress(total, total, "")
	return &wire.ActionRunOutput{OK: true, Outputs: []wire.OutputRef{ref}, Message: mergedSummary(len(in.Inputs), target)}
}

// mergedVideo feeds every image as a PNG frame to one ffmpeg run.
func mergedVideo(h Host, in *wire.ActionRunInput, sources, names []string, datas [][]byte, target string, params options.Options, outName string) *wire.ActionRunOutput {
	inputs := map[string]string{}
	for i, d := range datas {
		frame := d
		if sources[i] != "png" {
			conv, err := purego.Convert(sources[i], "png", d, params)
			if err != nil {
				e := &Error{Code: CodeConvertFailed, File: names[i], Detail: err.Error()}
				return failed(h, e)
			}
			frame = conv
		}
		fname := "frame-" + pad3(i+1) + ".png"
		ref, err := h.WriteOutput(fname, frame)
		if err != nil {
			e := &Error{Code: CodeWriteFailed, Detail: err.Error()}
			return failed(h, e)
		}
		inputs[fname] = ref.Ref
	}
	inv := engines.Slideshow(target, len(datas), "out", params)
	res, err := h.EngineRun(pluginkit.EngineRequest{Engine: inv.Engine, Args: inv.Args, Inputs: inputs, Outputs: inv.Outputs, TimeoutS: inv.TimeoutS})
	if err != nil {
		e := &Error{Code: CodeEngineFailed, Detail: err.Error(), Engines: []string{inv.Engine}}
		return failed(h, e)
	}
	if res.Exit != 0 || len(res.Outputs) == 0 {
		e := &Error{Code: CodeEngineFailed, Detail: "ffmpeg exit " + strconv.Itoa(res.Exit) + ": " + lastLine(res.StderrTail), Engines: []string{inv.Engine}}
		return failed(h, e)
	}
	pick := res.Outputs[0]
	for _, o := range res.Outputs {
		if o.Name == inv.Outputs[0] {
			pick = o
		}
	}
	total := int64(len(in.Inputs))
	h.Progress(total, total, "")
	return &wire.ActionRunOutput{OK: true, Outputs: []wire.OutputRef{{Ref: pick.Ref, Name: outName}}, Message: mergedSummary(len(in.Inputs), target)}
}

// failed ends a merged run on one failure: the person reads the reason in
// their language (Error.Text), the log keeps the technical detail.
func failed(h Host, e *Error) *wire.ActionRunOutput {
	line := e.Error()
	if e.File != "" {
		line = e.File + ": " + line
	}
	h.Log("warn", "merge: "+line)
	return &wire.ActionRunOutput{OK: false, Message: e.Text()}
}

func pad3(n int) string {
	s := strconv.Itoa(n)
	for len(s) < 3 {
		s = "0" + s
	}
	return s
}

func mergedSummary(n int, target string) wire.Text {
	return i18n.T("job.merged", "n", n, "format", labels(target))
}
