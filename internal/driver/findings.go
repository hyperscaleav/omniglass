package driver

import (
	"strconv"
	"strings"
)

// Finding is one fault in a driver spec, as the validate route answers it
// (#1163): what kind of thing is wrong (the spec does not parse, the spec
// itself, or one function), the function it is pinned on, and the
// validator's own message, which names the fault.
type Finding struct {
	Kind     string `json:"kind" enum:"parse,spec,poll,listener,command" doc:"What the fault is in: parse (the spec is not a spec), spec (the spec itself), or the family of the function it is pinned on"`
	Function string `json:"function,omitempty" doc:"The function the fault is pinned on: a poll's or a listener's name, a command binding's command type"`
	Message  string `json:"message" doc:"The fault, as the validator names it"`
}

// The kinds of a finding.
const (
	FindingParse    = "parse"
	FindingSpec     = "spec"
	FindingPoll     = "poll"
	FindingListener = "listener"
	FindingCommand  = "command"
)

// Findings validates a spec the way a save does (Parse, then Validate against
// the catalog) and reports every fault it can pin down, not only the first:
// the validator stops at a spec's first fault, so each function is validated
// again on its own, the spec's other fields intact, and a function whose
// isolated fault names it is a finding of its own. The first finding is always
// the fault a save would name; a fault no single function owns (a duplicate
// name, a bad transport) is reported once. A valid spec has no findings.
func Findings(raw []byte, cat Catalog) []Finding {
	sp, err := Parse(raw)
	if err != nil {
		return []Finding{{Kind: FindingParse, Message: err.Error()}}
	}
	whole := sp.Validate(cat)
	if whole == nil {
		return nil
	}
	var perFn []Finding
	isolated := func(kind, name string, only Spec) bool {
		err := only.Validate(cat)
		if err != nil && strings.Contains(err.Error(), strconv.Quote(name)) {
			perFn = append(perFn, Finding{Kind: kind, Function: name, Message: err.Error()})
		}
		return err == nil
	}
	// The polls and listeners that validate on their own hold a session for a
	// command binding's isolation, so a binding on a transport whose commands
	// ride a held session is checked as itself, not refused for want of one.
	var cleanPolls []Poll
	var cleanListeners []Listener
	for _, p := range sp.Polls {
		only := *sp
		only.Polls, only.Listeners, only.Commands = []Poll{p}, nil, nil
		if isolated(FindingPoll, p.Name, only) {
			cleanPolls = append(cleanPolls, p)
		}
	}
	for _, l := range sp.Listeners {
		only := *sp
		only.Polls, only.Listeners, only.Commands = nil, []Listener{l}, nil
		if isolated(FindingListener, l.Name, only) {
			cleanListeners = append(cleanListeners, l)
		}
	}
	for _, c := range sp.Commands {
		only := *sp
		only.Polls, only.Listeners, only.Commands = cleanPolls, cleanListeners, []CommandBinding{c}
		isolated(FindingCommand, c.CommandType, only)
	}

	first := Finding{Kind: FindingSpec, Message: whole.Error()}
	for _, f := range perFn {
		if f.Message == first.Message {
			first = f
			break
		}
	}
	out := []Finding{first}
	seen := map[string]bool{first.Message: true}
	for _, f := range perFn {
		if !seen[f.Message] {
			seen[f.Message] = true
			out = append(out, f)
		}
	}
	return out
}
