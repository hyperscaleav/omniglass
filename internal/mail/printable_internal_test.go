package mail

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// A relay's reply lands in a text column: no NUL, valid UTF-8, bounded, cut
// on a rune boundary.
func TestARelayReplyIsFitForATextColumn(t *testing.T) {
	reply := "550 no\x00such\xffuser " + strings.Repeat("é", 600)
	got := printable(reply)
	if strings.ContainsRune(got, 0) || !utf8.ValidString(got) || len(got) > 512 {
		t.Fatalf("printable = %q (len %d)", got, len(got))
	}
	if !strings.HasPrefix(got, "550 nosuch�user é") {
		t.Fatalf("printable = %q", got[:30])
	}
}
