package sim

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
)

// A paged list API (#2765) hands its records a page at a time and a token for
// the next: the way a real cloud platform lists thousands of rooms. pageKeys is
// the template's mechanics, so a profile's route stays declarative.

const (
	// defaultPageSize is the page a request that names none gets.
	defaultPageSize = 30
	// maxPageSize is the most a request may ask for.
	maxPageSize = 300
)

// pageKeys is one page of the sorted keys of a map: the keys after the token
// (the last key of the page before, empty for the first page), at most size of
// them (a number or its text; the default when it is empty or unreadable, the
// maximum when larger), and the token that asks for the next page, empty on
// the last. Pure.
//
//	{{ $p := pageKeys .State.status.rooms .Request.Query.next_page_token .Request.Query.page_size }}
//	{{ range $p.keys }}...{{ end }} "next_page_token": {{ marshal $p.next }}
func pageKeys(m any, after any, size any) map[string]any {
	keys := []string{}
	if mm, ok := m.(map[string]any); ok {
		for k := range mm {
			keys = append(keys, k)
		}
	}
	sort.Strings(keys)
	n := defaultPageSize
	if v, err := strconv.Atoi(fmt.Sprint(size)); err == nil && v > 0 {
		n = min(v, maxPageSize)
	}
	tok := ""
	if after != nil {
		tok = fmt.Sprint(after)
		if tok == "<no value>" {
			tok = ""
		}
	}
	start := 0
	if tok != "" {
		start = sort.SearchStrings(keys, tok)
		if start < len(keys) && keys[start] == tok {
			start++
		}
	}
	end := min(start+n, len(keys))
	page := keys[start:end]
	next := ""
	if end < len(keys) && len(page) > 0 {
		next = page[len(page)-1]
	}
	return map[string]any{"keys": page, "next": next}
}

// whole is a number read back from state as a whole number: state round-trips
// through JSON, which hands a count or a unix time back as a float, and a
// template that prints one would write 1.79565124e+09. Zero for a value that
// is no number.
//
//	"start": {{ whole (index $r "start") }}
func whole(v any) int64 {
	switch n := v.(type) {
	case int:
		return int64(n)
	case int64:
		return n
	case float64:
		return int64(n)
	case json.Number:
		if i, err := n.Int64(); err == nil {
			return i
		}
		f, _ := n.Float64()
		return int64(f)
	case string:
		i, _ := strconv.ParseInt(n, 10, 64)
		return i
	}
	return 0
}

func init() {
	templateFuncs["pageKeys"] = pageKeys
	templateFuncs["whole"] = whole
}
