package scripts

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// These drive scripts/agent-trust.sh, the gate in front of the agent
// workflows (#2175): a run:dev-gha label starts a build only from a
// maintainer, on an issue the project's own people wrote; a fix starts only
// from the maintainers or contributors, or from the reviewer App within its
// round cap, after which the PR parks for a person; the reviewer's latest
// review on the head is the check; and only those teams' comments reach the
// model.

func agentTrust(t *testing.T, mode, facts string) (string, int) {
	t.Helper()
	requireTools(t)
	cmd := exec.Command("bash", "agent-trust.sh", mode)
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

const teams = `"maintainers":["Fred"],"contributors":["casey"]`

func buildFacts(labeler, author, labels string, openPRs string) string {
	return `{"labeler":"` + labeler + `","author":"` + author + `","labels":[` + labels + `],"open_prs":` + openPRs + `,` + teams + `}`
}

func TestAgentTrustBuildRunsForAMaintainersLabelOnATeamIssue(t *testing.T) {
	for _, author := range []string{"fred", "casey"} {
		if got, _ := agentTrust(t, "build", buildFacts("fred", author, `"run:dev-gha","tier:mid"`, "0")); got != "run" {
			t.Fatalf("author %s: got %q, want run", author, got)
		}
	}
}

func TestAgentTrustBuildRefusesALabelFromOutsideTheMaintainers(t *testing.T) {
	for _, labeler := range []string{"casey", "stranger", ""} {
		got, _ := agentTrust(t, "build", buildFacts(labeler, "fred", `"run:dev-gha"`, "0"))
		if !strings.HasPrefix(got, "refuse:") {
			t.Fatalf("labeler %q: got %q, want a refusal", labeler, got)
		}
	}
}

func TestAgentTrustBuildRefusesAnIssueFromOutsideTheTeams(t *testing.T) {
	got, _ := agentTrust(t, "build", buildFacts("fred", "stranger", `"run:dev-gha"`, "0"))
	if !strings.Contains(got, "@stranger") || !strings.HasPrefix(got, "refuse:") {
		t.Fatalf("got %q, want a refusal naming the author", got)
	}
}

func TestAgentTrustBuildRefusesWhenAPRIsAlreadyOpen(t *testing.T) {
	got, _ := agentTrust(t, "build", buildFacts("fred", "fred", `"run:dev-gha"`, "1"))
	if !strings.Contains(got, "open PR") {
		t.Fatalf("got %q, want the open-PR refusal", got)
	}
}

func TestAgentTrustBuildRefusesWhileABlockerIsOpen(t *testing.T) {
	facts := `{"labeler":"fred","author":"fred","labels":[],"open_prs":0,"blocked_by":[12,34],` + teams + `}`
	if got, _ := agentTrust(t, "build", facts); got != "refuse: blocked by open #12, #34" {
		t.Fatalf("got %q, want the blocker refusal naming both", got)
	}
	facts = `{"labeler":"fred","author":"fred","labels":[],"open_prs":0,"blocked_by":[],` + teams + `}`
	if got, _ := agentTrust(t, "build", facts); got != "run" {
		t.Fatalf("no open blockers: got %q, want run", got)
	}
}

func TestAgentTrustBuildRefusesWhenTheBlockersCannotBeRead(t *testing.T) {
	facts := `{"labeler":"fred","author":"fred","labels":[],"open_prs":0,"blockers_unreadable":true,` + teams + `}`
	if got, _ := agentTrust(t, "build", facts); !strings.Contains(got, "blockers could not be read") {
		t.Fatalf("got %q, want the fail-closed refusal", got)
	}
}

func fixFacts(fields string) string {
	return `{` + fields + `,"app_bot":"hyperscale-agent[bot]",` + teams + `}`
}

func TestAgentTrustFixRunsForATeamMembersMention(t *testing.T) {
	for _, sender := range []string{"Fred", "casey"} {
		got, _ := agentTrust(t, "fix", fixFacts(`"event":"comment","sender":"`+sender+`","mentions":true,"head_ref":"fix/x","fork":false`))
		if got != "run" {
			t.Fatalf("sender %s: got %q, want run", sender, got)
		}
	}
}

func TestAgentTrustFixSkipsAnyoneOutsideTheTeams(t *testing.T) {
	got, _ := agentTrust(t, "fix", fixFacts(`"event":"comment","sender":"stranger","mentions":true,"head_ref":"fix/x","fork":false`))
	if !strings.HasPrefix(got, "skip:") {
		t.Fatalf("got %q, want a skip", got)
	}
}

func TestAgentTrustFixSkipsForks(t *testing.T) {
	got, _ := agentTrust(t, "fix", fixFacts(`"event":"comment","sender":"fred","mentions":true,"head_ref":"fix/x","fork":true`))
	if !strings.HasPrefix(got, "skip:") {
		t.Fatalf("got %q, want a skip", got)
	}
}

// Actions is the only builder, so a branch the retired harness left behind
// takes fixes like any other.
func TestAgentTrustFixTakesFeedbackOnAnOldHarnessBranch(t *testing.T) {
	got, _ := agentTrust(t, "fix", fixFacts(`"event":"comment","sender":"fred","mentions":true,"head_ref":"agent/issue-12","fork":false`))
	if got != "run" {
		t.Fatalf("got %q, want run", got)
	}
}

func TestAgentTrustFixTakesAReviewThatRequestsChangesOrMentions(t *testing.T) {
	cases := map[string]string{
		`"review_state":"CHANGES_REQUESTED","mentions":false`: "run",
		`"review_state":"COMMENTED","mentions":true`:          "run",
		`"review_state":"APPROVED","mentions":false`:          "skip",
	}
	for f, want := range cases {
		got, _ := agentTrust(t, "fix", fixFacts(`"event":"review","sender":"fred","head_ref":"fix/x","fork":false,`+f))
		if !strings.HasPrefix(got, want) {
			t.Fatalf("%s: got %q, want %s", f, got, want)
		}
	}
}

func TestAgentTrustFixCapsTheReviewersAutomaticRounds(t *testing.T) {
	for rounds, want := range map[string]string{"1": "run", "5": "run", "6": "park"} {
		got, _ := agentTrust(t, "fix", fixFacts(`"event":"review","sender":"hyperscale-agent[bot]","review_state":"CHANGES_REQUESTED","auto_rounds":`+rounds+`,"head_ref":"fix/x","fork":false`))
		if !strings.HasPrefix(got, want) {
			t.Fatalf("round %s: got %q, want %s", rounds, got, want)
		}
	}
	got, _ := agentTrust(t, "fix", fixFacts(`"event":"comment","sender":"hyperscale-agent[bot]","mentions":true,"head_ref":"fix/x","fork":false`))
	if !strings.HasPrefix(got, "skip:") {
		t.Fatalf("the App's own comment: got %q, want a skip", got)
	}
}

func verdictFacts(reviews string) string {
	return `{"app_bot":"hyperscale-agent[bot]","head_sha":"h2","reviews":[` + reviews + `]}`
}

func TestAgentTrustVerdictFollowsTheReviewersLatestReviewOnTheHead(t *testing.T) {
	cases := map[string]string{
		// Changes requested on an older head do not hold back the new one.
		`{"user":"hyperscale-agent[bot]","state":"CHANGES_REQUESTED","commit_id":"h1"},{"user":"Hyperscale-Agent[bot]","state":"COMMENTED","commit_id":"h2"}`: "ship",
		`{"user":"hyperscale-agent[bot]","state":"COMMENTED","commit_id":"h2"},{"user":"hyperscale-agent[bot]","state":"CHANGES_REQUESTED","commit_id":"h2"}`: "block: the reviewer requests changes",
		// Someone else's review is not the reviewer's verdict.
		`{"user":"fred","state":"APPROVED","commit_id":"h2"}`: "block: the reviewer posted no review on this head",
		``: "block: the reviewer posted no review on this head",
	}
	for reviews, want := range cases {
		if got, _ := agentTrust(t, "verdict", verdictFacts(reviews)); got != want {
			t.Fatalf("%s: got %q, want %q", reviews, got, want)
		}
	}
}

func TestAgentTrustActorsListsBothTeamsOnce(t *testing.T) {
	got, _ := agentTrust(t, "actors", `{"maintainers":["fred","casey"],"contributors":["casey","dana"]}`)
	if got != "casey,dana,fred" {
		t.Fatalf("got %q", got)
	}
}

func TestAgentTrustRefusesUnreadableInputAndBadUsage(t *testing.T) {
	if _, code := agentTrust(t, "build", "not json"); code != 2 {
		t.Fatalf("unreadable input: exit %d, want 2", code)
	}
	if _, code := agentTrust(t, "merge", "{}"); code != 64 {
		t.Fatalf("bad usage: exit %d, want 64", code)
	}
}

// open: agent-open-pr opens a PR as the agents' App, so the architect can
// approve it. Only a maintainer may dispatch it, only from a claude/ branch,
// and never twice for one branch.

func openFacts(sender, ref, openPRs string) string {
	return `{"sender":"` + sender + `","head_ref":"` + ref + `","open_prs":` + openPRs + `,` + teams + `}`
}

func TestAgentTrustOpenRunsForAMaintainersClaudeBranch(t *testing.T) {
	for _, ref := range []string{"claude/agent-open-pr", "claude/fix-2189", "claude/a/b.c_d-e"} {
		if got, _ := agentTrust(t, "open", openFacts("fred", ref, "0")); got != "run" {
			t.Fatalf("%s: got %q, want run", ref, got)
		}
	}
}

func TestAgentTrustOpenRefusesAnyoneButAMaintainer(t *testing.T) {
	for _, sender := range []string{"casey", "stranger", ""} {
		got, _ := agentTrust(t, "open", openFacts(sender, "claude/x", "0"))
		if !strings.HasPrefix(got, "refuse:") {
			t.Fatalf("sender %q: got %q, want refuse", sender, got)
		}
	}
}

func TestAgentTrustOpenRefusesABranchThatIsNotASessions(t *testing.T) {
	for _, ref := range []string{"main", "fix/x", "claude", "claude/", "claude/../main", "claude/a b", "claude//x", "release/0.1", "xclaude/x"} {
		got, _ := agentTrust(t, "open", openFacts("fred", ref, "0"))
		if !strings.Contains(got, "is not a claude/ branch") {
			t.Fatalf("%q: got %q, want refuse", ref, got)
		}
	}
}

func TestAgentTrustOpenRefusesABranchThatAlreadyHasAPR(t *testing.T) {
	if got, _ := agentTrust(t, "open", openFacts("fred", "claude/x", "1")); got != "refuse: claude/x already has an open PR" {
		t.Fatalf("got %q", got)
	}
}

// agent-review posts as a second App, hyperscale-reviewer, because GitHub
// refuses Request changes from the App that wrote the PR. Its review starts
// the automatic fix the same way, under the same round cap.
func TestAgentTrustFixTakesTheReviewerAppsChangesRequested(t *testing.T) {
	facts := func(sender, state, rounds string) string {
		return fixFacts(`"event":"review","sender":"` + sender + `","review_state":"` + state + `","auto_rounds":` + rounds + `,"reviewer_bot":"hyperscale-reviewer[bot]","head_ref":"fix/x","fork":false`)
	}
	cases := map[string]string{
		facts("hyperscale-reviewer[bot]", "CHANGES_REQUESTED", "1"): "run",
		facts("Hyperscale-Reviewer[bot]", "CHANGES_REQUESTED", "5"): "run",
		facts("hyperscale-reviewer[bot]", "CHANGES_REQUESTED", "6"): "park",
		facts("hyperscale-reviewer[bot]", "COMMENTED", "1"):         "skip",
	}
	for f, want := range cases {
		if got, _ := agentTrust(t, "fix", f); !strings.HasPrefix(got, want) {
			t.Fatalf("%s: got %q, want %s", f, got, want)
		}
	}
	// With no reviewer App set up, an event with no sender matches nothing.
	got, _ := agentTrust(t, "fix", fixFacts(`"event":"review","sender":"","review_state":"CHANGES_REQUESTED","reviewer_bot":"","head_ref":"fix/x","fork":false`))
	if !strings.HasPrefix(got, "skip:") {
		t.Fatalf("empty sender: got %q, want a skip", got)
	}
}

// On a PR its own App wrote, the reviewer can only comment, so a comment
// whose body opens with a blocking count still blocks. Both old and new formats
// are accepted: the old "agent-review: N blocking" and the new
// "❌ Changes requested · N blocking".
func TestAgentTrustVerdictBlocksOnACommentThatCarriesBlockingFindings(t *testing.T) {
	cases := map[string]string{
		// Old format.
		`"body":"agent-review: 2 blocking, 1 advisory (lenses: a)"`:  "block: the reviewer found 2 blocking issue(s) and could only comment, since its App wrote this PR",
		`"body":"agent-review: 0 blocking, 3 advisory (lenses: a)"`:  "ship",
		`"body":"agent-review: swept, nothing survived (lenses: a)"`: "ship",
		`"body":"looks fine; agent-review: 2 blocking"`:              "ship",
		// New compact format.
		`"body":"❌ Changes requested · 2 blocking · 1 advisory"`: "block: the reviewer found 2 blocking issue(s) and could only comment, since its App wrote this PR",
		`"body":"✅ Ship · nothing found"`:                        "ship",
		`"body":"⚠️ 3 advisory"`:                                 "ship",
	}
	for body, want := range cases {
		reviews := `{"user":"hyperscale-agent[bot]","state":"COMMENTED","commit_id":"h2",` + body + `}`
		if got, _ := agentTrust(t, "verdict", verdictFacts(reviews)); got != want {
			t.Fatalf("%s: got %q, want %q", body, got, want)
		}
	}
}
