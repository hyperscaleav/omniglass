package main

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/template"
)

// render runs the generator into a temp dir and returns the bytes it wrote.
func render(t *testing.T) []byte {
	t.Helper()
	path := filepath.Join(t.TempDir(), "sub", "seed.json")
	if err := run(path); err != nil {
		t.Fatalf("run: %v", err)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

// TestSeedgenIsDeterministic: running the generator again over an unchanged
// tree yields the same bytes. Go randomizes map iteration per run, so several
// renders are what would catch an emitter that leaks that order.
func TestSeedgenIsDeterministic(t *testing.T) {
	first := render(t)
	for i := 0; i < 5; i++ {
		if got := render(t); !bytes.Equal(first, got) {
			t.Fatalf("render %d differs from the first: the emitter is not deterministic", i+2)
		}
	}
}

// TestSeedgenMatchesCommitted ties the committed artifact to the generator the
// way make gen does, through the same run path.
func TestSeedgenMatchesCommitted(t *testing.T) {
	committed, err := os.ReadFile("../../" + dst)
	if err != nil {
		t.Fatalf("read committed %s: %v", dst, err)
	}
	if !bytes.Equal(committed, render(t)) {
		t.Errorf("%s drifted from the embedded seed YAMLs: run make gen and commit the result", dst)
	}
}

// TestSeedgenOneEntryPerLine: the artifact is canonically indented, so every
// object member and list element starts its own line. A packed line is one
// that two branches editing neighbouring entries would both touch.
func TestSeedgenOneEntryPerLine(t *testing.T) {
	out := render(t)
	var compact, want bytes.Buffer
	if err := json.Compact(&compact, out); err != nil {
		t.Fatalf("output is not JSON: %v", err)
	}
	if err := json.Indent(&want, compact.Bytes(), "", " "); err != nil {
		t.Fatal(err)
	}
	want.WriteByte('\n')
	if !bytes.Equal(out, want.Bytes()) {
		gotLines, wantLines := strings.Split(string(out), "\n"), strings.Split(want.String(), "\n")
		for i := 0; i < len(gotLines) && i < len(wantLines); i++ {
			if gotLines[i] != wantLines[i] {
				t.Fatalf("line %d is not one entry per line:\n got %q\nwant %q", i+1, gotLines[i], wantLines[i])
			}
		}
		t.Fatal("output differs from its canonical one-entry-per-line form")
	}
}

// node is a JSON value read with its object key order kept, which a decode
// into map[string]any would lose.
type node struct {
	keys  []string        // object keys in document order
	kids  map[string]node // object members
	elems []node          // list elements
}

func readNode(t *testing.T, dec *json.Decoder) node {
	t.Helper()
	tok, err := dec.Token()
	if err != nil {
		t.Fatal(err)
	}
	d, ok := tok.(json.Delim)
	if !ok {
		return node{}
	}
	n := node{kids: map[string]node{}}
	for dec.More() {
		if d == '{' {
			k, err := dec.Token()
			if err != nil {
				t.Fatal(err)
			}
			key := k.(string)
			n.keys = append(n.keys, key)
			n.kids[key] = readNode(t, dec)
		} else {
			n.elems = append(n.elems, readNode(t, dec))
		}
	}
	if _, err := dec.Token(); err != nil && err != io.EOF {
		t.Fatal(err)
	}
	return n
}

// checkMapsSorted walks the Go type beside the emitted node. A map's keys are
// data, so their order is the encoder's choice and must be sorted; a struct's
// keys are its declared field order, which is fixed and needs no check. A
// json.RawMessage holds a value the type cannot describe, so every object
// inside it is held to sorted keys.
func checkMapsSorted(t *testing.T, path string, typ reflect.Type, n node) {
	t.Helper()
	for typ.Kind() == reflect.Pointer {
		typ = typ.Elem()
	}
	switch {
	case typ == reflect.TypeOf(json.RawMessage{}):
		checkAllSorted(t, path, n)
	case typ.Kind() == reflect.Map:
		if !sort.StringsAreSorted(n.keys) {
			t.Errorf("%s: map keys are not sorted: %v", path, n.keys)
		}
		for _, k := range n.keys {
			checkMapsSorted(t, path+"."+k, typ.Elem(), n.kids[k])
		}
	case typ.Kind() == reflect.Slice:
		for _, e := range n.elems {
			checkMapsSorted(t, path+"[]", typ.Elem(), e)
		}
	case typ.Kind() == reflect.Struct:
		fields := map[string]reflect.Type{}
		for i := 0; i < typ.NumField(); i++ {
			name := strings.Split(typ.Field(i).Tag.Get("json"), ",")[0]
			if name != "" && name != "-" {
				fields[name] = typ.Field(i).Type
			}
		}
		for _, k := range n.keys {
			if ft, ok := fields[k]; ok {
				checkMapsSorted(t, path+"."+k, ft, n.kids[k])
			}
		}
	}
}

func checkAllSorted(t *testing.T, path string, n node) {
	t.Helper()
	if !sort.StringsAreSorted(n.keys) {
		t.Errorf("%s: raw object keys are not sorted: %v", path, n.keys)
	}
	for _, k := range n.keys {
		checkAllSorted(t, path+"."+k, n.kids[k])
	}
	for _, e := range n.elems {
		checkAllSorted(t, path+"[]", e)
	}
}

// TestSeedgenMapKeysSorted: the facts doc holds a map only inside its
// templates (the other facts types are structs and lists), so the check walks
// template.Template beside the emitted templates, and fails if any other top
// level section grows an object whose keys are not a struct's fields.
func TestSeedgenMapKeysSorted(t *testing.T) {
	root := readNode(t, json.NewDecoder(bytes.NewReader(render(t))))
	tpls, ok := root.kids["templates"]
	if !ok || len(tpls.elems) == 0 {
		t.Fatal("no templates in the seed facts: the map check has nothing to walk")
	}
	checkMapsSorted(t, "templates", reflect.TypeOf([]template.Template{}), tpls)

	// Templates are the only section walked by type, so an object nested in
	// any other section (a map would be one) is unchecked: fail until the
	// check learns its type.
	for name, section := range root.kids {
		if name == "templates" {
			continue
		}
		for _, e := range section.elems {
			for _, k := range e.keys {
				if hasNestedObject(e.kids[k]) {
					t.Errorf("section %s: member %q holds a nested object; walk its Go type in checkMapsSorted so its keys are proven sorted", name, k)
				}
			}
		}
	}
}

// hasNestedObject reports whether a member value is an object, or a list
// whose entries hold one: a list entry is itself a struct, but an object one
// level in is a map or a struct the type walk has not been taught.
func hasNestedObject(n node) bool {
	if len(n.keys) > 0 {
		return true
	}
	for _, e := range n.elems {
		for _, k := range e.keys {
			if hasNestedObject(e.kids[k]) {
				return true
			}
		}
	}
	return false
}
