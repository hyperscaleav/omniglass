package sim

import (
	"fmt"
	"strings"
	"testing"
)

// A paged list walks every key once, in order, whatever the page size, and the
// last page names no next token.
func TestPageKeysWalkEveryKeyOnce(t *testing.T) {
	m := map[string]any{}
	var want []string
	for i := 0; i < 23; i++ {
		k := fmt.Sprintf("room-%02d", i)
		m[k] = i
		want = append(want, k)
	}
	for _, size := range []any{nil, "", "7", 7, "1", "500", "bad"} {
		var got []string
		tok := ""
		for pages := 0; pages < 100; pages++ {
			p := pageKeys(m, tok, size)
			got = append(got, p["keys"].([]string)...)
			tok = p["next"].(string)
			if tok == "" {
				break
			}
		}
		if strings.Join(got, ",") != strings.Join(want, ",") {
			t.Fatalf("size %v walked %v", size, got)
		}
	}
	if p := pageKeys(m, "room-22", 5); len(p["keys"].([]string)) != 0 || p["next"] != "" {
		t.Fatalf("past the end: %v", p)
	}
}
