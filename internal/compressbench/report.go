package compressbench

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/hyperscaleav/omniglass/internal/labfootprint"
)

// Cell is one candidate at one cadence over every run: each figure the median
// of the runs, with their range.
type Cell struct {
	Candidate      string            `json:"candidate"`
	Cadence        string            `json:"cadence"`
	Samples        int               `json:"samples"`
	BytesPerSample labfootprint.Stat `json:"bytes_per_sample"`
	// CompressPer1k is milliseconds to compress 1000 samples; zero N for raw,
	// which is not compressed.
	CompressPer1k labfootprint.Stat `json:"compress_ms_per_1k"`
	ReadRaw       labfootprint.Stat `json:"read_24h_ms"`
	ReadBuckets   labfootprint.Stat `json:"read_30d_buckets_ms"`
	// Error is why the candidate did not give back what it was given, or did
	// not run; empty when every read and the round trip were exact.
	Error string `json:"error,omitempty"`
}

// Summarize is the cell for ms, the runs of one candidate at one cadence.
func Summarize(candidate string, cadence time.Duration, ms []Measure, compressed bool) Cell {
	c := Cell{Candidate: candidate, Cadence: CadenceLabel(cadence)}
	var bps, comp []float64
	var raw, bk []time.Duration
	for _, m := range ms {
		c.Samples = m.Samples
		bps = append(bps, float64(m.Bytes)/float64(m.Samples))
		if compressed {
			comp = append(comp, float64(m.Compress)/float64(time.Millisecond)/(float64(m.Samples)/1000))
		}
		if m.ReadRaw > 0 {
			raw = append(raw, m.ReadRaw)
		}
		bk = append(bk, m.ReadBuckets)
	}
	c.BytesPerSample = labfootprint.StatOf(bps)
	c.CompressPer1k = labfootprint.StatOf(comp)
	c.ReadRaw = labfootprint.LatencyStat(raw)
	c.ReadBuckets = labfootprint.LatencyStat(bk)
	return c
}

// CadenceLabel is a cadence as the ticket writes it: 2s, 30s, 60s, 24h.
func CadenceLabel(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d%time.Second == 0:
		return fmt.Sprintf("%ds", d/time.Second)
	}
	return d.String()
}

// Report is every cell of a benchmark, and the settings it ran under.
type Report struct {
	Runs  int    `json:"runs"`
	Reps  int    `json:"reps"`
	Specs []Spec `json:"specs"`
	Cells []Cell `json:"cells"`
}

// Table is the report as markdown: one table a cadence, then bytes per sample
// for every candidate across the cadences.
func (r Report) Table() string {
	var b strings.Builder
	cadences := r.cadences()
	for _, cad := range cadences {
		fmt.Fprintf(&b, "### %s cadence\n\n", cad)
		b.WriteString("| candidate | samples | B/sample | compress ms/1k | 24 h raw read ms | 30 d bucketed read ms | exact |\n")
		b.WriteString("|---|--:|--:|--:|--:|--:|---|\n")
		for _, c := range r.Cells {
			if c.Cadence != cad {
				continue
			}
			fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s | %s |\n", c.Candidate, c.Samples,
				stat(c.BytesPerSample, 2), stat(c.CompressPer1k, 2), stat(c.ReadRaw, 2), stat(c.ReadBuckets, 2), exact(c.Error))
		}
		b.WriteString("\n")
	}
	b.WriteString("### Bytes per sample across cadences\n\n| candidate |")
	for _, cad := range cadences {
		fmt.Fprintf(&b, " %s |", cad)
	}
	b.WriteString("\n|---|")
	for range cadences {
		b.WriteString("--:|")
	}
	b.WriteString("\n")
	for _, name := range r.candidates() {
		fmt.Fprintf(&b, "| %s |", name)
		for _, cad := range cadences {
			v := "n/a"
			for _, c := range r.Cells {
				if c.Candidate == name && c.Cadence == cad && c.BytesPerSample.N > 0 {
					v = fmt.Sprintf("%.2f", c.BytesPerSample.Median)
				}
			}
			fmt.Fprintf(&b, " %s |", v)
		}
		b.WriteString("\n")
	}
	return b.String()
}

func (r Report) cadences() []string {
	var out []string
	seen := map[string]bool{}
	for _, c := range r.Cells {
		if !seen[c.Cadence] {
			seen[c.Cadence] = true
			out = append(out, c.Cadence)
		}
	}
	return out
}

// candidates is every candidate in first-seen order, the order Candidates
// lists them, so each cadence's extras (day blocks, month partitions) land
// beside their hour and day kin.
func (r Report) candidates() []string {
	var out []string
	pos := map[string]int{}
	for _, c := range r.Cells {
		if _, ok := pos[c.Candidate]; !ok {
			pos[c.Candidate] = len(out)
			out = append(out, c.Candidate)
		}
	}
	sort.SliceStable(out, func(a, b int) bool { return family(out[a]) < family(out[b]) })
	return out
}

// family orders a name by its family (raw, F0 to F5) before its variant.
func family(name string) string {
	if name == "raw" {
		return ""
	}
	return name[:2]
}

func stat(s labfootprint.Stat, prec int) string {
	if s.N == 0 {
		return "n/a"
	}
	if s.N == 1 || s.Min == s.Max {
		return fmt.Sprintf("%.*f", prec, s.Median)
	}
	return fmt.Sprintf("%.*f (%.*f to %.*f)", prec, s.Median, prec, s.Min, prec, s.Max)
}

func exact(err string) string {
	if err == "" {
		return "yes"
	}
	return "**no**: " + strings.ReplaceAll(strings.SplitN(err, "\n", 2)[0], "|", `\|`)
}
