package labfootprint

import (
	"strings"
	"testing"
)

func TestParseCPUModelReadsTheFirstModelName(t *testing.T) {
	in := "processor\t: 0\nvendor_id\t: AuthenticAMD\nmodel name\t: AMD Ryzen 9 7940HS w/ Radeon 780M Graphics\n\nprocessor\t: 1\nmodel name\t: AMD Ryzen 9 7940HS w/ Radeon 780M Graphics\n"
	if got := parseCPUModel(strings.NewReader(in)); got != "AMD Ryzen 9 7940HS w/ Radeon 780M Graphics" {
		t.Fatalf("model = %q", got)
	}
	if got := parseCPUModel(strings.NewReader("processor\t: 0\n")); got != "" {
		t.Fatalf("a cpuinfo with no model name read %q, want empty", got)
	}
}

func TestParseMemoryGiBRoundsMemTotal(t *testing.T) {
	cases := map[string]int{
		"MemTotal:       32554184 kB\nMemFree: 1 kB\n": 31,
		"MemFree: 1 kB\nMemTotal:       16777216 kB\n": 16,
		"MemTotal:       16252928 kB\n":                16, // 15.5 GiB rounds up
		"MemFree: 1 kB\n":                              0,
		"MemTotal: lots kB\n":                          0,
	}
	for in, want := range cases {
		if got := parseMemoryGiB(strings.NewReader(in)); got != want {
			t.Errorf("parseMemoryGiB(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestReadFingerprintNamesThisHost(t *testing.T) {
	f := ReadFingerprint()
	if f.Cores <= 0 || !strings.HasPrefix(f.Go, "go") {
		t.Fatalf("fingerprint = %+v, want cores and a Go version", f)
	}
}
