package collection_test

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/hyperscaleav/omniglass/internal/collection"
	"github.com/hyperscaleav/omniglass/internal/driver"
)

// scriptedPages answers a list endpoint a page at a time, by whatever
// query parameter its test names, and records each request it was sent.
type scriptedPages struct {
	mu    sync.Mutex
	total int
	size  int
	fail  int // the 1-based request that draws a 503
	sent  []collection.HTTPExchange
}

func (s *scriptedPages) Exchange(_ context.Context, x collection.HTTPExchange) (collection.HTTPAnswer, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.sent = append(s.sent, x)
	if s.fail == len(s.sent) {
		return collection.HTTPAnswer{Status: 503, Body: []byte(`{}`)}, nil
	}
	from, _ := strconv.Atoi(x.Request.Query["after"])
	var recs []string
	for i := from; i < from+s.size && i < s.total; i++ {
		recs = append(recs, fmt.Sprintf(`{"id":"r%d","state":"idle"}`, i))
	}
	next := ""
	if from+s.size < s.total {
		next = strconv.Itoa(from + s.size)
	}
	return collection.HTTPAnswer{Status: 200, Body: []byte(fmt.Sprintf(`{"result":{"items":[%s]},"paging":{"cursor":%q}}`, strings.Join(recs, ","), next))}, nil
}

func pagedTask(p *driver.Pagination) collection.DriverPollTask {
	task := httpPollTask()
	task.Auth, task.Secrets = nil, nil
	task.Fn = &driver.BakedFunction{
		Driver: "d", Function: "list",
		Request:  &driver.Request{Path: "/v1/items", Query: map[string]string{"fixed": "1"}, Paginate: p},
		Records:  "result.items",
		Identity: &driver.IdentitySpec{Key: "ucc-room-id", Extract: driver.Extract{JSONPath: "id"}},
		Emits:    []driver.BakedEmit{{Name: "call-state", Lane: "property", Extract: driver.Extract{JSONPath: "state"}}},
	}
	return task
}

// One poll walks every page and yields one identified sample per record,
// past the single-answer cap, and each request carries the declared query
// beside the cursor.
func TestCollectDriverPollWalksPagesPastTheSingleAnswerCap(t *testing.T) {
	f := &scriptedPages{total: driver.MaxRecords + 500, size: 1000}
	p := &driver.Pagination{Style: driver.PageCursor, SizeParam: "limit", Size: 1000, CursorFrom: "paging.cursor", CursorParam: "after"}
	dps, faults := (&collection.Runner{HTTPFetch: f}).CollectDriverPoll(context.Background(), pagedTask(p))
	if len(faults) != 0 {
		t.Fatalf("faults %v", faults)
	}
	if len(dps) != f.total || len(f.sent) != 3 {
		t.Fatalf("samples %d requests %d, want %d and 3", len(dps), len(f.sent), f.total)
	}
	if q := f.sent[1].Request.Query; q["fixed"] != "1" || q["limit"] != "1000" || q["after"] != "1000" {
		t.Errorf("second page's query %v", q)
	}
	if dps[0].Identity.Value != "r0" || dps[len(dps)-1].Identity.Value != fmt.Sprintf("r%d", f.total-1) {
		t.Errorf("order: first %v last %v", dps[0].Identity, dps[len(dps)-1].Identity)
	}
}

// A failed middle page fails the poll naming the page, with nothing landed;
// a bound faults naming the endpoint.
func TestCollectDriverPollPagedFaults(t *testing.T) {
	p := &driver.Pagination{Style: driver.PageCursor, CursorFrom: "paging.cursor", CursorParam: "after"}
	f := &scriptedPages{total: 30, size: 10, fail: 2}
	dps, faults := (&collection.Runner{HTTPFetch: f}).CollectDriverPoll(context.Background(), pagedTask(p))
	if len(dps) != 0 || len(faults) != 1 || !strings.Contains(faults[0].Error(), "page 2 failed") || !strings.Contains(faults[0].Error(), "GET /v1/items") {
		t.Fatalf("samples %d faults %v", len(dps), faults)
	}
	p.MaxPages = 2
	f = &scriptedPages{total: 30, size: 10}
	dps, faults = (&collection.Runner{HTTPFetch: f}).CollectDriverPoll(context.Background(), pagedTask(p))
	if len(dps) != 0 || len(faults) != 1 || !strings.Contains(faults[0].Error(), "d/list GET /v1/items") || !strings.Contains(faults[0].Error(), "cap of 2 pages") {
		t.Fatalf("samples %d faults %v", len(dps), faults)
	}
}
