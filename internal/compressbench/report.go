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
	// SummaryShare is the share of the bytes the summary columns hold, and
	// CompressedRows the share of block rows whose samples Postgres
	// compressed; zero N for a candidate that is not a block table.
	SummaryShare   labfootprint.Stat `json:"summary_share"`
	CompressedRows labfootprint.Stat `json:"compressed_rows"`
	// MergeMs and MergeWAL are the time and WAL bytes of landing one late
	// sample in a closed span.
	MergeMs  labfootprint.Stat `json:"late_merge_ms"`
	MergeWAL labfootprint.Stat `json:"late_merge_wal_bytes"`
	// SpanDays is the series-days stored at each span, and DaySpans each
	// one, for a candidate that reports them (DaySpans only for a dataset
	// whose cadence changes).
	SpanDays map[string]int `json:"span_days,omitempty"`
	DaySpans []DaySpan      `json:"day_spans,omitempty"`
	// Error is why the candidate did not give back what it was given, or did
	// not run; empty when every read and the round trip were exact.
	Error string `json:"error,omitempty"`
}

// Summarize is the cell for ms, the runs of one candidate at one cadence.
func Summarize(candidate string, cadence time.Duration, ms []Measure, compressed bool) Cell {
	c := Cell{Candidate: candidate, Cadence: CadenceLabel(cadence)}
	var bps, comp []float64
	var raw, bk, merge []time.Duration
	var share, rows, wal []float64
	for _, m := range ms {
		if m.Summarized {
			share = append(share, float64(m.SummaryBytes)/float64(m.Bytes))
		}
		if m.Blocks {
			rows = append(rows, m.CompressedRows)
		}
		if m.Merged {
			merge = append(merge, m.Merge)
			wal = append(wal, float64(m.MergeWAL))
		}
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
	c.SummaryShare = labfootprint.StatOf(share)
	c.CompressedRows = labfootprint.StatOf(rows)
	c.MergeMs = labfootprint.LatencyStat(merge)
	c.MergeWAL = labfootprint.StatOf(wal)
	return c
}

// CadenceLabel is a cadence as the tickets write it: 10s, 60s, 5m, 1h, 24h.
func CadenceLabel(d time.Duration) string {
	switch {
	case d >= time.Hour && d%time.Hour == 0:
		return fmt.Sprintf("%dh", d/time.Hour)
	case d > time.Minute && d%time.Minute == 0:
		return fmt.Sprintf("%dm", d/time.Minute)
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
		b.WriteString("| candidate | samples | B/sample | summary share | rows compressed | compress ms/1k | 24 h raw read ms | 30 d bucketed read ms | late merge ms | late merge WAL B | exact |\n")
		b.WriteString("|---|--:|--:|--:|--:|--:|--:|--:|--:|--:|---|\n")
		for _, c := range r.Cells {
			if c.Cadence != cad {
				continue
			}
			fmt.Fprintf(&b, "| %s | %d | %s | %s | %s | %s | %s | %s | %s | %s | %s |\n", c.Candidate, c.Samples,
				stat(c.BytesPerSample, 2), pct(c.SummaryShare), pct(c.CompressedRows), stat(c.CompressPer1k, 2),
				stat(c.ReadRaw, 2), stat(c.ReadBuckets, 2), stat(c.MergeMs, 2), stat(c.MergeWAL, 0), exact(c.Error))
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

// pct is a share as a percentage, its range only when the runs differ.
func pct(s labfootprint.Stat) string {
	if s.N == 0 {
		return "n/a"
	}
	if s.N == 1 || s.Min == s.Max {
		return fmt.Sprintf("%.1f%%", s.Median*100)
	}
	return fmt.Sprintf("%.1f%% (%.1f%% to %.1f%%)", s.Median*100, s.Min*100, s.Max*100)
}

// Exact is a cell's error as the tables write it: yes, or why not.
func Exact(err string) string { return exact(err) }

func exact(err string) string {
	if err == "" {
		return "yes"
	}
	return "**no**: " + strings.ReplaceAll(strings.SplitN(err, "\n", 2)[0], "|", `\|`)
}

// spans is every block span, shortest first.
var spans = []string{"hour", "day", "week", "month"}

// SpanChoice is the span the rule picks for one family at one cadence:
// a block span, or "raw" when no block beats the narrow row.
type SpanChoice struct {
	Cadence           string
	Span              string
	BytesPerSample    float64
	RawBytesPerSample float64
}

// compressAt is the share of block rows that must compress for a span to pass
// the threshold: most of them.
const compressAt = 0.5

// SpanRule is, for each cadence in r's specs, the smallest span of family (F0
// or F2) whose rows mostly cross Postgres's compression threshold. When no
// span's rows do, it is the span with the fewest bytes a sample, and raw when
// even that is no smaller than the narrow row.
func SpanRule(r Report, family string) []SpanChoice {
	var out []SpanChoice
	for _, spec := range r.Specs {
		cad := spec.Label()
		raw, ok := r.cell("raw", cad)
		if !ok || raw.BytesPerSample.N == 0 {
			continue
		}
		choice := SpanChoice{Cadence: cad, Span: "raw", BytesPerSample: raw.BytesPerSample.Median, RawBytesPerSample: raw.BytesPerSample.Median}
		var best *SpanChoice
		for _, span := range spans {
			name := family
			if span != "hour" {
				name += "/" + span
			}
			c, ok := r.cell(name, cad)
			if !ok || c.BytesPerSample.N == 0 || c.Error != "" {
				continue
			}
			sc := SpanChoice{Cadence: cad, Span: span, BytesPerSample: c.BytesPerSample.Median, RawBytesPerSample: choice.RawBytesPerSample}
			if c.CompressedRows.N > 0 && c.CompressedRows.Median >= compressAt {
				best = &sc
				break
			}
			if best == nil || sc.BytesPerSample < best.BytesPerSample {
				best = &sc
			}
		}
		if best != nil && best.BytesPerSample < choice.RawBytesPerSample {
			choice = *best
		}
		out = append(out, choice)
	}
	return out
}

// RawCutoff is the fastest cadence whose choice is raw, or "" when every
// cadence takes a block.
func RawCutoff(choices []SpanChoice) string {
	for _, c := range choices {
		if c.Span == "raw" {
			return c.Cadence
		}
	}
	return ""
}

// RuleTable is the span rule as markdown, one table a block family.
func (r Report) RuleTable() string {
	var b strings.Builder
	for _, fam := range []string{"F0", "F2"} {
		choices := SpanRule(r, fam)
		if len(choices) == 0 {
			continue
		}
		fmt.Fprintf(&b, "### %s span rule\n\n| cadence | span | B/sample | raw B/sample |\n|---|---|--:|--:|\n", fam)
		for _, c := range choices {
			fmt.Fprintf(&b, "| %s | %s | %.2f | %.2f |\n", c.Cadence, c.Span, c.BytesPerSample, c.RawBytesPerSample)
		}
		if cut := RawCutoff(choices); cut != "" {
			fmt.Fprintf(&b, "\nA series at %s or slower stays raw.\n\n", cut)
		} else {
			b.WriteString("\nEvery cadence measured takes a block.\n\n")
		}
	}
	return b.String()
}

func (r Report) cell(name, cadence string) (Cell, bool) {
	for _, c := range r.Cells {
		if c.Candidate == name && c.Cadence == cadence {
			return c, true
		}
	}
	return Cell{}, false
}
