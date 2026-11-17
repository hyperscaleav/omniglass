// Package vocab is the product vocabulary (#2029): every word an operator or
// an admin reads, defined once in terms.yaml. The docs glossary and each mapped
// OpenAPI entity schema's description are generated from it, and the guards
// that keep the API, the console and the docs speaking it read it too.
package vocab

import (
	_ "embed"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gopkg.in/yaml.v3"
)

//go:embed terms.yaml
var termsYAML []byte

// Audience is who meets a term first.
type Audience string

const (
	Operator Audience = "operator"
	Admin    Audience = "admin"
)

// Retired is a word a term replaced. Pattern is the regexp the docs and
// console denylist matches it with; Replacement, when set, is the denylist's
// hint in place of the term's word.
type Retired struct {
	Word        string `yaml:"word"`
	Pattern     string `yaml:"pattern"`
	Origin      string `yaml:"origin"`
	Replacement string `yaml:"replacement"`
}

// Term is one row of the vocabulary.
type Term struct {
	Word       string    `yaml:"word"`
	Plural     string    `yaml:"plural"`
	Definition string    `yaml:"definition"`
	Audience   Audience  `yaml:"audience"`
	Collection string    `yaml:"collection"`
	Schema     string    `yaml:"schema"`
	Permission string    `yaml:"permission"`
	Nav        string    `yaml:"nav"`
	NavLabel   string    `yaml:"nav_label"`
	Verdict    string    `yaml:"verdict"`
	Retired    []Retired `yaml:"retired"`
}

// Label is the console nav label for the term: nav_label when set, else the
// plural with its first letter capitalized.
func (t Term) Label() string {
	if t.NavLabel != "" {
		return t.NavLabel
	}
	return strings.ToUpper(t.Plural[:1]) + t.Plural[1:]
}

// NotTerm is an API collection or a console nav entry that names no product
// noun, with why.
type NotTerm struct {
	Collection string `yaml:"collection"`
	Nav        string `yaml:"nav"`
	Reason     string `yaml:"reason"`
}

// Vocabulary is the parsed table.
type Vocabulary struct {
	Terms    []Term    `yaml:"terms"`
	NotTerms []NotTerm `yaml:"not_terms"`
}

// Load parses and validates the embedded table.
func Load() (Vocabulary, error) {
	return parse(termsYAML)
}

// MustLoad is Load for callers with no error path (code generation, init).
func MustLoad() Vocabulary {
	v, err := Load()
	if err != nil {
		panic(err)
	}
	return v
}

func parse(b []byte) (Vocabulary, error) {
	var v Vocabulary
	dec := yaml.NewDecoder(strings.NewReader(string(b)))
	dec.KnownFields(true)
	if err := dec.Decode(&v); err != nil {
		return v, fmt.Errorf("vocab: parse: %w", err)
	}
	return v, v.validate()
}

func (v Vocabulary) validate() error {
	seen := map[string]string{}
	claim := func(kind, key, word string) error {
		if key == "" {
			return nil
		}
		k := kind + " " + key
		if prev, ok := seen[k]; ok {
			return fmt.Errorf("vocab: %s claimed by both %q and %q", k, prev, word)
		}
		seen[k] = word
		return nil
	}
	for _, t := range v.Terms {
		switch {
		case t.Word == "" || t.Plural == "" || t.Definition == "":
			return fmt.Errorf("vocab: term %q needs word, plural and definition", t.Word)
		case t.Audience != Operator && t.Audience != Admin:
			return fmt.Errorf("vocab: term %q audience %q is not operator or admin", t.Word, t.Audience)
		case !strings.HasSuffix(t.Definition, "."):
			return fmt.Errorf("vocab: term %q definition is not one sentence ending in a period", t.Word)
		case t.NavLabel != "" && t.Nav == "":
			return fmt.Errorf("vocab: term %q has a nav_label but no nav", t.Word)
		}
		for _, c := range []struct{ kind, key string }{
			{"word", t.Word}, {"collection", t.Collection}, {"schema", t.Schema},
			{"nav", t.Nav}, {"verdict", t.Verdict},
		} {
			if err := claim(c.kind, c.key, t.Word); err != nil {
				return err
			}
		}
		for _, r := range t.Retired {
			if r.Word == "" || r.Pattern == "" || r.Origin == "" {
				return fmt.Errorf("vocab: term %q has a retired word missing word, pattern or origin", t.Word)
			}
			if _, err := regexp.Compile(r.Pattern); err != nil {
				return fmt.Errorf("vocab: term %q retired %q: %w", t.Word, r.Word, err)
			}
		}
	}
	for _, n := range v.NotTerms {
		if (n.Collection == "") == (n.Nav == "") || n.Reason == "" {
			return fmt.Errorf("vocab: not_term %+v needs exactly one of collection or nav, and a reason", n)
		}
		if err := claim("collection", n.Collection, "not_term"); err != nil {
			return err
		}
		if err := claim("nav", n.Nav, "not_term"); err != nil {
			return err
		}
	}
	return nil
}

// Collections maps each API collection to the term that names it, or to nil
// for a not_term.
func (v Vocabulary) Collections() map[string]*Term {
	out := map[string]*Term{}
	for i := range v.Terms {
		if c := v.Terms[i].Collection; c != "" {
			out[c] = &v.Terms[i]
		}
	}
	for _, n := range v.NotTerms {
		if n.Collection != "" {
			out[n.Collection] = nil
		}
	}
	return out
}

// Nav maps each console nav path to the term it lists, or to nil for a
// not_term.
func (v Vocabulary) Nav() map[string]*Term {
	out := map[string]*Term{}
	for i := range v.Terms {
		if p := v.Terms[i].Nav; p != "" {
			out[p] = &v.Terms[i]
		}
	}
	for _, n := range v.NotTerms {
		if n.Nav != "" {
			out[n.Nav] = nil
		}
	}
	return out
}

// Verdicts maps each health verdict value to its term.
func (v Vocabulary) Verdicts() map[string]Term {
	out := map[string]Term{}
	for _, t := range v.Terms {
		if t.Verdict != "" {
			out[t.Verdict] = t
		}
	}
	return out
}

// SchemaDescriptions maps each mapped OpenAPI entity schema to its term's
// definition.
func (v Vocabulary) SchemaDescriptions() map[string]string {
	out := map[string]string{}
	for _, t := range v.Terms {
		if t.Schema != "" {
			out[t.Schema] = t.Definition
		}
	}
	return out
}

// BannedWord is a retired word with the term that replaced it, the shape the
// docs and console denylist consumes.
type BannedWord struct {
	Pattern     *regexp.Regexp
	Replacement string
	Origin      string
}

// Banned lists every retired word, in table order.
func (v Vocabulary) Banned() []BannedWord {
	var out []BannedWord
	for _, t := range v.Terms {
		for _, r := range t.Retired {
			hint := r.Replacement
			if hint == "" {
				hint = t.Word
			}
			out = append(out, BannedWord{
				Pattern:     regexp.MustCompile(r.Pattern),
				Replacement: hint,
				Origin:      r.Origin,
			})
		}
	}
	return out
}

// GlossaryPath is where make gen writes the rendered glossary.
const GlossaryPath = "docs/src/content/docs/reference/glossary.md"

// RenderGlossary renders the docs glossary page: operator terms, then admin
// terms, each alphabetical.
func (v Vocabulary) RenderGlossary() string {
	var b strings.Builder
	b.WriteString(`---
title: Glossary
description: The words Omniglass uses, each with one meaning, generated from the vocabulary table.
---

<!-- Generated by cmd/vocabgen from internal/vocab/terms.yaml. Do not edit by hand; run make gen. -->

Every word the console, the API and these docs use for a thing you manage means one thing, defined
here. **API** is the collection the thing lives under; **Permission** is the noun its permissions
carry (` + "`component:read`" + `); **Formerly** is a word it replaced, which no longer appears.

`)
	for _, sec := range []struct {
		aud   Audience
		title string
	}{{Operator, "Operator terms"}, {Admin, "Admin terms"}} {
		var ts []Term
		for _, t := range v.Terms {
			if t.Audience == sec.aud {
				ts = append(ts, t)
			}
		}
		sort.Slice(ts, func(i, j int) bool { return ts[i].Word < ts[j].Word })
		fmt.Fprintf(&b, "## %s\n\n| Term | Meaning | API | Permission | Formerly |\n| --- | --- | --- | --- | --- |\n", sec.title)
		for _, t := range ts {
			api, perm := "", ""
			if t.Collection != "" {
				api = "`/" + t.Collection + "`"
			}
			if t.Permission != "" {
				perm = "`" + t.Permission + "`"
			}
			var old []string
			for _, r := range t.Retired {
				old = append(old, r.Word)
			}
			fmt.Fprintf(&b, "| **%s** | %s | %s | %s | %s |\n", t.Word, t.Definition, api, perm, strings.Join(old, ", "))
		}
		b.WriteString("\n")
	}
	return b.String()
}
