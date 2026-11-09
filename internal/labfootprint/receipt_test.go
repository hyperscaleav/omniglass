package labfootprint

import (
	"strings"
	"testing"
	"time"
)

func TestParseReceiptCountReadsThePsqlRow(t *testing.T) {
	got, err := parseReceiptCount("12|3|4\n")
	if err != nil || got != (ReceiptCount{Landed: 12, Margin: 3, AfterEnd: 4}) {
		t.Fatalf("parse = %+v, %v", got, err)
	}
	for _, bad := range []string{"", "1|2", "1|x|3"} {
		if _, err := parseReceiptCount(bad); err == nil {
			t.Fatalf("parse %q: want an error", bad)
		}
	}
}

// A node name is quoted as a literal, never spliced as SQL.
func TestReceiptCountQueryQuotesTheNodeName(t *testing.T) {
	q := receiptCountQuery("o'brien", time.Unix(100, 5000), time.Unix(200, 0))
	if !strings.Contains(q, "'og.v1.telemetry.o''brien '") {
		t.Fatalf("query does not quote the name:\n%s", q)
	}
	if !strings.Contains(q, "to_timestamp(100.000005)") || !strings.Contains(q, "to_timestamp(70.000005)") {
		t.Fatalf("query does not carry the window and its margin to the microsecond:\n%s", q)
	}
}
