package labfootprint

import (
	"strings"
	"testing"
	"time"
)

func TestParseReceiptCountReadsThePsqlRow(t *testing.T) {
	got, err := parseReceiptCount("12|3|2|4\n")
	if err != nil || got != (ReceiptCount{Landed: 12, Margin: 3, Backlog: 2, AfterEnd: 4}) {
		t.Fatalf("parse = %+v, %v", got, err)
	}
	for _, bad := range []string{"", "1|2|3", "1|x|3|4"} {
		if _, err := parseReceiptCount(bad); err == nil {
			t.Fatalf("parse %q: want an error", bad)
		}
	}
}

// A node name is quoted as a literal, never spliced as SQL.
func TestReceiptCountQueryQuotesTheNodeName(t *testing.T) {
	q := receiptCountQuery("o'brien", time.Unix(100, 5000), time.Unix(130, 0), time.Unix(200, 0))
	if !strings.Contains(q, "'og.v1.telemetry.o''brien '") {
		t.Fatalf("query does not quote the name:\n%s", q)
	}
	for _, at := range []string{"to_timestamp(100.000005)", "to_timestamp(70.000005)", "to_timestamp(130.000000)", "to_timestamp(200.000000)"} {
		if !strings.Contains(q, at) {
			t.Fatalf("query does not carry %s (the window, its margin and the drain, to the microsecond):\n%s", at, q)
		}
	}
}

func TestParseDrainedReadsThePsqlRow(t *testing.T) {
	for _, c := range []struct {
		out     string
		drained bool
		at      time.Time
	}{
		{"t|1700000000.25\n", true, time.Unix(1700000000, 250000000).UTC()},
		{"f|1700000000.5", false, time.Unix(1700000000, 500000000).UTC()},
	} {
		drained, at, err := parseDrained(c.out)
		if err != nil || drained != c.drained || !at.Equal(c.at) {
			t.Fatalf("parse %q = %v, %s, %v; want %v, %s", c.out, drained, at, err, c.drained, c.at)
		}
	}
	for _, bad := range []string{"", "t", "x|1", "t|now"} {
		if _, _, err := parseDrained(bad); err == nil {
			t.Fatalf("parse %q: want an error", bad)
		}
	}
}
