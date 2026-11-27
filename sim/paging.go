package sim

import (
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

func init() { templateFuncs["pageKeys"] = pageKeys }
