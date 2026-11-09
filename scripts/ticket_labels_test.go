package scripts

import (
	"bytes"
	"os/exec"
	"strings"
	"testing"
)

// These drive scripts/ticket-labels.sh, the labeler behind the ticket-labels
// workflow (#1602): an issue form's Tier and Risk dropdowns land in the body as
// "### Tier" and "### Risk" sections, since a form cannot apply a label from a
// dropdown, and the script turns each answer into the matching tier:* or
// risk:* label, removing a conflicting one of the same family. Its plan mode
// prints the moves without touching GitHub, which is what these assert.

func ticketLabelsPlan(t *testing.T, issue string) (string, string, int) {
	t.Helper()
	requireTools(t)
	cmd := exec.Command("bash", "ticket-labels.sh", "plan")
	cmd.Stdin = strings.NewReader(issue)
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

func formBody(tier, risk string) string {
	return "### Goal\n\nSomething.\n\n### Tier\n\n" + tier + "\n\n### Risk\n\n" + risk + "\n"
}

func issueJSON(body string, labels ...string) string {
	var b strings.Builder
	b.WriteString(`{"body":`)
	b.WriteString(strconvQuote(body))
	b.WriteString(`,"labels":[`)
	for i, l := range labels {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(`{"name":"` + l + `"}`)
	}
	b.WriteString("]}")
	return b.String()
}

func strconvQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	s = strings.ReplaceAll(s, "\n", `\n`)
	return `"` + s + `"`
}

func TestTicketLabelsAppliesBothAnswers(t *testing.T) {
	out, errs, code := ticketLabelsPlan(t, issueJSON(formBody("tier:sr", "risk:high"), "area:api"))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if out != "add tier:sr\nadd risk:high\n" {
		t.Fatalf("plan:\n%s", out)
	}
}

func TestTicketLabelsReplacesAConflictingLabelOfTheSameFamily(t *testing.T) {
	out, _, code := ticketLabelsPlan(t, issueJSON(formBody("tier:jr", "risk:low"), "tier:mid", "tier:sr", "risk:low", "area:ui"))
	if code != 0 {
		t.Fatalf("exit %d", code)
	}
	if out != "remove tier:mid\nremove tier:sr\nadd tier:jr\n" {
		t.Fatalf("plan:\n%s", out)
	}
}

func TestTicketLabelsChangesNothingWhenTheLabelsAlreadyMatch(t *testing.T) {
	out, _, code := ticketLabelsPlan(t, issueJSON(formBody("tier:mid", "risk:low"), "tier:mid", "risk:low"))
	if code != 0 || out != "" {
		t.Fatalf("exit %d, plan:\n%s", code, out)
	}
}

func TestTicketLabelsLeavesAnIssueWithoutTheSectionsAlone(t *testing.T) {
	// An issue filed by hand or by an agent carries its labels directly and no
	// dropdown answers; the labeler must not strip them.
	out, _, code := ticketLabelsPlan(t, issueJSON("### Goal\n\nSomething.\n", "tier:sr", "risk:high"))
	if code != 0 || out != "" {
		t.Fatalf("exit %d, plan:\n%s", code, out)
	}
}

func TestTicketLabelsIgnoresAnAnswerOutsideTheFamily(t *testing.T) {
	// A hand-edited section with free text is not a label; it is reported and
	// nothing moves for that family, while the other family still lands.
	out, errs, code := ticketLabelsPlan(t, issueJSON(formBody("whatever", "risk:high")))
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if out != "add risk:high\n" || !strings.Contains(errs, "Tier") {
		t.Fatalf("plan:\n%s\nstderr:\n%s", out, errs)
	}
}

func TestTicketLabelsTakesTheOneLineAnswerWithSurroundingBlankLines(t *testing.T) {
	body := "### Tier\n\n\n  tier:sr  \n\n\n### Risk\n\nrisk:low\n\n### Files likely touched\n\n- a\n"
	out, _, code := ticketLabelsPlan(t, issueJSON(body))
	if code != 0 || out != "add tier:sr\nadd risk:low\n" {
		t.Fatalf("exit %d, plan:\n%s", code, out)
	}
}

func TestTicketLabelsRefusesUnreadableInput(t *testing.T) {
	_, errs, code := ticketLabelsPlan(t, "not json")
	if code != 2 || !strings.Contains(errs, "issue JSON") {
		t.Fatalf("exit %d: %s", code, errs)
	}
}

func TestTicketLabelsApplyNeedsAnIssueNumber(t *testing.T) {
	requireTools(t)
	cmd := exec.Command("bash", "ticket-labels.sh", "apply")
	cmd.Stdin = strings.NewReader(issueJSON(formBody("tier:mid", "risk:low")))
	var errb bytes.Buffer
	cmd.Stderr = &errb
	err := cmd.Run()
	exit, ok := err.(*exec.ExitError)
	if !ok || exit.ExitCode() != 64 || !strings.Contains(errb.String(), "usage") {
		t.Fatalf("err %v: %s", err, errb.String())
	}
}
