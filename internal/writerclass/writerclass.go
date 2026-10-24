// Package writerclass declares who may write each telemetry key (ADR-0161).
// Every metric, property and event type has exactly one writer class, and each
// class admits a fixed set of writers: the entry points a write comes through.
// The Storage Gateway checks every sample and event write against it, the way it
// injects scope on every query, so no route can forget to.
//
// Two classes form the reserved system family, fixed and internal: node-probe
// (the keys a node's own probes and session machine write, declared in
// probekeys) and platform (keys only the Omniglass process itself writes: the
// health rollup's verdict today, its self-monitoring later). Their names are
// declared in code and reserved against operator-created types. The other two
// are resolved from the catalogs: calc (a calc rule names the key as its output)
// and collected (every other key: a driver's emits on the node lane, a push, the
// observers fusion reconciles by source).
//
// A leaf: storage, the push route, the node lane and the driver spec all read
// this one ruling.
package writerclass

import (
	"encoding/json"
	"maps"
	"slices"
	"strings"

	"github.com/hyperscaleav/omniglass/internal/collection/probekeys"
)

// Class is a key's writer class.
type Class string

// The writer classes. NodeProbe and Platform are the reserved system family.
const (
	NodeProbe Class = "node-probe"
	Platform  Class = "platform"
	Calc      Class = "calc"
	Collected Class = "collected"
)

// Classes is every writer class, the reserved family first.
func Classes() []Class { return []Class{NodeProbe, Platform, Calc, Collected} }

// Reserved reports a class of the reserved system family: fixed, internal, its
// keys declared in code and their names closed to operator-created types.
func (c Class) Reserved() bool { return c == NodeProbe || c == Platform }

// Writer is the entry point a write comes through, which the gateway matches
// against the key's class. Each writer fixes the provenance it writes.
type Writer string

// The writers.
const (
	// Node is the node lane: a node's own collection (its probes, its session
	// machine, the drivers it runs), observed, bound to the endpoint its task
	// ran against.
	Node Writer = "node"
	// Push is the telemetry push lane, observed.
	Push Writer = "push"
	// CalcRule is a calc rule's tick writing its own output, calculated.
	CalcRule Writer = "calc-rule"
	// Process is the Omniglass process itself (the server, later a node of
	// itself): the health rollup today, calculated.
	Process Writer = "process"
	// Command is a command issue: its intended value and its caused event.
	Command Writer = "command"
	// CommandReply is a command's reply read through its driver binding, an
	// observation of the command's target.
	CommandReply Writer = "command-reply"
	// Operator is an operator's declared value.
	Operator Writer = "operator"
	// HostKeyPin is an operator's ssh host key pin, declared through an
	// endpoint's audited :retry.
	HostKeyPin Writer = "host-key-pin"
	// EventRule is an event rule's fire or clear event.
	EventRule Writer = "event-rule"
)

// Writers is every writer.
func Writers() []Writer {
	return []Writer{Node, Push, CalcRule, Process, Command, CommandReply, Operator, HostKeyPin, EventRule}
}

// Health is the platform key the health rollup writes: the rolled-up verdict
// of a component, system or location.
const Health = "health"

// platform maps each platform key to the part of the process that writes it.
// A key only the Omniglass process writes about itself joins here with its
// writer (the self-monitoring follow-on, #1094, is its planned consumer).
var platform = map[string]string{
	Health: "the health rollup",
}

// pinnable are the node-probe keys an operator may also declare, each with its
// path: a declared row is never the node's observation (the reads serve only
// observed rows), and this one is a control input the worklist hands back.
var pinnable = map[string]string{
	probekeys.SSHHostKey: "an operator's pin, set with an endpoint's :retry (ADR-0157)",
}

// admits is the whole ruling: the writers each class admits.
//
//   - node-probe: the node lane alone (and HostKeyPin on a pinnable key): a
//     push, a calc rule, a command, an operator or an event rule never writes
//     what the console reads as the node's own observation.
//   - platform: the process alone.
//   - calc: the rule that outputs it, and an operator's declared value, which
//     resolves ahead of the calculated one by design (properties page).
//   - collected: every observer (the node lane carrying a driver's emits, a
//     push, a command's reply), a command's intended value, an operator's
//     declared value, an event rule's event. A calc rule never: its output is a
//     key it owns, so a collected key is never one.
var admits = map[Class][]Writer{
	NodeProbe: {Node},
	Platform:  {Process},
	Calc:      {CalcRule, Operator},
	Collected: {Node, Push, Command, CommandReply, Operator, EventRule},
}

// Admits reports whether w may write key, whose class is c.
func Admits(c Class, w Writer, key string) bool {
	if slices.Contains(admits[c], w) {
		return true
	}
	return c == NodeProbe && w == HostKeyPin && pinnable[key] != ""
}

// Reserved returns the reserved class a key's name holds, if any. It matches
// case-insensitively and ignores surrounding space, failing closed the way the
// probekeys ruling does: a variant is refused as reserved should a lookup that
// would refuse it as unregistered ever loosen.
func Reserved(name string) (Class, bool) {
	if probekeys.IsNodeOnly(name) {
		return NodeProbe, true
	}
	if _, ok := platformKey(name); ok {
		return Platform, true
	}
	return "", false
}

// platformKey resolves name to its platform key, case-insensitively.
func platformKey(name string) (string, bool) {
	name = strings.TrimSpace(name)
	for k := range platform {
		if strings.EqualFold(k, name) {
			return k, true
		}
	}
	return "", false
}

// ReservedNames is every reserved key, both classes, sorted, a fresh slice.
func ReservedNames() []string {
	out := probekeys.NodeOnly()
	for k := range platform {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Of resolves a key's class: its reserved class by name, else calc when a calc
// rule outputs it (calcOwned, read from the catalog by the caller), else
// collected.
func Of(name string, calcOwned bool) Class {
	if c, ok := Reserved(name); ok {
		return c
	}
	if calcOwned {
		return Calc
	}
	return Collected
}

// Refusal is the reason a write of key by w is refused, naming the key and the
// writer that owns it. rule names the calc rule a calc key belongs to.
func Refusal(c Class, key string, w Writer, rule string) string {
	switch c {
	case NodeProbe:
		if w == Node {
			return key + " is a native probe key: a node writes it only under the endpoint whose task it ran"
		}
		return key + " is a native probe key, a node's own observation: only the node writes it"
	case Platform:
		by := "the Omniglass process"
		if k, ok := platformKey(key); ok {
			by = platform[k]
		}
		return key + " is a platform key: only " + by + " writes it"
	case Calc:
		if rule == "" {
			return key + " is written by a calc rule: only that rule writes it"
		}
		return key + " is written by the calc rule " + rule + ": only that rule writes it"
	default:
		return key + " is a collected key: a " + string(w) + " does not write it"
	}
}

// classDoc is each class in one line, for the docs render.
var classDoc = map[Class]string{
	NodeProbe: "what a node's own probes and session machine write: the node's account of an endpoint",
	Platform:  "what only the Omniglass process writes about itself or its rollups",
	Calc:      "a key a calc rule names as its output",
	Collected: "every other key: what a driver's emits, a push, a command or an operator say",
}

// writerDoc is each writer in one line, for the docs render.
var writerDoc = map[Writer]string{
	Node:         "the node lane, under the endpoint whose task the node ran",
	Push:         "the telemetry push lane",
	CalcRule:     "the calc rule's tick",
	Process:      "the Omniglass process (the health rollup)",
	Command:      "a command's intended value and caused event",
	CommandReply: "a command's reply, read through its driver binding",
	Operator:     "an operator's declared value",
	HostKeyPin:   "an operator's ssh host key pin, through an endpoint's :retry",
	EventRule:    "an event rule's fire or clear event",
}

// ClassFact is one class as data: what it holds, and who may write it.
type ClassFact struct {
	Class    Class        `json:"class"`
	Reserved bool         `json:"reserved"`
	Doc      string       `json:"doc"`
	Keys     []string     `json:"keys,omitempty"`
	Admits   []WriterFact `json:"admits"`
	Pins     []PinFact    `json:"pins,omitempty"`
}

// PinFact is the one declared row a node-probe key admits: the key, the
// writer that may pin it, and its path.
type PinFact struct {
	Key    string `json:"key"`
	Writer Writer `json:"writer"`
	Doc    string `json:"doc"`
}

// WriterFact is one writer as data.
type WriterFact struct {
	Writer Writer `json:"writer"`
	Doc    string `json:"doc"`
}

// Facts is the whole ruling as data, in class order: the docs render it
// (cmd/writerclassgen writes docs/src/generated/writerclass.json) so the page
// that teaches the ruling cannot drift from the one the gateway enforces.
func Facts() []ClassFact {
	out := make([]ClassFact, 0, len(Classes()))
	for _, c := range Classes() {
		f := ClassFact{Class: c, Reserved: c.Reserved(), Doc: classDoc[c]}
		switch c {
		case NodeProbe:
			f.Keys = probekeys.NodeOnly()
			for _, k := range slices.Sorted(maps.Keys(pinnable)) {
				f.Pins = append(f.Pins, PinFact{Key: k, Writer: HostKeyPin, Doc: pinnable[k]})
			}
		case Platform:
			for k := range platform {
				f.Keys = append(f.Keys, k)
			}
			slices.Sort(f.Keys)
		}
		for _, w := range admits[c] {
			f.Admits = append(f.Admits, WriterFact{Writer: w, Doc: writerDoc[w]})
		}
		out = append(out, f)
	}
	return out
}

// FactsJSON is Facts as the indented JSON the docs read, newline-terminated.
func FactsJSON() ([]byte, error) {
	b, err := json.MarshalIndent(Facts(), "", "  ")
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}
