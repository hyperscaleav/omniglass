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
// round cap; and only those teams' comments reach the model.

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

func TestAgentTrustBuildRefusesBothRunLabels(t *testing.T) {
	got, _ := agentTrust(t, "build", buildFacts("fred", "fred", `"run:dev-gha","run:dev-hah"`, "0"))
	if !strings.Contains(got, "neither builds") {
		t.Fatalf("got %q, want the both-labels refusal", got)
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

func TestAgentTrustFixSkipsHarnessBranchesAndForks(t *testing.T) {
	for _, f := range []string{
		`"event":"comment","sender":"fred","mentions":true,"head_ref":"agent/issue-12","fork":false`,
		`"event":"comment","sender":"fred","mentions":true,"head_ref":"fix/x","fork":true`,
	} {
		if got, _ := agentTrust(t, "fix", fixFacts(f)); !strings.HasPrefix(got, "skip:") {
			t.Fatalf("%s: got %q, want a skip", f, got)
		}
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
	for rounds, want := range map[string]string{"1": "run", "2": "run", "3": "skip"} {
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
