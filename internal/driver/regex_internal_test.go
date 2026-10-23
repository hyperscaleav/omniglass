package driver

import "testing"

// Review of #817, finding 4: a pattern is compiled once and reused, not
// recompiled for every inbound line on a held session's reader.
func TestRegexCompiledOnce(t *testing.T) {
	a, err := compiled(`^EVT INPUT (\S+)$`)
	if err != nil {
		t.Fatal(err)
	}
	b, _ := compiled(`^EVT INPUT (\S+)$`)
	if a != b {
		t.Fatal("the same pattern compiled twice")
	}
	if _, err := compiled("("); err == nil {
		t.Fatal("a bad pattern compiled")
	}
	m := Match{Regex: `^EVT `}
	m.Matches("EVT INPUT a")
	before := compileCount.Load()
	for i := 0; i < 100; i++ {
		m.Matches("EVT INPUT a")
		_, _ = locate(Extract{Regex: `^EVT INPUT (\S+)$`}, Payload{Text: "EVT INPUT a"})
	}
	if n := compileCount.Load() - before; n != 0 {
		t.Fatalf("matching 100 lines compiled %d patterns", n)
	}
}
