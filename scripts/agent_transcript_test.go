package scripts

import (
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// These drive scripts/agent-transcript.sh, what an agent run did (#2230): the
// shell commands it ran with their exit codes, whether make preflight and make
// test ran, and a copy of the transcript safe to keep on a public repo.

const fixRun = "testdata/agent-transcript/fix-run.json"

func agentTranscript(t *testing.T, args ...string) (string, string, int) {
	t.Helper()
	requireTools(t)
	cmd := exec.Command("bash", append([]string{"agent-transcript.sh"}, args...)...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return out.String(), errb.String(), code
}

type ranCommand struct {
	Command  string `json:"command"`
	ExitCode *int   `json:"exit_code"`
}

type ran struct {
	Ran      bool `json:"ran"`
	ExitCode *int `json:"exit_code"`
}

type agentCommands struct {
	Commands  []ranCommand `json:"commands"`
	Preflight ran          `json:"preflight"`
	MakeTest  ran          `json:"make_test"`
}

func commandsOf(t *testing.T, file string) agentCommands {
	t.Helper()
	out, errs, code := agentTranscript(t, "commands", file)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var c agentCommands
	if err := json.Unmarshal([]byte(out), &c); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	return c
}

func code(n *int) string {
	if n == nil {
		return "null"
	}
	b, _ := json.Marshal(*n)
	return string(b)
}

func TestAgentTranscriptListsEachShellCommandWithItsExitCode(t *testing.T) {
	c := commandsOf(t, fixRun)
	want := []struct{ command, exit string }{
		{"gh api repos/o/r/pulls/7/reviews/9", "0"},
		{"make test-web", "0"},
		{"make preflight", "2"}, // "Exit code 2" on an error
		{"git add api/openapi.json && git commit -m 'chore: regenerate'", "1"}, // an error with no code
		{"make preflight 2>&1 | tail -20", "0"},
		{"git push origin HEAD", "null"}, // the run hit its turn cap before it returned
	}
	if len(c.Commands) != len(want) {
		t.Fatalf("commands = %+v, want %d (Read is not a shell command)", c.Commands, len(want))
	}
	for i, w := range want {
		if c.Commands[i].Command != w.command || code(c.Commands[i].ExitCode) != w.exit {
			t.Errorf("command %d = %q exit %s, want %q exit %s", i, c.Commands[i].Command, code(c.Commands[i].ExitCode), w.command, w.exit)
		}
	}
}

// The sweep asks one question of every run: did it run the gate, and did the
// gate pass the last time. make test-web is a shard, not make test.
func TestAgentTranscriptSaysWhetherPreflightAndMakeTestRan(t *testing.T) {
	c := commandsOf(t, fixRun)
	if !c.Preflight.Ran || code(c.Preflight.ExitCode) != "0" {
		t.Errorf("preflight = ran %v exit %s, want the last run's 0", c.Preflight.Ran, code(c.Preflight.ExitCode))
	}
	if c.MakeTest.Ran || c.MakeTest.ExitCode != nil {
		t.Errorf("make_test = %+v, want not run", c.MakeTest)
	}
}

// One message per line reads the same as the array.
func TestAgentTranscriptReadsOneMessagePerLine(t *testing.T) {
	requireTools(t)
	b, err := os.ReadFile(fixRun)
	if err != nil {
		t.Fatal(err)
	}
	var msgs []json.RawMessage
	if err := json.Unmarshal(b, &msgs); err != nil {
		t.Fatal(err)
	}
	var lines bytes.Buffer
	for _, m := range msgs {
		var compact bytes.Buffer
		if err := json.Compact(&compact, m); err != nil {
			t.Fatal(err)
		}
		lines.Write(compact.Bytes())
		lines.WriteByte('\n')
	}
	jsonl := filepath.Join(t.TempDir(), "run.jsonl")
	if err := os.WriteFile(jsonl, lines.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	got, _, rc := agentTranscript(t, "commands", jsonl)
	want, _, _ := agentTranscript(t, "commands", fixRun)
	if rc != 0 || got != want {
		t.Fatalf("jsonl (exit %d) = %s\narray = %s", rc, got, want)
	}
}

// The repo is public, so its artifacts are: tokens and private keys an agent
// printed never reach one, in the transcript or in the command list.
func TestAgentTranscriptMasksTokensAndKeys(t *testing.T) {
	requireTools(t)
	// Built, not written out, so no scanner mistakes this file for a leak.
	key := " PRIVATE KEY-----"
	secrets := []string{
		"ghs_" + strings.Repeat("A", 36),
		"github_pat_11" + strings.Repeat("A", 20) + "_" + strings.Repeat("B", 20),
		"sk-ant-oat01-" + strings.Repeat("C", 24),
		"-----BEGIN RSA" + key + "\nMIIEow\n-----END RSA" + key,
	}
	msgs := []map[string]any{
		{"type": "assistant", "message": map[string]any{"content": []map[string]any{
			{"type": "tool_use", "id": "t1", "name": "Bash", "input": map[string]any{"command": "curl -H 'Authorization: token " + secrets[1] + "' x"}},
		}}},
		{"type": "user", "message": map[string]any{"content": []map[string]any{
			{"type": "tool_result", "tool_use_id": "t1", "content": strings.Join(secrets, " "), "is_error": false},
		}}},
	}
	b, _ := json.Marshal(msgs)
	file := filepath.Join(t.TempDir(), "run.json")
	if err := os.WriteFile(file, b, 0o644); err != nil {
		t.Fatal(err)
	}
	redacted, errs, rc := agentTranscript(t, "redact", file)
	if rc != 0 {
		t.Fatalf("redact: exit %d: %s", rc, errs)
	}
	commands, errs, rc := agentTranscript(t, "commands", file)
	if rc != 0 {
		t.Fatalf("commands: exit %d: %s", rc, errs)
	}
	for _, out := range []string{redacted, commands} {
		for _, s := range []string{"ghs_AAAA", "github_pat_11", "sk-ant-oat01-CCCC", "MIIEow"} {
			if strings.Contains(out, s) {
				t.Errorf("%q survived: %s", s, out)
			}
		}
	}
	if !strings.Contains(redacted, "[redacted token]") || !strings.Contains(redacted, "[redacted private key]") {
		t.Fatalf("redacted = %s", redacted)
	}
	// The rest of the transcript is kept.
	if !strings.Contains(redacted, `"tool_use_id": "t1"`) {
		t.Fatalf("redact dropped more than the secrets: %s", redacted)
	}
}

// A run that left no transcript, or a broken one, fails this script but never
// the run: the workflow warns on the exit code.
func TestAgentTranscriptRefusesAMissingOrBrokenFileAndBadUsage(t *testing.T) {
	dir := t.TempDir()
	empty := filepath.Join(dir, "empty.json")
	broken := filepath.Join(dir, "broken.json")
	_ = os.WriteFile(empty, nil, 0o644)
	_ = os.WriteFile(broken, []byte("{not json"), 0o644)
	for _, args := range [][]string{
		{"commands", filepath.Join(dir, "absent.json")},
		{"commands", empty},
		{"commands", broken},
		{"redact", broken},
	} {
		if _, _, rc := agentTranscript(t, args...); rc != 2 {
			t.Errorf("%v: exit %d, want 2", args, rc)
		}
	}
	for _, args := range [][]string{{}, {"commands"}, {"summarize", fixRun}, {"commands", fixRun, "extra"}} {
		if _, _, rc := agentTranscript(t, args...); rc != 64 {
			t.Errorf("%v: exit %d, want 64", args, rc)
		}
	}
}
