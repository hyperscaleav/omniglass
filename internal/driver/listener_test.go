package driver_test

import (
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// The listener half of the spec (#817): a request may declare the protocol's
// reply shape, so a held session hands an exchange only its own answer; a
// spec may declare what happens to an inbound line no listener claims.

func TestValidateReplyShapeAndUnmatchedPolicy(t *testing.T) {
	s := validSpec()
	s.Polls[0].Request.Reply = &driver.Reply{Prefix: "INPUT "}
	s.Commands[0].Request.Reply = &driver.Reply{Regex: `^INPUT \S+$`}
	s.Unmatched = driver.UnmatchedCollectionFailed
	if err := s.Validate(catalog()); err != nil {
		t.Fatalf("a spec with reply shapes and an unmatched policy refused: %v", err)
	}
	for _, p := range []string{"", driver.UnmatchedDiscard, driver.UnmatchedCollectionFailed} {
		s.Unmatched = p
		if err := s.Validate(catalog()); err != nil {
			t.Fatalf("unmatched %q refused: %v", p, err)
		}
	}

	cases := []struct {
		name   string
		mutate func(*driver.Spec)
		want   string
	}{
		{"reply with two rules", func(s *driver.Spec) {
			s.Polls[0].Request.Reply = &driver.Reply{Prefix: "A", Regex: "B"}
		}, "reply"},
		{"reply with no rule", func(s *driver.Spec) { s.Polls[0].Request.Reply = &driver.Reply{} }, "reply"},
		{"reply with a bad regex", func(s *driver.Spec) { s.Commands[0].Request.Reply = &driver.Reply{Regex: "("} }, "reply"},
		{"unknown unmatched policy", func(s *driver.Spec) { s.Unmatched = "shrug" }, "shrug"},
		{"reply on a request with no line", func(s *driver.Spec) {
			s.Transport = "snmp"
			s.Commands = nil
			s.Listeners = nil
			s.Polls[0].Request = driver.Request{Get: []string{"1.3.6.1.2.1.1.3.0"}, Reply: &driver.Reply{Prefix: "x"}}
			s.Polls[0].Emits[0].Extract = driver.Extract{OID: "1.3.6.1.2.1.1.3.0"}
		}, "reply"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			s := validSpec()
			tc.mutate(s)
			err := s.Validate(catalog())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("err = %v, want a refusal naming %q", err, tc.want)
			}
		})
	}
}

func TestMatchMatches(t *testing.T) {
	if !(driver.Match{Prefix: "EVT "}).Matches("EVT INPUT hdmi2") || (driver.Match{Prefix: "EVT "}).Matches("INPUT hdmi2") {
		t.Fatal("prefix match disagrees")
	}
	if !(driver.Match{Regex: `^INPUT \S+$`}).Matches("INPUT hdmi1") || (driver.Match{Regex: `^INPUT \S+$`}).Matches("EVT INPUT hdmi1") {
		t.Fatal("regex match disagrees")
	}
	var none *driver.Reply
	if !none.Accepts("anything") {
		t.Fatal("no declared reply shape accepts any line")
	}
	if (&driver.Reply{Prefix: "INPUT "}).Accepts("OK") {
		t.Fatal("a declared reply shape accepted a line it does not match")
	}
}

// A rendered command keeps its binding's reply shape, so the node can match
// the device's answer over a held session.
func TestRenderRequestKeepsTheReplyShape(t *testing.T) {
	req, err := driver.RenderRequest(driver.Request{Line: "SET INPUT ${value}", Reply: &driver.Reply{Prefix: "INPUT "}}, "hdmi3", nil)
	if err != nil {
		t.Fatal(err)
	}
	if req.Line != "SET INPUT hdmi3" || req.Reply == nil || req.Reply.Prefix != "INPUT " {
		t.Fatalf("rendered = %+v", req)
	}
}

// Review of #817, finding 1: a reply shape may declare the protocol's error
// shape, so a refusal answers the exchange as a failure instead of timing it
// out.
func TestReplyErrorShape(t *testing.T) {
	r := &driver.Reply{Prefix: "INPUT ", Error: &driver.Match{Prefix: "-ERR"}}
	if !r.Accepts("INPUT hdmi1") || r.Accepts("-ERR unknown command: SET INPUT x") {
		t.Fatal("the answer shape disagrees")
	}
	if !r.Fails("-ERR unknown command: SET INPUT x") || r.Fails("INPUT hdmi1") {
		t.Fatal("the error shape disagrees")
	}
	if (&driver.Reply{Prefix: "INPUT "}).Fails("-ERR x") {
		t.Fatal("a reply with no error shape declared a failure")
	}
	for name, mutate := range map[string]func(*driver.Spec){
		"error with two rules": func(s *driver.Spec) {
			s.Commands[0].Request.Reply = &driver.Reply{Prefix: "INPUT ", Error: &driver.Match{Prefix: "-", Regex: "E"}}
		},
		"error with a bad regex": func(s *driver.Spec) {
			s.Commands[0].Request.Reply = &driver.Reply{Prefix: "INPUT ", Error: &driver.Match{Regex: "("}}
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := validSpec()
			mutate(s)
			if err := s.Validate(catalog()); err == nil || !strings.Contains(err.Error(), "error") {
				t.Fatalf("err = %v, want a refusal naming the error shape", err)
			}
		})
	}
}

// Review of #817, finding 3: a regex is Go RE2 with substring semantics, so
// an unanchored reply regex also matches a push that contains it; anchor it.
func TestReplyRegexIsASubstringSearch(t *testing.T) {
	loose := &driver.Reply{Regex: `INPUT \S+`}
	anchored := &driver.Reply{Regex: `^INPUT \S+$`}
	if !loose.Accepts("EVT INPUT hdmi2") {
		t.Fatal("an unanchored regex did not match a line containing it: the guide's warning would be wrong")
	}
	if anchored.Accepts("EVT INPUT hdmi2") || !anchored.Accepts("INPUT hdmi2") {
		t.Fatal("an anchored regex disagrees")
	}
}
