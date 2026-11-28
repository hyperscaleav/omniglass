package httpapi

import (
	"reflect"
	"testing"
)

func TestPagingReadsItsOwnParameters(t *testing.T) {
	p := Paging{TokenParam: "cursor", SizeParam: "limit", Default: 10, Max: 50}
	for _, tc := range []struct {
		name  string
		query map[string]string
		size  int
		after string
	}{
		{"none", nil, 10, ""},
		{"a size and a token", map[string]string{"limit": "5", "cursor": "r-3"}, 5, "r-3"},
		{"the names of another API are not read", map[string]string{"page_size": "5", "next_page_token": "x"}, 10, ""},
		{"past the most", map[string]string{"limit": "9999"}, 50, ""},
		{"zero", map[string]string{"limit": "0"}, 10, ""},
		{"negative", map[string]string{"limit": "-3"}, 10, ""},
		{"not a number", map[string]string{"limit": "many"}, 10, ""},
		{"empty", map[string]string{"limit": ""}, 10, ""},
	} {
		if got := p.Size(tc.query); got != tc.size {
			t.Errorf("%s: size %d, want %d", tc.name, got, tc.size)
		}
		if got := p.After(tc.query); got != tc.after {
			t.Errorf("%s: after %q, want %q", tc.name, got, tc.after)
		}
	}
	if Cloud.TokenParam != "next_page_token" || Cloud.SizeParam != "page_size" || Cloud.Default != 30 || Cloud.Max != 300 {
		t.Errorf("the cloud paging is %+v", Cloud)
	}
}

// A page is the ids after the token in id order, the next token the last of the
// page while more remain; a token nobody issued starts at the first id past it.
func TestCutPagesTheSortedIds(t *testing.T) {
	ids := []string{"c", "a", "d", "b"}
	for _, tc := range []struct {
		after string
		size  int
		page  []string
		next  string
	}{
		{"", 2, []string{"a", "b"}, "b"},
		{"b", 2, []string{"c", "d"}, ""},
		{"a", 1, []string{"b"}, "b"},
		{"d", 2, nil, ""},
		{"bb", 5, []string{"c", "d"}, ""},
		{"", 4, []string{"a", "b", "c", "d"}, ""},
		{"0", 1, []string{"a"}, "a"},
	} {
		page, next := Cut(ids, tc.after, tc.size)
		if !reflect.DeepEqual(page, tc.page) && (len(page) != 0 || len(tc.page) != 0) || next != tc.next {
			t.Errorf("Cut(after %q, size %d) = %v %q, want %v %q", tc.after, tc.size, page, next, tc.page, tc.next)
		}
	}
	if !reflect.DeepEqual(ids, []string{"c", "a", "d", "b"}) {
		t.Errorf("Cut sorted the caller's ids: %v", ids)
	}
}

// The document is the layout the templates gave a paged list: blank indented
// lines above each record, an empty list closing right after its bracket.
func TestPagedDocumentKeepsTheTemplatesLayout(t *testing.T) {
	got := string(PagedDocument("rooms", []string{`{"id": "a"}`, `{"id": "b"}`}, 1, "b"))
	want := "{\n  \"rooms\": [\n    \n    {\"id\": \"a\"},\n    \n    {\"id\": \"b\"}\n  ],\n  \"page_size\": 2,\n  \"next_page_token\": \"b\"\n}\n"
	if got != want {
		t.Errorf("document:\n%q\nwant\n%q", got, want)
	}
	if got := string(PagedDocument("rooms", nil, 1, "")); got != "{\n  \"rooms\": [\n  ],\n  \"page_size\": 0,\n  \"next_page_token\": \"\"\n}\n" {
		t.Errorf("empty document: %q", got)
	}
	if got := string(PagedDocument("recordings", []string{"{}"}, 2, "")); got != "{\n  \"recordings\": [\n    \n    \n    {}\n  ],\n  \"page_size\": 1,\n  \"next_page_token\": \"\"\n}\n" {
		t.Errorf("two blank lines: %q", got)
	}
}
