package devseed

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/hyperscaleav/omniglass/internal/seed"
)

// MethodGap is a collection method no healthy lab component exercises yet,
// and the issue that adds it (#1370). An unbuilt one is not in the binary at
// all (no driver, no registry transport) and is listed so the gap is said
// out loud.
type MethodGap struct {
	Issue   string
	Unbuilt bool
	// Why is the reason, one line, rendered beside the gap in the docs
	// (MethodFactsJSON).
	Why string
}

// MethodGaps are the collection methods the dev lab does not exercise, keyed
// by driver or transport name. The coverage test fails on a method neither
// exercised nor listed here, and on a listed one the lab exercises now; the
// lab's own check skips them.
var MethodGaps = map[string]MethodGap{
	"websocket":         {Issue: "#1414", Why: "no simulated device speaks a plain ws face, and no driver rides it"},
	"kestrel-relay-api": {Issue: "#1446", Why: "no Relay C1 is placed in the dev fleet yet"},
	"push-listener":     {Issue: "#1148", Unbuilt: true, Why: "the shared push listener is not built"},
	"syslog":            {Issue: "#1149", Unbuilt: true, Why: "the syslog transport is not built"},
	"snmp-trap":         {Issue: "#1150", Unbuilt: true, Why: "the SNMP trap transport is not built"},
}

// MethodFacts is what the docs render of the collection-method coverage
// (docs/src/generated/methods.json, via cmd/labgen on make gen, #1419): the
// healthy and planted endpoints the dev fleet attaches beyond the plan's own,
// and the methods it does not exercise. Read from the fixture and MethodGaps
// the coverage test holds, so a moved endpoint or a closed gap changes the
// page and a stale one fails the drift test.
type MethodFacts struct {
	Methods []MethodRow `json:"methods"`
	Gaps    []GapRow    `json:"gaps"`
}

// MethodRow is one sim_endpoints entry: a driver attached, or a transport
// probed bare, on a fixture component.
type MethodRow struct {
	Method string `json:"method"`
	// Kind is "driver" or "transport".
	Kind string `json:"kind"`
	// Transport is the wire a driver rides; empty on a transport row.
	Transport string `json:"transport,omitempty"`
	Component string `json:"component"`
	// Place is the label of the location the component sits in.
	Place string `json:"place"`
	Label string `json:"label"`
	// Planted marks an endpoint broken on purpose: it does not count as the
	// method running healthy.
	Planted bool `json:"planted,omitempty"`
}

// GapRow is one MethodGaps entry.
type GapRow struct {
	Method  string `json:"method"`
	Issue   string `json:"issue"`
	Unbuilt bool   `json:"unbuilt,omitempty"`
	Why     string `json:"why"`
}

// BuildMethodFacts derives the facts from the embedded fixture, the shipped
// driver specs and MethodGaps.
func BuildMethodFacts() (MethodFacts, error) {
	doc, err := Fixtures()
	if err != nil {
		return MethodFacts{}, err
	}
	places := map[string]string{}
	for _, l := range doc.Locations {
		places[l.Key] = firstNonEmpty(l.Label, l.Name, l.Key)
	}
	siteOf := map[string]string{}
	for _, c := range append(append([]Component{}, doc.Components...), doc.SimComponents...) {
		siteOf[c.Key] = places[c.Location]
	}
	facts := MethodFacts{Methods: []MethodRow{}, Gaps: []GapRow{}}
	for _, se := range doc.SimEndpoints {
		place, ok := siteOf[se.Component]
		if !ok || place == "" {
			return MethodFacts{}, fmt.Errorf("devseed: sim endpoint %q is on component %q, which has no place in the fixture", se.Label, se.Component)
		}
		row := MethodRow{Component: se.Component, Place: place, Label: se.Label, Planted: se.PlantedFault != ""}
		if se.Driver != "" {
			spec, err := seed.ShippedDriverSpec(se.Driver)
			if err != nil {
				return MethodFacts{}, fmt.Errorf("devseed: sim endpoint %q: %w", se.Label, err)
			}
			row.Method, row.Kind, row.Transport = se.Driver, "driver", spec.Transport
		} else {
			row.Method, row.Kind = se.Transport, "transport"
		}
		facts.Methods = append(facts.Methods, row)
	}
	for name, g := range MethodGaps {
		facts.Gaps = append(facts.Gaps, GapRow{Method: name, Issue: g.Issue, Unbuilt: g.Unbuilt, Why: g.Why})
	}
	sort.Slice(facts.Gaps, func(i, j int) bool { return facts.Gaps[i].Method < facts.Gaps[j].Method })
	return facts, nil
}

// MethodFactsJSON renders BuildMethodFacts as the committed JSON the docs read.
func MethodFactsJSON() ([]byte, error) {
	facts, err := BuildMethodFacts()
	if err != nil {
		return nil, err
	}
	out, err := json.MarshalIndent(facts, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("devseed: method facts: %w", err)
	}
	return append(out, '\n'), nil
}

func firstNonEmpty(ss ...string) string {
	for _, s := range ss {
		if s != "" {
			return s
		}
	}
	return ""
}
