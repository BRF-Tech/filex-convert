package engines

import (
	"testing"

	"github.com/brf-tech/filex/backend/pkg/pluginkit/officecmd"

	"github.com/brf-tech/filex-convert/internal/graph"
	"github.com/brf-tech/filex-convert/internal/options"
)

// Every office edge's command line is one the host reads (filex 0.50,
// pkg/pluginkit/officecmd - the same reader the host's office engine and the
// SDK's test host use): the target it means, PDF/A when asked, and the result
// under the name this app collects. A line the host refused would fail every
// office conversion on filex 0.50 while every unit test here stayed green.
//
// Red before filex 0.50's SDK: there was no officecmd, and the engine was
// `libreoffice`, run as a program.
func TestOfficeLines_TheHostReadsEveryOne(t *testing.T) {
	n := 0
	for _, e := range graph.Edges() {
		if e.Engine != graph.Office {
			continue
		}
		n++
		for _, pdfa := range []bool{false, true} {
			inv, err := Build(e, InputName(e.From), "out", options.Options{options.PDFA: pdfa})
			if err != nil {
				t.Fatalf("%s: %v", e.Key(), err)
			}
			cmd, err := officecmd.Parse(inv.Args)
			if err != nil {
				t.Fatalf("%s: the host refuses %q: %v", e.Key(), inv.Args, err)
			}
			if cmd.To != ext(e.To) {
				t.Errorf("%s: the host reads target %q", e.Key(), cmd.To)
			}
			if len(cmd.Inputs) != 1 || cmd.Inputs[0] != inv.Input {
				t.Errorf("%s: the host reads inputs %v, the app stages %q", e.Key(), cmd.Inputs, inv.Input)
			}
			if len(inv.Outputs) != 1 || officecmd.OutputName(inv.Input, cmd.To) != inv.Outputs[0] {
				t.Errorf("%s: the host names the result %q, the app collects %v", e.Key(), officecmd.OutputName(inv.Input, cmd.To), inv.Outputs)
			}
			wantPDFA := pdfa && e.To == "pdf"
			if cmd.PDFA != wantPDFA {
				t.Errorf("%s pdfa=%v: the host reads PDF/A %v", e.Key(), pdfa, cmd.PDFA)
			}
		}
	}
	if n == 0 {
		t.Fatal("no office edge at all: nothing was measured")
	}
}

// ONLYOFFICE makes no HTML from a spreadsheet (its conversion API answers -7,
// measured on Document Server 9.4), so no office edge promises one.
//
// Red before 0.2.0: xlsx → html and ods → html were LibreOffice edges.
func TestOfficeEdges_NoSpreadsheetToHTML(t *testing.T) {
	for _, e := range graph.Edges() {
		if e.Engine != graph.Office || e.To != "html" {
			continue
		}
		switch e.From {
		case "xlsx", "ods", "csv", "pptx", "odp":
			t.Errorf("%s: ONLYOFFICE does not make HTML from a spreadsheet or a presentation", e.Key())
		}
	}
}
