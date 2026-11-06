package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// stream builds a go test -json stream from compact event lines.
func stream(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

const pkgA = "example.com/m/a"
const pkgB = "example.com/m/b"

func ev(action, pkg, test, output string, elapsed float64) string {
	s := `{"Action":"` + action + `","Package":"` + pkg + `"`
	if test != "" {
		s += `,"Test":"` + test + `"`
	}
	if output != "" {
		s += `,"Output":"` + output + `"`
	}
	if action == "pass" || action == "fail" || action == "skip" {
		s += `,"Elapsed":` + formatFloat(elapsed)
	}
	return s + "}"
}

func formatFloat(f float64) string { return strconv.FormatFloat(f, 'f', -1, 64) }

func TestAPassingPackagePrintsOnlyItsOkLine(t *testing.T) {
	in := stream(
		ev("run", pkgA, "TestOne", "", 0),
		ev("output", pkgA, "TestOne", "=== RUN   TestOne\\n", 0),
		ev("pass", pkgA, "TestOne", "", 1.5),
		ev("output", pkgA, "", "PASS\\n", 0),
		ev("output", pkgA, "", "ok  \\t"+pkgA+"\\t1.600s\\n", 0),
		ev("pass", pkgA, "", "", 1.6),
	)
	var live bytes.Buffer
	if _, err := Read(strings.NewReader(in), &live); err != nil {
		t.Fatal(err)
	}
	if got, want := live.String(), "ok  \t"+pkgA+"\t1.600s\n"; got != want {
		t.Errorf("live output = %q, want %q", got, want)
	}
}

func TestAFailingPackagePrintsEverythingItSaid(t *testing.T) {
	in := stream(
		ev("run", pkgA, "TestBad", "", 0),
		ev("output", pkgA, "TestBad", "=== RUN   TestBad\\n", 0),
		ev("output", pkgA, "TestBad", "    bad_test.go:9: boom\\n", 0),
		ev("fail", pkgA, "TestBad", "", 0.2),
		ev("output", pkgA, "", "FAIL\\n", 0),
		ev("output", pkgA, "", "FAIL\\t"+pkgA+"\\t0.3s\\n", 0),
		ev("fail", pkgA, "", "", 0.3),
	)
	var live bytes.Buffer
	sum, err := Read(strings.NewReader(in), &live)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"=== RUN   TestBad", "bad_test.go:9: boom", "FAIL\t" + pkgA} {
		if !strings.Contains(live.String(), want) {
			t.Errorf("live output lacks %q:\n%s", want, live.String())
		}
	}
	if !sum.Failed {
		t.Error("Failed = false for a stream with a failing package")
	}
}

func TestBuildOutputWithoutAPackageIsPassedThrough(t *testing.T) {
	in := stream(
		`{"Action":"build-output","ImportPath":"`+pkgA+` [`+pkgA+`.test]","Output":"a.go:3:1: syntax error\n"}`,
		`{"Action":"build-fail","ImportPath":"`+pkgA+` [`+pkgA+`.test]"}`,
		`{"Action":"start","Package":"`+pkgA+`"}`,
		`{"Action":"output","Package":"`+pkgA+`","Output":"FAIL\t`+pkgA+` [build failed]\n"}`,
		`{"Action":"fail","Package":"`+pkgA+`","Elapsed":0}`,
	)
	var live bytes.Buffer
	sum, err := Read(strings.NewReader(in), &live)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(live.String(), "a.go:3:1: syntax error") {
		t.Errorf("build error was swallowed:\n%s", live.String())
	}
	if !sum.Failed {
		t.Error("Failed = false for a build failure")
	}
}

func TestNonJSONLinesPassThrough(t *testing.T) {
	var live bytes.Buffer
	if _, err := Read(strings.NewReader("go: downloading x\n"), &live); err != nil {
		t.Fatal(err)
	}
	if live.String() != "go: downloading x\n" {
		t.Errorf("live = %q", live.String())
	}
}

func TestTheReportListsTheSlowestTopLevelTestsAndEveryPackage(t *testing.T) {
	in := stream(
		ev("pass", pkgA, "TestSlow", "", 9),
		ev("pass", pkgA, "TestSlow/sub", "", 8),
		ev("pass", pkgA, "TestFast", "", 1),
		ev("pass", pkgB, "TestMid", "", 4),
		ev("pass", pkgA, "", "", 12),
		ev("pass", pkgB, "", "", 5),
	)
	sum, err := Read(strings.NewReader(in), &bytes.Buffer{})
	if err != nil {
		t.Fatal(err)
	}
	var out bytes.Buffer
	sum.Report(&out, 2)
	got := out.String()
	slow, mid := strings.Index(got, "TestSlow"), strings.Index(got, "TestMid")
	if slow < 0 || mid < 0 || slow > mid {
		t.Errorf("slowest tests not listed in descending order:\n%s", got)
	}
	if strings.Contains(got, "TestFast") {
		t.Errorf("limit 2 listed a third test:\n%s", got)
	}
	if strings.Contains(got, "TestSlow/sub") {
		t.Errorf("a subtest was listed beside its parent:\n%s", got)
	}
	_, perPkg, _ := strings.Cut(got, "Package wall time")
	a, b := strings.Index(perPkg, pkgA+" "), strings.Index(perPkg, pkgB+" ")
	if a < 0 || b < 0 || a > b {
		t.Errorf("packages not listed slowest first:\n%s", got)
	}
	if !strings.Contains(got, "12.0s") || !strings.Contains(got, "5.0s") {
		t.Errorf("package wall times missing:\n%s", got)
	}
}

func TestTheReportOfAnEmptyStreamSaysSo(t *testing.T) {
	sum, _ := Read(strings.NewReader(""), &bytes.Buffer{})
	var out bytes.Buffer
	sum.Report(&out, 20)
	if !strings.Contains(out.String(), "no tests ran") {
		t.Errorf("report = %q", out.String())
	}
}

// writeModule makes a throwaway module with one test file per name.
func writeModule(t *testing.T, tests map[string]string) {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module example.com/m\n\ngo 1.22\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	for name, body := range tests {
		if err := os.WriteFile(filepath.Join(dir, name+"_test.go"), []byte("package m\n\nimport \"testing\"\n\n"+body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Chdir(dir)
}

func TestRunEndsAGreenLogWithTheReportAndExitsZero(t *testing.T) {
	writeModule(t, map[string]string{"ok": "func TestFine(t *testing.T) {}\n"})
	var out, errOut bytes.Buffer
	if code := run([]string{"./..."}, 20, &out, &errOut); code != 0 {
		t.Fatalf("exit = %d, want 0\n%s%s", code, out.String(), errOut.String())
	}
	got := out.String()
	if !strings.HasPrefix(got, "ok  \texample.com/m\t") {
		t.Errorf("log should open with the ok line:\n%s", got)
	}
	if !strings.Contains(got, "TestFine") || !strings.HasSuffix(strings.TrimSpace(got), "ok") {
		t.Errorf("log should end with the report:\n%s", got)
	}
}

func TestRunShowsAFailureAsGoTestDoesAndExitsNonZero(t *testing.T) {
	writeModule(t, map[string]string{"bad": "func TestFine(t *testing.T) {}\n\nfunc TestBroken(t *testing.T) { t.Fatal(\"boom\") }\n"})
	var out, errOut bytes.Buffer
	if code := run([]string{"./..."}, 20, &out, &errOut); code == 0 {
		t.Fatalf("exit = 0 for a failing test\n%s", out.String())
	}
	got := out.String()
	if !strings.Contains(got, "boom") || !strings.Contains(got, "--- FAIL: TestBroken") {
		t.Errorf("the failure is not shown:\n%s", got)
	}
	if strings.Contains(got, "=== RUN   TestFine") {
		t.Errorf("a passing test's output leaked into the failure:\n%s", got)
	}
	if !strings.Contains(got, "Package wall time") {
		t.Errorf("a red run still ends with the report:\n%s", got)
	}
}
