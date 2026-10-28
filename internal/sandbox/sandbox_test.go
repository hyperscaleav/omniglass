package sandbox_test

import (
	"errors"
	"strings"
	"testing"

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
