package storage

import (
	"encoding/json"
	"fmt"

	"github.com/hyperscaleav/omniglass/internal/label"
)

// The label data map, declared once (#729).
//
// The keys a rule may read used to be built inline, key by key, in three map
// literals, and typed out a second time in the prose that teaches the language,
// a third time in two API field descriptions, and a fourth in the CLI reference
// those descriptions render into. This arc changed the set three times (#682
// built it, #685 added the placement facts, #693 changed what Ordinal reports)
// and each of those copies had to be found and edited by hand. It is the same
// defect class #701 closed one paragraph away, on the function set.
//
// So each kind's map is ONE ordered declaration carrying the key, the summary
// the docs render, and the accessor that produces the value. [labelData] builds
// the map from it and [LabelDataFactsJSON] renders the artifact from it, which
// makes the taught set and the reachable set incapable of disagreeing rather
// than merely expected to agree.
//
// This declaration IS the sandbox (see internal/label). Adding a key here is the
// only way to widen what a rule can see, so a key that is a secret, a credential,
// or a handle to another entity is not filtered out downstream, it is simply
// never added, and the value type stays a string so nothing here can be
// traversed through.

// labelKey is one entry in one kind's data map. The summary is written for an
// operator authoring a rule, so it says what the value IS rather than which
// column it came from.
type labelKey[T any] struct {
	Name    string
	Summary string
	value   func(T) string
}

// labelData builds the closed map from a kind's declaration. Every key is always
// present, which is why the value type is a string: an absent fact is the empty
// string, so a rule never renders "<no value>" and {{if}} works on anything.
func labelData[T any](keys []labelKey[T], f T) label.Data {
	d := make(label.Data, len(keys))
	for _, k := range keys {
		d[k.Name] = k.value(f)
	}
	return d
}

// componentFacts is the three halves of a component's inputs (its own row, its
// classification, its placement) in one value, so the accessors below take one
// argument. It is the only place the three meet, so the single-row stamp and the
// bulk recompute cannot drift on what a rule sees: they resolve the halves
// differently, one row at a time versus a page at a time, and then call this.
type componentFacts struct {
	c  *Component
	in componentLabelInputs
	pl componentPlacement
}

type systemFacts struct {
	s  *System
	in systemLabelInputs
	pl systemPlacement
}

type locationFacts struct {
	l  *Location
	in locationLabelInputs
}

var componentLabelKeys = []labelKey[componentFacts]{
	{
		Name:    "Name",
		Summary: "The component's own name, whether an operator typed it or the platform minted it.",
		value:   func(f componentFacts) string { return f.c.Name },
	},
	{
		Name: "Ordinal",
		Summary: "The number the component's NAME carries, and empty when it carries none: an operator " +
			"named the row, or the mint suppressed the first of its stem in the bucket (ADR-0101). " +
			"Empty is falsy, so {{if .Ordinal}} is the whole of the suppression rule a rule author writes.",
		value: func(f componentFacts) string {
			return mintedOrdinalText(componentMint(f.in.stem), f.c.Ordinal)
		},
	},
	{
		Name: "TypeName",
		Summary: "The display name of the component_type the product classifies it as, resolved over an " +
			"operator's fork of a shipped row.",
		value: func(f componentFacts) string { return f.in.typeName },
	},
	{
		Name:    "TypeAbbrev",
		Summary: "The abbreviation inherited down the component_type chain, first non-null wins.",
		value:   func(f componentFacts) string { return f.in.abbrev },
	},
	{
		Name:    "Stem",
		Summary: "The naming stem inherited down the component_type chain, the token generated names are built from.",
		value:   func(f componentFacts) string { return f.in.stem },
	},
	{
		Name:    "ProductName",
		Summary: "The product's display name.",
		value:   func(f componentFacts) string { return f.in.productName },
	},
	{
		Name:    "VendorName",
		Summary: "The vendor's display name, empty when the product names no vendor.",
		value:   func(f componentFacts) string { return f.in.vendorName },
	},
	{
		Name: "LocationLabel",
		Summary: "How the location this component sits AT reads: the label an operator typed or the " +
			"platform rendered, else that location's own name. It is the row's own location, never an " +
			"ancestor's and never its plane root's, so a nested component with no location of its own " +
			"reads it as absent.",
		value: func(f componentFacts) string { return f.pl.locationLabel },
	},
	{
		Name: "SystemTypeLabel",
		Summary: "The display name of the type of the system this component is a PRIMARY member of, " +
			"empty when it is a member of none.",
		value: func(f componentFacts) string { return f.pl.systemTypeLabel },
	},
}

var systemLabelKeys = []labelKey[systemFacts]{
	{
		Name:    "Name",
		Summary: "The system's own name, whether an operator typed it or the platform minted it.",
		value:   func(f systemFacts) string { return f.s.Name },
	},
	{
		Name: "Ordinal",
		Summary: "The number the system's NAME carries, and empty when it carries none, so the two halves " +
			"of a divisible boardroom read as the name shows them (ADR-0101).",
		value: func(f systemFacts) string {
			return mintedOrdinalText(systemMint(f.in.stem), f.s.Ordinal)
		},
	},
	{
		Name:    "TypeName",
		Summary: "The display name of the system_type it is classified as.",
		value:   func(f systemFacts) string { return f.in.typeName },
	},
	{
		Name:    "TypeAbbrev",
		Summary: "The abbreviation inherited down the system_type chain, first non-null wins.",
		value:   func(f systemFacts) string { return f.in.abbrev },
	},
	{
		Name:    "Stem",
		Summary: "The naming stem inherited down the system_type chain.",
		value:   func(f systemFacts) string { return f.in.stem },
	},
	{
		Name:    "StandardName",
		Summary: "The display name of the standard it conforms to, empty when it conforms to none.",
		value:   func(f systemFacts) string { return f.in.standardName },
	},
	{
		Name: "LocationLabel",
		Summary: "How the location this system sits at reads: the label an operator typed or the platform " +
			"rendered, else that location's own name.",
		value: func(f systemFacts) string { return f.pl.locationLabel },
	},
}

// locationLabelKeys is two keys, and the absences are the design. No product and
// no vendor, because a location is not an instance of a catalog row. No
// placement, because a location's placement is its parent and a rule that read it
// would stale every descendant on a campus rename. And deliberately no Ordinal,
// although #687 gave a location one: the key would be REDUNDANT, since a
// positional location's name IS the number and a stemmed one carries it in the
// name too, so "{{title (words .Name)}}" already reads "Wing 2" off `wing-2`.
var locationLabelKeys = []labelKey[locationFacts]{
	{
		Name:    "Name",
		Summary: "The location's own name, which an operator types unless its type carries a name rule.",
		value:   func(f locationFacts) string { return f.l.Name },
	},
	{
		Name:    "TypeName",
		Summary: "The display name of the location_type it is classified as, resolved over an operator's fork.",
		value:   func(f locationFacts) string { return f.in.typeName },
	},
}

// LabelDataKinds are the entity kinds that carry a data map, in the order the
// docs render them. It IS [LabelRuleKinds] rather than a second list of the same
// three strings: a kind carries a rule exactly when it has facts for one to read,
// and this file exists because a restated fact drifts. A kind added there without
// a declaration here fails TestTheBuiltMapIsExactlyTheDeclaredKeys rather than
// rendering an empty table.
var LabelDataKinds = LabelRuleKinds

// LabelDataKeys returns the keys a rule for this entity kind may read, in the
// documented order, or nil for a kind that carries no data map.
//
// Exported because the taught set has readers outside this package: the API's
// own `label_rule` field descriptions name the keys, and those descriptions
// render into the OpenAPI, the CLI reference and the console's field help. A
// test holds them to this list rather than to a copy somebody typed.
func LabelDataKeys(kind string) []string {
	switch kind {
	case "component":
		return keyNames(componentLabelKeys)
	case "system":
		return keyNames(systemLabelKeys)
	case "location":
		return keyNames(locationLabelKeys)
	}
	return nil
}

func keyNames[T any](keys []labelKey[T]) []string {
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.Name
	}
	return out
}

// --- the generated artifact --------------------------------------------------

type labelKeyDoc struct {
	Name    string `json:"name"`
	Summary string `json:"summary"`
}

type labelKindDoc struct {
	Kind string        `json:"kind"`
	Keys []labelKeyDoc `json:"keys"`
}

type labelDataDoc struct {
	Comment string         `json:"//"`
	Kinds   []labelKindDoc `json:"kinds"`
}

// LabelDataFactsJSON renders the data map into docs/src/generated/labeldata.json,
// the file the docs render the per-kind key tables from rather than typing them
// out (#729, cmd/labelgen). Deterministic: the kinds and their keys keep their
// declared order, and the trailing newline makes the committed file POSIX-clean.
// Same drift discipline as the seed, identity and rule-language artifacts, a
// committed file plus a test that re-renders it.
func LabelDataFactsJSON() ([]byte, error) {
	doc := labelDataDoc{Comment: "generated by make gen (cmd/labelgen); do not edit"}
	for _, kind := range LabelDataKinds {
		k := labelKindDoc{Kind: kind}
		switch kind {
		case "component":
			k.Keys = keyDocs(componentLabelKeys)
		case "system":
			k.Keys = keyDocs(systemLabelKeys)
		case "location":
			k.Keys = keyDocs(locationLabelKeys)
		}
		doc.Kinds = append(doc.Kinds, k)
	}
	out, err := json.MarshalIndent(doc, "", " ")
	if err != nil {
		return nil, fmt.Errorf("label data facts: marshal: %w", err)
	}
	return append(out, '\n'), nil
}

func keyDocs[T any](keys []labelKey[T]) []labelKeyDoc {
	out := make([]labelKeyDoc, len(keys))
	for i, k := range keys {
		out[i] = labelKeyDoc{Name: k.Name, Summary: k.Summary}
	}
	return out
}
