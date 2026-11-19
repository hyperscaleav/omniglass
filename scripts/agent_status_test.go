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

func TestAgentStatusSaysWhyAFailedRunStopped(t *testing.T) {
	got, _ := agentStatus(t, "body", `{"workflow":"agent-build","state":"failed","run_url":"`+runURL+`","reason":"It used all 60 of its turns.","summary":"Apply run:dev-gha again to retry."}`)
	if !strings.Contains(got, "stopped before it finished ([run]("+runURL+")). It used all 60 of its turns. Nothing was pushed.") ||
		!strings.Contains(got, "> Apply run:dev-gha again to retry.") {
		t.Fatalf("got %q", got)
	}
}

func TestAgentStatusConclusion(t *testing.T) {
	for _, c := range []struct{ facts, want string }{
		{`{"state":"done","pushed":"abc1234"}`, "success"},
		{`{"state":"done","pushed":""}`, "neutral"},
		{`{"state":"done"}`, "neutral"},
		{`{"state":"failed","pushed":"abc1234"}`, "failure"},
		{`{"state":"failed","pushed":""}`, "failure"},
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

func agentStopReason(t *testing.T, notes string) (string, int) {
	t.Helper()
	requireTools(t)
	cmd := exec.Command("bash", "agent-stop-reason.sh")
	cmd.Stdin = strings.NewReader(notes)
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

func TestAgentStopReasonNamesTheTurnCap(t *testing.T) {
	got, _ := agentStopReason(t, `[
		{"annotation_level":"warning","message":"Node.js 20 is deprecated."},
		{"annotation_level":"failure","message":"Process completed with exit code 1."},
		{"annotation_level":"failure","message":"Action failed with error: SDK execution error: Error: Claude Code returned an error result: Reached maximum number of turns (60)"}]`)
	if got != "It used all 60 of its turns." {
		t.Fatalf("got %q", got)
	}
}

func TestAgentStopReasonGivesAnyOtherError(t *testing.T) {
	got, _ := agentStopReason(t, `[{"annotation_level":"failure","message":"Process completed with exit code 1."},
		{"annotation_level":"failure","message":"Action failed with error:\n  credit balance too low"}]`)
	if got != "It failed with: Action failed with error: credit balance too low" {
		t.Fatalf("got %q", got)
	}
}

func TestAgentStopReasonSaysNothingWithoutAnError(t *testing.T) {
	if got, _ := agentStopReason(t, `[{"annotation_level":"failure","message":"Process completed with exit code 1."}]`); got != "" {
		t.Fatalf("got %q", got)
	}
	if _, code := agentStopReason(t, `{}`); code != 2 {
		t.Fatalf("object: exit %d, want 2", code)
	}
}

const pushedSHA = "abc1234aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"

// agent-fix answers each point of the feedback on one line, with its reasons
// collapsed under them (#2224). A point counts as fixed only when its sha is a
// commit the run pushed.
func TestAgentStatusListsEachPointOnOneLine(t *testing.T) {
	got, _ := agentStatus(t, "body", `{"workflow":"agent-fix","state":"done","run_url":"`+runURL+`","pushed":"abc1234",
		"commits":["`+pushedSHA+`"],
		"points":[
			{"point":"Retry\nbacks off","status":"fixed","sha":"abc1234","reason":"added backoff and a test","comment_id":5},
			{"point":"Rename the flag","status":"unchanged","reason":"the name is public config"},
			{"point":"Claimed","status":"fixed","sha":"deadbee"}]}`)
	for _, want := range []string{
		"**agent-fix** pushed abc1234",
		"> ✅ Retry backs off · Fixed in abc1234\n> ↩️ Rename the flag · Not changed\n> ↩️ Claimed · Not changed",
		"> <details>\n> <summary>Why</summary>",
		"> - Retry backs off: added backoff and a test\n> - Rename the flag: the name is public config",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %q in:\n%s", want, got)
		}
	}
}

// Without points the comment is what it was: the agent's summary, quoted.
func TestAgentStatusKeepsTheSummaryWithoutPoints(t *testing.T) {
	got, _ := agentStatus(t, "body", `{"workflow":"agent-fix","state":"done","pushed":"abc1234","summary":"did a thing"}`)
	if !strings.HasSuffix(got, "\n\n> did a thing") || strings.Contains(got, "<details>") {
		t.Fatalf("got %q", got)
	}
}

// agent-fix replies on each review thread it fixed, and only those: a point
// that was not changed, came from no thread, or names a commit the run did not
// push gets no reply.
func TestAgentStatusRepliesOnTheThreadsItFixed(t *testing.T) {
	got, code := agentStatus(t, "replies", `{"commits":["`+pushedSHA+`"],"points":[
		{"point":"a","status":"fixed","sha":"abc1234","comment_id":5},
		{"point":"a again","status":"fixed","sha":"abc1234aa","comment_id":5},
		{"point":"b","status":"unchanged","comment_id":6},
		{"point":"c","status":"fixed","sha":"abc1234"},
		{"point":"d","status":"fixed","sha":"deadbee","comment_id":7},
		{"point":"e","status":"fixed","sha":"abc1234","comment_id":"8; rm -rf"}]}`)
	if code != 0 || got != `[{"comment_id":5,"body":"✅ Fixed in abc1234"}]` {
		t.Fatalf("exit %d: %s", code, got)
	}
	if got, _ := agentStatus(t, "replies", `{}`); got != "[]" {
		t.Fatalf("no points: %s", got)
	}
}
