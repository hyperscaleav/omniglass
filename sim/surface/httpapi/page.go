package httpapi

import (
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Paging is how a cloud platform's list API pages (ADR-2789): the query parameter
// a client sends the last id of the page before in, the one it sends the size in,
// the size a request that names none gets and the most it may ask for. A family
// declares its own names; the engine knows none.
type Paging struct {
	TokenParam, SizeParam string
	Default, Max          int
}

// Cloud is the paging the simulated cloud services share: next_page_token and
// page_size, thirty a page and three hundred at most.
var Cloud = Paging{TokenParam: "next_page_token", SizeParam: "page_size", Default: 30, Max: 300}

// Size is the page size a request asks for: the parameter as a whole number
// above zero, capped at the most; the default when it is missing, not a number,
// zero or negative.
func (p Paging) Size(query map[string]string) int {
	if n, err := strconv.Atoi(query[p.SizeParam]); err == nil && n > 0 {
		return min(n, p.Max)
	}
	return p.Default
}

// After is the token a request sends: the last id of the page before, empty for
// the first page.
func (p Paging) After(query map[string]string) string { return query[p.TokenParam] }

// Cut is one page of the sorted ids: the ids after the token (the first id past
// it when the token names none), at most size of them, and the token that asks for
// the next page (the last id of this one), empty on the last. Pure.
func Cut(ids []string, after string, size int) (page []string, next string) {
	ids = append([]string(nil), ids...)
	sort.Strings(ids)
	start := 0
	if after != "" {
		start = sort.SearchStrings(ids, after)
		if start < len(ids) && ids[start] == after {
			start++
		}
	}
	end := min(start+size, len(ids))
	page = ids[start:end]
	if end < len(ids) && len(page) > 0 {
		next = page[len(page)-1]
	}
	return page, next
}

// PagedDocument is the document a paged list answers: its records under key, one a
// line, each already rendered, then the size of the page and the token for the
// next. A record sits under blank indented lines, one for each comment the
// template that first wrote the layout carried above it, so a document's bytes
// stay what they were (blank is how many a list has).
func PagedDocument(key string, records []string, blank int, next string) []byte {
	var b strings.Builder
	fmt.Fprintf(&b, "{\n  %q: [", key)
	for i, r := range records {
		if i > 0 {
			b.WriteString(",")
		}
		b.WriteString(strings.Repeat("\n    ", blank+1) + r)
	}
	fmt.Fprintf(&b, "\n  ],\n  \"page_size\": %d,\n  \"next_page_token\": %s\n}\n", len(records), string(encode(next, "", "")))
	return []byte(b.String())
}
