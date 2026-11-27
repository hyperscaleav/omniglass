package driver

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

// A paged list API (#2789): a cloud platform lists thousands of rooms or
// devices a page at a time, so a poll's request declares how its pages chain
// and the collection engine walks them all in one poll. The records of every
// page are gathered into the one answer the function's `records` path reads,
// then identified and fanned out once (ADR-1282), exactly as a single-page
// answer is. A page that fails, or a bound that is hit, fails the poll and
// nothing lands.

// The pagination styles.
const (
	// PageCursor reads a cursor (a value the platform hands back) off each
	// answer and sends it on the next request, until an answer carries none.
	PageCursor = "cursor"
	// PageNumber sends an ascending page number, until an answer has no
	// records or its own declared total says the last page was read.
	PageNumber = "page"
)

// The pagination bounds: a poll walks at most DefaultMaxPages pages and
// gathers at most DefaultMaxPagedRecords records unless the rule declares
// its own, which may not pass the hard caps. MaxRecords still bounds an
// unpaged answer.
const (
	DefaultMaxPages        = 100
	HardMaxPages           = 1000
	DefaultMaxPagedRecords = 10000
	HardMaxPagedRecords    = 100000
)

// Pagination is the rule on an http poll's request.
type Pagination struct {
	// Style is token or page.
	Style string `json:"style"`
	// SizeParam is the query parameter that carries the page size, Size the
	// size asked for; both or neither. Absent, the platform's default size
	// applies.
	SizeParam string `json:"size_param,omitempty"`
	Size      int    `json:"size,omitempty"`
	// CursorFrom is the dotted path in the answer's JSON body where the
	// next cursor lives, or CursorFromHeader the response header that
	// carries it (exactly one); absent, null or empty on the last page.
	CursorFrom       string `json:"cursor_from,omitempty"`
	CursorFromHeader string `json:"cursor_from_header,omitempty"`
	// CursorParam is the query parameter the cursor is sent back in, or
	// CursorHeader the request header (exactly one).
	CursorParam  string `json:"cursor_param,omitempty"`
	CursorHeader string `json:"cursor_header,omitempty"`
	// PageParam is the query parameter a page style sends the page number
	// in, counting from FirstPage (1 when absent; 0 for a zero-based API).
	// PagesFrom and TotalFrom optionally name the dotted paths of the
	// answer's total page count and total record count, so the walk stops
	// without asking for an empty page.
	PageParam string `json:"page_param,omitempty"`
	FirstPage *int   `json:"first_page,omitempty"`
	PagesFrom string `json:"pages_from,omitempty"`
	TotalFrom string `json:"total_from,omitempty"`
	// MaxPages and MaxRecords bound one poll's walk; reaching either is a
	// fault, never a silent truncation.
	MaxPages   int `json:"max_pages,omitempty"`
	MaxRecords int `json:"max_records,omitempty"`
}

// PagesCap is the page bound in force.
func (p *Pagination) PagesCap() int {
	if p.MaxPages > 0 {
		return p.MaxPages
	}
	return DefaultMaxPages
}

// RecordsCap is the total record bound in force.
func (p *Pagination) RecordsCap() int {
	if p.MaxRecords > 0 {
		return p.MaxRecords
	}
	return DefaultMaxPagedRecords
}

func (p *Pagination) firstPage() int {
	if p.FirstPage != nil {
		return *p.FirstPage
	}
	return 1
}

// validate holds a rule to its shape.
func (p *Pagination) validate(what string) error {
	bad := func(format string, a ...any) error {
		return fmt.Errorf("driver: %s paginate %s", what, fmt.Sprintf(format, a...))
	}
	switch p.Style {
	case PageCursor:
		if (p.CursorFrom == "") == (p.CursorFromHeader == "") {
			return bad("style cursor needs exactly one of cursor_from (the path of the cursor in the answer body) and cursor_from_header (the response header)")
		}
		if (p.CursorParam == "") == (p.CursorHeader == "") {
			return bad("style cursor needs exactly one of cursor_param (the query parameter the cursor is sent back in) and cursor_header (the request header)")
		}
		if p.PageParam != "" || p.PagesFrom != "" || p.TotalFrom != "" || p.FirstPage != nil {
			return bad("style cursor does not read page_param, first_page, pages_from or total_from")
		}
		if p.CursorFrom != "" {
			if err := checkDotted(p.CursorFrom); err != nil {
				return bad("cursor_from %q %v", p.CursorFrom, err)
			}
		}
		if p.CursorHeader != "" && !isToken(p.CursorHeader) {
			return bad("cursor_header %q is not an http token", p.CursorHeader)
		}
	case PageNumber:
		if p.PageParam == "" {
			return bad("style page needs page_param (the query parameter the page number is sent in)")
		}
		if p.CursorFrom != "" || p.CursorFromHeader != "" || p.CursorParam != "" || p.CursorHeader != "" {
			return bad("style page does not read the cursor fields")
		}
		if p.FirstPage != nil && *p.FirstPage < 0 {
			return bad("first_page %d is negative", *p.FirstPage)
		}
		for _, path := range []string{p.PagesFrom, p.TotalFrom} {
			if path != "" {
				if err := checkDotted(path); err != nil {
					return bad("%q %v", path, err)
				}
			}
		}
		if p.PagesFrom != "" && p.TotalFrom != "" {
			return bad("declares both pages_from and total_from; one total is enough")
		}
		if p.TotalFrom != "" && p.Size == 0 {
			return bad("total_from counts records, so it needs the page size (size_param and size)")
		}
	default:
		return bad("style %q is not %s or %s", p.Style, PageCursor, PageNumber)
	}
	if (p.SizeParam == "") != (p.Size == 0) {
		return bad("size_param and size go together (the query parameter and the page size it carries)")
	}
	if p.Size < 0 {
		return bad("size %d is negative", p.Size)
	}
	if p.MaxPages < 0 || p.MaxPages > HardMaxPages {
		return bad("max_pages %d is outside 0 to %d", p.MaxPages, HardMaxPages)
	}
	if p.MaxRecords < 0 || p.MaxRecords > HardMaxPagedRecords {
		return bad("max_records %d is outside 0 to %d", p.MaxRecords, HardMaxPagedRecords)
	}
	return nil
}

func checkDotted(path string) error {
	for _, step := range strings.Split(path, ".") {
		if step == "" {
			return fmt.Errorf("has an empty step; write a dotted path of object keys")
		}
	}
	return nil
}

// Page is one answer: its body and its response headers (canonical keys).
type Page struct {
	Body   []byte
	Header map[string]string
}

// PageRequest is the extra request fields one page adds to the declared ones.
type PageRequest struct {
	Query  map[string]string
	Header map[string]string
}

func (p *Pagination) request(cursor string, page int) PageRequest {
	r := PageRequest{Query: map[string]string{}, Header: map[string]string{}}
	if p.SizeParam != "" {
		r.Query[p.SizeParam] = strconv.Itoa(p.Size)
	}
	switch p.Style {
	case PageCursor:
		if cursor != "" {
			if p.CursorParam != "" {
				r.Query[p.CursorParam] = cursor
			} else {
				r.Header[p.CursorHeader] = cursor
			}
		}
	case PageNumber:
		r.Query[p.PageParam] = strconv.Itoa(page)
	}
	return r
}

// Walk fetches every page of a list and returns one answer: the first page's
// body with the array at records replaced by the records of every page, in
// page order. fetch sends one request with the extra query parameters it is
// given and returns the page's body. endpoint names the request in a fault.
// A failed page, an unreadable page, or a bound reached returns an error
// naming the endpoint and the page, and no answer.
func (p *Pagination) Walk(endpoint, records string, fetch func(PageRequest) (Page, error)) ([]byte, error) {
	maxPages, maxRecords := p.PagesCap(), p.RecordsCap()
	var first []byte
	var all []json.RawMessage
	cursor, page := "", p.firstPage()
	for n := 1; ; n++ {
		if n > maxPages {
			return nil, fmt.Errorf("%s: the list still has more pages after the cap of %d pages (max_pages); nothing landed", endpoint, maxPages)
		}
		pg, err := fetch(p.request(cursor, page))
		if err != nil {
			return nil, fmt.Errorf("%s: page %d failed: %w; nothing landed", endpoint, n, err)
		}
		body := pg.Body
		recs, err := rawRecordsOf(body, records)
		if err != nil {
			return nil, fmt.Errorf("%s: page %d records %q: %w; nothing landed", endpoint, n, records, err)
		}
		if first == nil {
			first = body
		}
		if len(all)+len(recs) > maxRecords {
			return nil, fmt.Errorf("%s: more than the cap of %d records (max_records) by page %d; nothing landed", endpoint, maxRecords, n)
		}
		all = append(all, recs...)
		switch p.Style {
		case PageCursor:
			var next string
			if p.CursorFromHeader != "" {
				next = strings.TrimSpace(pg.Header[http.CanonicalHeaderKey(p.CursorFromHeader)])
			} else if next, err = scalarAt(body, p.CursorFrom); err != nil {
				return nil, fmt.Errorf("%s: page %d %w; nothing landed", endpoint, n, err)
			}
			if next == "" {
				return mergeRecords(first, records, all)
			}
			if next == cursor {
				return nil, fmt.Errorf("%s: page %d answered the cursor it was asked with (%q), a list that never ends; nothing landed", endpoint, n, next)
			}
			cursor = next
		case PageNumber:
			if len(recs) == 0 {
				return mergeRecords(first, records, all)
			}
			if p.PagesFrom != "" || p.TotalFrom != "" {
				path := p.PagesFrom
				if path == "" {
					path = p.TotalFrom
				}
				total, err := scalarAt(body, path)
				if err != nil {
					return nil, fmt.Errorf("%s: page %d %w; nothing landed", endpoint, n, err)
				}
				if t, err := strconv.Atoi(total); err == nil {
					if p.TotalFrom != "" {
						if n*p.Size >= t {
							return mergeRecords(first, records, all)
						}
					} else if n >= t {
						return mergeRecords(first, records, all)
					}
				}
			}
			page++
		}
	}
}

// rawRecordsOf is the array at path of a page, element by element.
func rawRecordsOf(doc []byte, path string) ([]json.RawMessage, error) {
	cur, err := rawAt(doc, path)
	if err != nil {
		return nil, err
	}
	var elems []json.RawMessage
	if err := json.Unmarshal(cur, &elems); err != nil || elems == nil {
		return nil, fmt.Errorf("the path lands on a non-array")
	}
	return elems, nil
}

// rawAt walks a dotted path of object keys.
func rawAt(doc []byte, path string) (json.RawMessage, error) {
	if len(doc) == 0 {
		return nil, fmt.Errorf("the response carries no JSON body")
	}
	cur := json.RawMessage(doc)
	for _, step := range strings.Split(path, ".") {
		var obj map[string]json.RawMessage
		if err := json.Unmarshal(cur, &obj); err != nil || obj == nil {
			if !json.Valid(cur) {
				return nil, fmt.Errorf("the response is not JSON")
			}
			return nil, fmt.Errorf("the path steps through a non-object")
		}
		next, ok := obj[step]
		if !ok {
			return nil, fmt.Errorf("the response has no key %q", step)
		}
		cur = next
	}
	return cur, nil
}

// scalarAt reads a string or number at path as text; a missing key, null
// and an empty string are all "" (nothing more to read).
func scalarAt(doc []byte, path string) (string, error) {
	cur, err := rawAt(doc, path)
	if err != nil {
		var obj map[string]json.RawMessage
		if json.Unmarshal(doc, &obj) == nil && strings.HasPrefix(err.Error(), "the response has no key") {
			return "", nil
		}
		return "", fmt.Errorf("reads %q: %w", path, err)
	}
	var s string
	if json.Unmarshal(cur, &s) == nil {
		return strings.TrimSpace(s), nil
	}
	var n json.Number
	if dec := json.NewDecoder(bytes.NewReader(cur)); dec.Decode(&n) == nil {
		return n.String(), nil
	}
	if bytes.Equal(bytes.TrimSpace(cur), []byte("null")) {
		return "", nil
	}
	return "", fmt.Errorf("reads %q: not a string or a number", path)
}

// mergeRecords is body with the array at path replaced by recs.
func mergeRecords(body []byte, path string, recs []json.RawMessage) ([]byte, error) {
	arr, err := json.Marshal(recs)
	if err != nil {
		return nil, err
	}
	if recs == nil {
		arr = []byte("[]")
	}
	return setAt(body, strings.Split(path, "."), arr)
}

func setAt(doc []byte, steps []string, val []byte) ([]byte, error) {
	if len(steps) == 0 {
		return val, nil
	}
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(doc, &obj); err != nil || obj == nil {
		return nil, fmt.Errorf("the path steps through a non-object")
	}
	inner, err := setAt(obj[steps[0]], steps[1:], val)
	if err != nil {
		return nil, err
	}
	obj[steps[0]] = inner
	return json.Marshal(obj)
}
