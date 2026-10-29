package bus

import (
	"context"
	"errors"
	"strings"
	"testing"
)

// partitionStore is a store whose metric partition roll answers with err and
// counts its calls.
type partitionStore struct {
	fakeStore
	err   error
	calls *int
}

func (s partitionStore) EnsureMetricPartitions(context.Context) ([]string, error) {
	*s.calls++
	return nil, s.err
}

// The bus makes sure the metric table has this month's partition and the next
// two before its ingest starts (#420), and refuses to start when it cannot:
// ingest over a missing month would only retry until one exists.
func TestTheBusChecksTheMetricMonthsBeforeIngest(t *testing.T) {
	calls := 0
	srv, err := New(Config{Host: "127.0.0.1", Port: 0}, partitionStore{calls: &calls})
	if err != nil {
		t.Fatalf("start: %v", err)
	}
	srv.Shutdown()
	if calls == 0 {
		t.Fatal("the bus started its ingest without checking the metric partitions")
	}
	calls = 0
	_, err = New(Config{Host: "127.0.0.1", Port: 0}, partitionStore{calls: &calls, err: errors.New("lock timeout")})
	if err == nil || !strings.Contains(err.Error(), "metric partitions") {
		t.Fatalf("start over a failed partition check: err %v, want the bus refused naming the partitions", err)
	}
}
