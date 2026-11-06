// Command gotestreport runs go test with -json and prints what a plain go test
// would, then ends the log with the run's slowest tests and the wall time of
// each package (#1536). The CI test shards use it so the slow tests are named
// where the gate runs, which is what the work of making them faster starts
// from; the local make targets use it too, so the two cannot drift (#214).
//
//	go run ./cmd/gotestreport [-n 20] -- <go test arguments>
//
// A passing package prints its "ok" line only. A failing package prints
// everything its failing tests said, and a build error prints as it arrives,
// so a red run reads as a red go test run does. Only top-level tests are
// ranked: a subtest's time is already inside its parent's. The exit status is
// go test's own.
package main

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"sort"
	"strings"
)

// event is the subset of test2json's output the report reads. A build error
// carries an ImportPath and no Package.
type event struct {
	Action     string
	Package    string
	ImportPath string
	Test       string
	Output     string
	Elapsed    float64
}

type testTime struct {
	Package string
	Name    string
	Seconds float64
}

type packageTime struct {
	Name    string
	Seconds float64
	Passed  bool
}

// Summary is what a stream said: the timings the report ranks, and whether
// anything in it failed.
type Summary struct {
	Failed   bool
	Tests    []testTime
	Packages []packageTime
	// NoTests counts packages that were listed but have no test files.
	NoTests int
}

// outputLine is one line of a package's output, kept in arrival order with the
// test it belongs to ("" for the package itself).
type outputLine struct {
	test string
	text string
}

type pending struct {
	lines   []outputLine
	results map[string]string // test name -> pass, fail or skip
}

// Read consumes a go test -json stream, writing to live what a plain go test
// would have printed, and returns the timings. A line that is not JSON is
// passed through, so go test's own diagnostics are never lost.
func Read(r io.Reader, live io.Writer) (Summary, error) {
	var sum Summary
	open := map[string]*pending{}
	br := bufio.NewReader(r)
	for {
		line, err := br.ReadString('\n')
		if line != "" {
			sum.take(line, live, open)
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return sum, err
		}
	}
	// A package that never reported a result (the binary was killed) still
	// says what it said, and counts as failed.
	for _, p := range open {
		sum.Failed = true
		flush(live, p)
	}
	return sum, nil
}

func (s *Summary) take(line string, live io.Writer, open map[string]*pending) {
	var e event
	if err := json.Unmarshal([]byte(line), &e); err != nil || e.Action == "" {
		io.WriteString(live, line)
		return
	}
	if e.Package == "" {
		// Build output and build failures, which have no package yet.
		if e.Output != "" {
			io.WriteString(live, e.Output)
		}
		if e.Action == "build-fail" {
			s.Failed = true
		}
		return
	}
	p := open[e.Package]
	if p == nil {
		p = &pending{results: map[string]string{}}
		open[e.Package] = p
	}
	switch e.Action {
	case "output":
		p.lines = append(p.lines, outputLine{e.Test, e.Output})
	case "pass", "fail", "skip":
		if e.Test != "" {
			p.results[e.Test] = e.Action
			if e.Action == "fail" {
				s.Failed = true
			}
			if e.Action != "skip" && !strings.Contains(e.Test, "/") {
				s.Tests = append(s.Tests, testTime{e.Package, e.Test, e.Elapsed})
			}
			return
		}
		delete(open, e.Package)
		if e.Action == "fail" {
			s.Failed = true
			flush(live, p)
		} else {
			for _, l := range p.lines {
				if l.test == "" && (strings.HasPrefix(l.text, "ok ") || strings.HasPrefix(l.text, "?")) {
					io.WriteString(live, l.text)
				}
			}
		}
		if e.Action == "skip" {
			s.NoTests++
		} else {
			s.Packages = append(s.Packages, packageTime{e.Package, e.Elapsed, e.Action == "pass"})
		}
	}
}

// flush prints a failing package's output: its own lines, and those of every
// test that did not pass or skip (a test still running when the binary died has
// no result, and is the likeliest cause).
func flush(w io.Writer, p *pending) {
	for _, l := range p.lines {
		if l.test != "" {
			if r := p.results[l.test]; r == "pass" || r == "skip" {
				continue
			}
		}
		io.WriteString(w, l.text)
	}
}

// Report writes the n slowest top-level tests and every package's wall time,
// slowest first.
func (s Summary) Report(w io.Writer, n int) {
	if len(s.Tests) == 0 && len(s.Packages) == 0 {
		fmt.Fprintln(w, "\nTiming report: no tests ran")
		return
	}
	tests := append([]testTime(nil), s.Tests...)
	sort.SliceStable(tests, func(i, j int) bool { return tests[i].Seconds > tests[j].Seconds })
	if len(tests) > n {
		tests = tests[:n]
	}
	fmt.Fprintf(w, "\nSlowest %d tests (seconds, test, package):\n", len(tests))
	for _, t := range tests {
		fmt.Fprintf(w, "%9.1fs  %s  %s\n", t.Seconds, t.Name, t.Package)
	}
	pkgs := append([]packageTime(nil), s.Packages...)
	sort.SliceStable(pkgs, func(i, j int) bool { return pkgs[i].Seconds > pkgs[j].Seconds })
	fmt.Fprintf(w, "\nPackage wall time (seconds, package; %d without test files omitted):\n", s.NoTests)
	for _, p := range pkgs {
		status := "ok"
		if !p.Passed {
			status = "FAIL"
		}
		fmt.Fprintf(w, "%9.1fs  %s  %s\n", p.Seconds, p.Name, status)
	}
}

func main() {
	n := flag.Int("n", 20, "how many of the slowest tests to list")
	flag.Parse()
	os.Exit(run(flag.Args(), *n, os.Stdout, os.Stderr))
}

// run executes go test -json with args, prints through Read, then the report.
// The exit status is go test's.
func run(args []string, n int, stdout, stderr io.Writer) int {
	cmd := exec.Command("go", append([]string{"test", "-json"}, args...)...)
	cmd.Stderr = stderr
	out, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Fprintf(stderr, "gotestreport: %v\n", err)
		return 2
	}
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(stderr, "gotestreport: %v\n", err)
		return 2
	}
	sum, readErr := Read(out, stdout)
	waitErr := cmd.Wait()
	sum.Report(stdout, n)
	if readErr != nil {
		fmt.Fprintf(stderr, "gotestreport: %v\n", readErr)
		return 2
	}
	var exit *exec.ExitError
	switch {
	case errors.As(waitErr, &exit):
		return exit.ExitCode()
	case waitErr != nil:
		fmt.Fprintf(stderr, "gotestreport: %v\n", waitErr)
		return 2
	}
	return 0
}
