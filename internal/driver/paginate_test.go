package driver_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// pagedServer serves ids in pages of size, in the shape each test names: the
// engine knows no field name, so each shape spells its own.
type pagedServer struct {
	ids   []string
	calls []driver.PageRequest
	fail  int // the 1-based call that fails, 0 for none
}

func (s *pagedServer) records(from, n int) string {
	var recs []string
	for i := from; i < from+n && i < len(s.ids); i++ {
		recs = append(recs, fmt.Sprintf(`{"id":%q}`, s.ids[i]))
	}
	return "[" + strings.Join(recs, ",") + "]"
}

func mk(n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("r%02d", i+1)
	}
	return out
}

func ids(t *testing.T, doc []byte, path string) string {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(doc, &root); err != nil {
		t.Fatal(err)
	}
	cur := root[path]
	if strings.Contains(path, ".") {
		parts := strings.Split(path, ".")
		var inner map[string]json.RawMessage
		_ = json.Unmarshal(root[parts[0]], &inner)
		cur = inner[parts[1]]
	}
	var recs []struct{ ID string }
	if err := json.Unmarshal(cur, &recs); err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, r := range recs {
		out = append(out, r.ID)
	}
	return strings.Join(out, ",")
}

func (s *pagedServer) fetchFn(serve func(pr driver.PageRequest) driver.Page) func(driver.PageRequest) (driver.Page, error) {
	return func(pr driver.PageRequest) (driver.Page, error) {
		s.calls = append(s.calls, pr)
		if s.fail == len(s.calls) {
			return driver.Page{}, errors.New("http 503")
		}
		return serve(pr), nil
	}
}

// Each rule walks N pages and yields every record once, in order, under
// differently named fields: nothing the engine reads is a hard-coded name.
func TestPaginationWalksEachRuleOverAFixture(t *testing.T) {
	all := mk(7)
	t.Run("cursor in the body, sent as a query parameter", func(t *testing.T) {
		s := &pagedServer{ids: all}
		p := &driver.Pagination{Style: driver.PageCursor, SizeParam: "page_size", Size: 3, CursorFrom: "next_page_token", CursorParam: "next_page_token"}
		got, err := p.Walk("ep", "rooms", s.fetchFn(func(pr driver.PageRequest) driver.Page {
			from, _ := strconv.Atoi(pr.Query["next_page_token"])
			next := ""
			if from+3 < len(all) {
				next = strconv.Itoa(from + 3)
			}
			return driver.Page{Body: []byte(fmt.Sprintf(`{"rooms":%s,"next_page_token":%q,"keep":1}`, s.records(from, 3), next))}
		}))
		if err != nil {
			t.Fatal(err)
		}
		if g := ids(t, got, "rooms"); g != strings.Join(all, ",") {
			t.Errorf("records %s", g)
		}
		if len(s.calls) != 3 || s.calls[0].Query["page_size"] != "3" || s.calls[0].Query["next_page_token"] != "" || s.calls[2].Query["next_page_token"] != "6" {
			t.Errorf("calls %+v", s.calls)
		}
		if !strings.Contains(string(got), `"keep":1`) {
			t.Errorf("the first page's other fields are lost: %s", got)
		}
	})
	t.Run("cursor in a nested object, sent as a header", func(t *testing.T) {
		s := &pagedServer{ids: all}
		p := &driver.Pagination{Style: driver.PageCursor, CursorFrom: "meta.cursor", CursorHeader: "X-Cursor"}
		got, err := p.Walk("ep", "data.items", s.fetchFn(func(pr driver.PageRequest) driver.Page {
			from, _ := strconv.Atoi(pr.Header["X-Cursor"])
			cur := "null"
			if from+4 < len(all) {
				cur = strconv.Itoa(from + 4)
			}
			return driver.Page{Body: []byte(fmt.Sprintf(`{"data":{"items":%s},"meta":{"cursor":%s}}`, s.records(from, 4), strings.ReplaceAll(cur, "null", "null")))}
		}))
		if err != nil {
			t.Fatal(err)
		}
		if g := ids(t, got, "data.items"); g != strings.Join(all, ",") {
			t.Errorf("records %s", g)
		}
	})
	t.Run("cursor in a response header", func(t *testing.T) {
		s := &pagedServer{ids: all}
		p := &driver.Pagination{Style: driver.PageCursor, CursorFromHeader: "x-next", CursorParam: "after"}
		got, err := p.Walk("ep", "rooms", s.fetchFn(func(pr driver.PageRequest) driver.Page {
			from, _ := strconv.Atoi(pr.Query["after"])
			h := map[string]string{}
			if from+5 < len(all) {
				h["X-Next"] = strconv.Itoa(from + 5)
			}
			return driver.Page{Body: []byte(`{"rooms":` + s.records(from, 5) + `}`), Header: h}
		}))
		if err != nil || ids(t, got, "rooms") != strings.Join(all, ",") {
			t.Fatalf("%s %v", got, err)
		}
	})
	t.Run("page number, stopping at an empty page", func(t *testing.T) {
		s := &pagedServer{ids: all}
		p := &driver.Pagination{Style: driver.PageNumber, PageParam: "pg", SizeParam: "per", Size: 3}
		got, err := p.Walk("ep", "rooms", s.fetchFn(func(pr driver.PageRequest) driver.Page {
			pg, _ := strconv.Atoi(pr.Query["pg"])
			return driver.Page{Body: []byte(`{"rooms":` + s.records((pg-1)*3, 3) + `}`)}
		}))
		if err != nil || ids(t, got, "rooms") != strings.Join(all, ",") {
			t.Fatalf("%s %v", got, err)
		}
		if len(s.calls) != 4 || s.calls[0].Query["pg"] != "1" {
			t.Errorf("calls %+v", s.calls)
		}
	})
	t.Run("zero-based page number stopping at the declared total", func(t *testing.T) {
		s := &pagedServer{ids: all}
		zero := 0
		p := &driver.Pagination{Style: driver.PageNumber, PageParam: "p", FirstPage: &zero, PagesFrom: "info.pages"}
		got, err := p.Walk("ep", "rooms", s.fetchFn(func(pr driver.PageRequest) driver.Page {
			pg, _ := strconv.Atoi(pr.Query["p"])
			return driver.Page{Body: []byte(`{"rooms":` + s.records(pg*4, 4) + `,"info":{"pages":2}}`)}
		}))
		if err != nil || ids(t, got, "rooms") != strings.Join(all, ",") {
			t.Fatalf("%s %v", got, err)
		}
		if len(s.calls) != 2 || s.calls[0].Query["p"] != "0" {
			t.Errorf("an extra request after the last page: %+v", s.calls)
		}
	})
	t.Run("page number stopping at the declared total records", func(t *testing.T) {
		s := &pagedServer{ids: all}
		p := &driver.Pagination{Style: driver.PageNumber, PageParam: "p", SizeParam: "n", Size: 3, TotalFrom: "total"}
		got, err := p.Walk("ep", "rooms", s.fetchFn(func(pr driver.PageRequest) driver.Page {
			pg, _ := strconv.Atoi(pr.Query["p"])
			return driver.Page{Body: []byte(`{"rooms":` + s.records((pg-1)*3, 3) + `,"total":7}`)}
		}))
		if err != nil || ids(t, got, "rooms") != strings.Join(all, ",") || len(s.calls) != 3 {
			t.Fatalf("%s %v calls=%d", got, err, len(s.calls))
		}
	})
	t.Run("one page", func(t *testing.T) {
		s := &pagedServer{ids: mk(2)}
		p := &driver.Pagination{Style: driver.PageCursor, CursorFrom: "next", CursorParam: "c"}
		got, err := p.Walk("ep", "rooms", s.fetchFn(func(driver.PageRequest) driver.Page {
			return driver.Page{Body: []byte(`{"rooms":` + s.records(0, 5) + `}`)}
		}))
		if err != nil || ids(t, got, "rooms") != "r01,r02" || len(s.calls) != 1 {
			t.Fatalf("%s %v", got, err)
		}
	})
}

// The bounds fault naming the endpoint and the page, and nothing is returned.
func TestPaginationBoundsFault(t *testing.T) {
	endless := func(s *pagedServer) func(driver.PageRequest) (driver.Page, error) {
		return s.fetchFn(func(pr driver.PageRequest) driver.Page {
			from, _ := strconv.Atoi(pr.Query["c"])
			return driver.Page{Body: []byte(fmt.Sprintf(`{"rooms":%s,"next":"%d"}`, s.records(from, 2), from+2))}
		})
	}
	s := &pagedServer{ids: mk(100)}
	p := &driver.Pagination{Style: driver.PageCursor, CursorFrom: "next", CursorParam: "c", MaxPages: 3}
	got, err := p.Walk("driver poll d/f GET /api/rooms", "rooms", endless(s))
	if err == nil || got != nil || !strings.Contains(err.Error(), "driver poll d/f GET /api/rooms") || !strings.Contains(err.Error(), "cap of 3 pages") || len(s.calls) != 3 {
		t.Errorf("page bound: %v calls=%d", err, len(s.calls))
	}
	s = &pagedServer{ids: mk(100)}
	p = &driver.Pagination{Style: driver.PageCursor, CursorFrom: "next", CursorParam: "c", MaxRecords: 5}
	got, err = p.Walk("driver poll d/f GET /api/rooms", "rooms", endless(s))
	if err == nil || got != nil || !strings.Contains(err.Error(), "GET /api/rooms") || !strings.Contains(err.Error(), "cap of 5 records") || !strings.Contains(err.Error(), "nothing landed") {
		t.Errorf("record bound: %v", err)
	}
	// A cursor that answers itself is a loop, not a long list.
	s = &pagedServer{ids: mk(4)}
	p = &driver.Pagination{Style: driver.PageCursor, CursorFrom: "next", CursorParam: "c"}
	_, err = p.Walk("ep", "rooms", s.fetchFn(func(driver.PageRequest) driver.Page {
		return driver.Page{Body: []byte(`{"rooms":[{"id":"a"}],"next":"same"}`)}
	}))
	if err == nil || !strings.Contains(err.Error(), "never ends") {
		t.Errorf("loop: %v", err)
	}
}

// A failed middle page fails the walk with its number and returns nothing.
func TestPaginationFailedPageReturnsNothing(t *testing.T) {
	s := &pagedServer{ids: mk(9), fail: 2}
	p := &driver.Pagination{Style: driver.PageCursor, CursorFrom: "next", CursorParam: "c"}
	got, err := p.Walk("ep GET /x", "rooms", s.fetchFn(func(pr driver.PageRequest) driver.Page {
		from, _ := strconv.Atoi(pr.Query["c"])
		return driver.Page{Body: []byte(fmt.Sprintf(`{"rooms":%s,"next":"%d"}`, s.records(from, 3), from+3))}
	}))
	if got != nil || err == nil || !strings.Contains(err.Error(), "ep GET /x: page 2 failed") || !strings.Contains(err.Error(), "nothing landed") {
		t.Errorf("got %s err %v", got, err)
	}
	// A page whose body does not hold the records is a fault naming its page.
	_, err = p.Walk("ep", "rooms", func(driver.PageRequest) (driver.Page, error) { return driver.Page{Body: []byte(`{"other":[]}`)}, nil })
	if err == nil || !strings.Contains(err.Error(), "page 1 records") {
		t.Errorf("unreadable page: %v", err)
	}
}

func TestPaginationValidation(t *testing.T) {
	one := 1
	bad := map[string]driver.Pagination{
		"unknown style":        {Style: "offset"},
		"cursor no read":       {Style: driver.PageCursor, CursorParam: "c"},
		"cursor no send":       {Style: driver.PageCursor, CursorFrom: "n"},
		"cursor both sends":    {Style: driver.PageCursor, CursorFrom: "n", CursorParam: "c", CursorHeader: "X-C"},
		"cursor with page":     {Style: driver.PageCursor, CursorFrom: "n", CursorParam: "c", PageParam: "p"},
		"page no param":        {Style: driver.PageNumber},
		"page with cursor":     {Style: driver.PageNumber, PageParam: "p", CursorParam: "c"},
		"size without param":   {Style: driver.PageNumber, PageParam: "p", Size: 3},
		"total needs size":     {Style: driver.PageNumber, PageParam: "p", TotalFrom: "t"},
		"two totals":           {Style: driver.PageNumber, PageParam: "p", PagesFrom: "a", TotalFrom: "b", SizeParam: "s", Size: 1},
		"max pages too large":  {Style: driver.PageNumber, PageParam: "p", MaxPages: driver.HardMaxPages + 1},
		"max records too many": {Style: driver.PageNumber, PageParam: "p", MaxRecords: driver.HardMaxPagedRecords + 1},
		"reserved header":      {Style: driver.PageCursor, CursorFrom: "n", CursorHeader: "Authorization"},
		"empty step":           {Style: driver.PageCursor, CursorFrom: "a..b", CursorParam: "c"},
	}
	for name, p := range bad {
		spec := pagedSpec(&p)
		if err := parseValidate(spec); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
	good := driver.Pagination{Style: driver.PageNumber, PageParam: "p", FirstPage: &one, TotalFrom: "t", SizeParam: "s", Size: 10}
	if err := parseValidate(pagedSpec(&good)); err != nil {
		t.Errorf("good rule refused: %v", err)
	}
	// Paging needs the records it gathers, and a poll's GET.
	noRecords := strings.Replace(pagedSpec(&good), `"records":"rooms",`, "", 1)
	if err := parseValidate(noRecords); err == nil || !strings.Contains(err.Error(), "records") {
		t.Errorf("paginate with no records: %v", err)
	}
	post := strings.Replace(pagedSpec(&good), `"path":"/r"`, `"path":"/r","method":"POST"`, 1)
	if err := parseValidate(post); err == nil || !strings.Contains(err.Error(), "GET") {
		t.Errorf("paginate on a POST: %v", err)
	}
}

func parseValidate(raw string) error {
	sp, err := driver.Parse([]byte(raw))
	if err != nil {
		return err
	}
	return sp.Validate(nil)
}

func pagedSpec(p *driver.Pagination) string {
	pj, _ := json.Marshal(p)
	return `{"version":1,"transport":"http","polls":[{"name":"rooms","schedule":{"every":"30s"},"request":{"path":"/r","paginate":` + string(pj) + `},"records":"rooms","identity":{"key":"ucc-room-id","extract":{"jsonpath":"id"}},"emits":[{"name":"call-state","extract":{"jsonpath":"state"}}]}]}`
}

// A walk of large records is bounded by the bytes it holds, not only their
// count, and a refused cursor never echoes its value in the fault.
func TestPaginationHoldsBoundedBytes(t *testing.T) {
	big := strings.Repeat("x", 1<<20)
	p := &driver.Pagination{Style: driver.PageCursor, CursorFrom: "next", CursorParam: "c"}
	n := 0
	_, err := p.Walk("ep GET /x", "rooms", func(driver.PageRequest) (driver.Page, error) {
		n++
		return driver.Page{Body: []byte(fmt.Sprintf(`{"rooms":[{"id":%q}],"next":"p%d"}`, big, n))}, nil
	})
	if err == nil || !strings.Contains(err.Error(), "32 MiB") || n > 40 {
		t.Errorf("n=%d err=%v", n, err)
	}
}
