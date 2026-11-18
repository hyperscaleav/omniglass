package scripts

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// These drive scripts/agent-status.sh, the words and check conclusion an agent
// run shows on its PR or issue (#2175): working while it runs, then what it
// pushed or opened, or that it pushed nothing, with the agent's own summary.

func agentStatus(t *testing.T, mode, facts string) (string, int) {
	t.Helper()
	requireTools(t)
	cmd := exec.Command("bash", "agent-status.sh", mode)
	cmd.Stdin = strings.NewReader(facts)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if exit, ok := err.(*exec.ExitError); ok {
		code = exit.ExitCode()
	} else if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(out.String()), code
}

const runURL = "https://github.com/o/r/actions/runs/1"

func TestAgentStatusSaysWorkingWithTheRunLink(t *testing.T) {
	got, _ := agentStatus(t, "body", `{"workflow":"agent-fix","state":"working","run_url":"`+runURL+`","summary":"ignored"}`)
	if !strings.HasPrefix(got, "<!-- agent-status -->") || !strings.Contains(got, "**agent-fix** is working on this") ||
		!strings.Contains(got, runURL) || strings.Contains(got, "ignored") {
		t.Fatalf("got %q", got)
	}
}

func TestAgentStatusNamesWhatAFixPushed(t *testing.T) {
	got, _ := agentStatus(t, "body", `{"workflow":"agent-fix","state":"done","run_url":"`+runURL+`","pushed":"abc1234","summary":"Fixed the lock.\nAdded a test."}`)
	if !strings.Contains(got, "**agent-fix** pushed abc1234") || !strings.Contains(got, "> Fixed the lock.\n> Added a test.") {
		t.Fatalf("got %q", got)
	}
}

func TestAgentStatusSaysNothingWasPushed(t *testing.T) {
	got, _ := agentStatus(t, "body", `{"workflow":"agent-fix","state":"done","run_url":"`+runURL+`","pushed":"","summary":"Every point was already addressed."}`)
	if !strings.Contains(got, "finished with nothing pushed") || !strings.Contains(got, "> Every point was already addressed.") {
		t.Fatalf("got %q", got)
	}
}

func TestAgentStatusLinksTheBuildsPR(t *testing.T) {
	got, _ := agentStatus(t, "body", `{"workflow":"agent-build","state":"done","pr_url":"https://github.com/o/r/pull/9"}`)
	if !strings.Contains(got, "**agent-build** opened https://github.com/o/r/pull/9") {
		t.Fatalf("got %q", got)
	}
	got, _ = agentStatus(t, "body", `{"workflow":"agent-build","state":"done"}`)
	if !strings.Contains(got, "finished without opening a PR") {
		t.Fatalf("got %q", got)
	}
}

func TestAgentStatusSaysAFailedRunStopped(t *testing.T) {
	got, _ := agentStatus(t, "body", `{"workflow":"agent-fix","state":"failed","run_url":"`+runURL+`"}`)
	if !strings.Contains(got, "stopped before it finished") || !strings.Contains(got, "Nothing was pushed.") {
		t.Fatalf("got %q", got)
	}
	got, _ = agentStatus(t, "body", `{"workflow":"agent-fix","state":"failed","pushed":"abc1234"}`)
	if !strings.Contains(got, "It pushed abc1234 first.") {
		t.Fatalf("got %q", got)
	}
}

func TestAgentStatusConclusion(t *testing.T) {
	for _, c := range []struct{ facts, want string }{
		{`{"state":"done","pushed":"abc1234"}`, "success"},
		{`{"state":"done","pushed":""}`, "neutral"},
		{`{"state":"done"}`, "neutral"},
		{`{"state":"failed","pushed":"abc1234"}`, "failure"},
	} {
		if got, _ := agentStatus(t, "conclusion", c.facts); got != c.want {
			t.Fatalf("%s: got %q, want %q", c.facts, got, c.want)
		}
	}
}

func TestAgentStatusRejectsBadInput(t *testing.T) {
	if _, code := agentStatus(t, "body", `[]`); code != 2 {
		t.Fatalf("array: exit %d, want 2", code)
	}
	if _, code := agentStatus(t, "nope", `{}`); code != 64 {
		t.Fatalf("bad mode: exit %d, want 64", code)
	}
}
