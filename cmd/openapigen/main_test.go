package main

import "testing"

// TestCanonicalJSON pins the committed spec's layout (#1095): pretty-printed,
// one key per line in sorted order, so two API changes touch different lines
// and git merges them, and a conflict that does happen is resolved by make gen.
func TestCanonicalJSON(t *testing.T) {
	in := []byte(`{"paths":{"/b":{"post":{},"get":{"summary":"<b> & c"}},"/a":{}},"openapi":"3.1.0","info":{"version":"1","title":"t"},"n":[3,1.50,1e3,12345678901234567890],"e":[],"o":{}}`)
	want := `{
  "e": [],
  "info": {
    "title": "t",
    "version": "1"
  },
  "n": [
    3,
    1.50,
    1e3,
    12345678901234567890
  ],
  "o": {},
  "openapi": "3.1.0",
  "paths": {
    "/a": {},
    "/b": {
      "get": {
        "summary": "<b> & c"
      },
      "post": {}
    }
  }
}
`
	got, err := canonicalJSON(in)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != want {
		t.Errorf("canonicalJSON:\n%s\nwant:\n%s", got, want)
	}
	again, err := canonicalJSON(got)
	if err != nil || string(again) != want {
		t.Errorf("canonicalJSON is not idempotent:\n%s (%v)", again, err)
	}
	if _, err := canonicalJSON([]byte(`{"a":1}{"b":2}`)); err == nil {
		t.Error("trailing data accepted")
	}
}
