package sandbox_test

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/hyperscaleav/omniglass/internal/sandbox"
)

// The sandbox is shared by every caller that renders an operator's template
// (#618): a label rule and a notification template. These pin what a second
// caller relies on beyond what the label tests already cover: the grammar's
// refusals name the construct, the output cap is the caller's, and the output
// is returned as written (a message body keeps its lines).

func TestParseRefusesTheConstructsOutsideTheGrammar(t *testing.T) {
	for src, want := range map[string]string{
		`{{$a := printf "%1000s" .X}}{{$a = printf "%s%s" $a $a}}`: "declare a variable",
		`{{printf "%s%s" .X .X}}`:                                  "printf",
		`{{call .X}}`:                                              "call",
		`{{index .X 0}}`:                                           "index",
		`{{range .X}}{{end}}`:                                      "{{range}}",
		`{{with .X}}{{end}}`:                                       "{{with}}",
		`{{define "x"}}y{{end}}`:                                   "define another template",
	} {
		_, err := sandbox.Basic.Parse(src)
		if !errors.Is(err, sandbox.ErrUnparseable) {
			t.Errorf("Parse(%q) = %v, want ErrUnparseable", src, err)
			continue
		}
		if !strings.Contains(err.Error(), want) {
			t.Errorf("Parse(%q) = %q, want the refusal to name %q", src, err, want)
		}
	}
}

func TestExecuteRendersAMissingKeyEmptyAndKeepsLines(t *testing.T) {
	tpl, err := sandbox.Basic.Parse("{{.Component}} is {{.Severity | upper}}\n{{.NotAFact}}line two")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	got, err := tpl.Execute(sandbox.Data{"Component": "display-1", "Severity": "high"}, 100)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if want := "display-1 is HIGH\nline two"; got != want {
		t.Fatalf("Execute = %q, want %q", got, want)
	}
}

func TestExecuteRefusesPastTheCallersCap(t *testing.T) {
	tpl, err := sandbox.Basic.Parse("{{.X}}{{.X}}")
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if _, err := tpl.Execute(sandbox.Data{"X": "12345"}, 10); err != nil {
		t.Fatalf("Execute at exactly the cap: %v", err)
	}
	if _, err := tpl.Execute(sandbox.Data{"X": "123456"}, 10); !errors.Is(err, sandbox.ErrFailed) {
		t.Fatalf("Execute past the cap = %v, want ErrFailed", err)
	}
}

// The review's scenario (#618): a variadic builtin over copies of a large
// fact. The grammar admits eq and upper, so the bound has to be on the work:
// a call's arguments, the source's length, and each fact's length. The render
// stays within a few megabytes whatever the fact holds.
func TestWorkIsBoundedWhateverTheFactsHold(t *testing.T) {
	huge := strings.Repeat("x", 1<<20)
	args := strings.Repeat(" (upper .Message)", sandbox.MaxArgs)
	if _, err := sandbox.Basic.Parse("{{if eq .Message" + args + " (upper .Message)}}y{{end}}"); !errors.Is(err, sandbox.ErrUnparseable) {
		t.Fatalf("a call past MaxArgs = %v, want refused at parse", err)
	}
	if _, err := sandbox.Basic.Parse(strings.Repeat("a", sandbox.MaxSource+1)); !errors.Is(err, sandbox.ErrUnparseable) {
		t.Fatalf("a source past MaxSource = %v, want refused at parse", err)
	}
	src := strings.Repeat("{{if eq .Message"+strings.Repeat(" (upper .Message)", sandbox.MaxArgs-1)+"}}y{{end}}", 20)
	tpl, err := sandbox.Basic.Parse(src)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	if _, err := tpl.Execute(sandbox.Data{"Message": huge}, 4000); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	runtime.ReadMemStats(&after)
	if grew := after.TotalAlloc - before.TotalAlloc; grew > 8<<20 {
		t.Fatalf("a render over a 1 MB fact allocated %d bytes, want it bounded by MaxFact", grew)
	}
}

func TestAFactIsCutOnARuneBoundary(t *testing.T) {
	tpl, _ := sandbox.Basic.Parse("{{.X}}")
	got, err := tpl.Execute(sandbox.Data{"X": strings.Repeat("a", sandbox.MaxFact-1) + "é" + "tail"}, 1<<20)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != sandbox.MaxFact-1 || !utf8.ValidString(got) {
		t.Fatalf("cut fact is %d bytes (valid %v), want %d and valid UTF-8", len(got), utf8.ValidString(got), sandbox.MaxFact-1)
	}
}
