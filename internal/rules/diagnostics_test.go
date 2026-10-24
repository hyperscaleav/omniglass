package rules

import (
	"errors"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/expression"
)

// The starter diagnostics (#1054) on the sites as they are declared, not
// only on the fixtures' site-shaped environments.

func systemEnv(t *testing.T, output string, inputs ...TypedInput) expression.Env {
	t.Helper()
	env, err := CalcEnv(OwnerSystem, inputs, output)
	if err != nil {
		t.Fatal(err)
	}
	return env
}

var tempInput = TypedInput{Alias: "t", Key: "room_temperature", ValueType: ValueNumber}

// sum(members, .has.t) > 0 is refused as sum-of-bools on the sum, not as
// Expr's operator mismatch at the >, at validate and at create alike, and
// its fix yields an expression that compiles.
func TestSumOfBoolsOnTheCalcSite(t *testing.T) {
	env := systemEnv(t, ValueBool, tempInput)
	src := "sum(members, .has.t) > 0"
	_, err := CheckCalc(src, env)
	var ee *expression.Error
	if !errors.As(err, &ee) || ee.Code != "sum-of-bools" || ee.Line != 1 || ee.Column != 1 || ee.Fix == nil {
		t.Fatalf("CheckCalc = %v (%+v), want sum-of-bools at 1:1 with a fix", err, ee)
	}
	_, err = CompileCalc(OwnerSystem, []TypedInput{tempInput}, CalcReduce{Expr: src}, ValueBool)
	if !errors.As(err, &ee) || ee.Code != "sum-of-bools" {
		t.Fatalf("CompileCalc = %v, want sum-of-bools", err)
	}
	fixed, err := expression.Apply(src, *ee.Fix)
	if err != nil || fixed != "count(members, .has.t) > 0" {
		t.Fatalf("fixed = %q, %v", fixed, err)
	}
	if _, err := CompileCalc(OwnerSystem, []TypedInput{tempInput}, CalcReduce{Expr: fixed}, ValueBool); err != nil {
		t.Fatalf("the fixed reduce does not compile: %v", err)
	}
}

// A calc rule's env names its aliases as its inputs, so an alias the reduce
// never names is a warning, which never refuses the rule.
func TestUnusedAliasOnTheCalcSite(t *testing.T) {
	up := TypedInput{Alias: "up", Key: "power", ValueType: ValueBool}
	env := systemEnv(t, ValueNumber, tempInput, up)
	if got := env.Inputs(); len(got) != 2 || got[0] != "t" || got[1] != "up" {
		t.Fatalf("Inputs() = %v", got)
	}
	diags, err := CheckCalc("max(t)", env)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || diags[0].Code != "unused-alias" || diags[0].Severity != expression.SeverityWarning || diags[0].Line != 0 {
		t.Fatalf("diagnostics = %+v, want one unused-alias warning with no position", diags)
	}
	p, err := CompileCalc(OwnerSystem, []TypedInput{tempInput, up}, CalcReduce{Expr: "max(t)"}, ValueNumber)
	if err != nil {
		t.Fatalf("a warning refused the rule: %v", err)
	}
	if ds := p.Program.Diagnostics(); len(ds) != 1 || ds[0].Code != "unused-alias" {
		t.Errorf("the compiled reduce carries %+v", ds)
	}
}

// A criteria's float equality is a warning on a rule that still compiles;
// unused-alias is calc's alone.
func TestFloatEqualityOnTheCriteriaSite(t *testing.T) {
	diags, err := expression.Check("value == 1.5", metricEnv)
	if err != nil {
		t.Fatal(err)
	}
	if len(diags) != 1 || diags[0].Code != "float-equality" || diags[0].Column != 7 {
		t.Fatalf("diagnostics = %+v", diags)
	}
	if _, err := Compile(LaneMetric, "", Criteria{Expr: "value == 1.5"}, nil); err != nil {
		t.Fatalf("a warning refused the criteria: %v", err)
	}
}
