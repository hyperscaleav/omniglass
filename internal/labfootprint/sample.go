package labfootprint

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"time"
)

// ScaleSample is one line of a sampling run's time series (#1632): the
// one-shot report's figures at the same JSON paths as `lab measure --json`
// writes them, with the sample's place in the run. A failed sample carries
// its error and no figures.
type ScaleSample struct {
	// Seq numbers the run's samples from 1.
	Seq int `json:"seq"`
	// At is when the sample's window closed, or when it failed.
	At time.Time `json:"at"`
	// UptimeS is how long the server had been running when the window
	// closed: a series whose uptime falls was measured across a restart.
	UptimeS *float64 `json:"uptime_seconds,omitempty"`
	Error   string   `json:"error,omitempty"`
	*ScaleReport
}

// Line is the sample as one line of compact JSON with its newline.
func (s ScaleSample) Line() ([]byte, error) {
	b, err := json.Marshal(s)
	if err != nil {
		return nil, err
	}
	return append(b, '\n'), nil
}

// SampleResult is one measured sample.
type SampleResult struct {
	Report  ScaleReport
	End     time.Time
	UptimeS float64
}

// SampleFunc measures sample seq.
type SampleFunc func(ctx context.Context, seq int) (SampleResult, error)

// SampleLoop is a sampling run: a sample every Every, Samples of them (0
// runs until stopped), each written to Out as a line.
type SampleLoop struct {
	Every   time.Duration
	Samples int
	Measure SampleFunc
	Out     io.Writer
	// wait is sleep unless a test replaces it.
	wait func(context.Context, time.Duration) error
}

// RunSamples runs the loop. Sample k starts Every after sample k-1 started,
// or at once when k-1 overran. A failed sample is written as an error line
// and the run goes on. Cancelling stop ends the run between samples: the
// sample in flight is measured to its end and written, since a cut-off
// window is no figure. Only a write that fails ends the run with an error.
func RunSamples(stop context.Context, l SampleLoop) error {
	wait := l.wait
	if wait == nil {
		wait = sleep
	}
	measureCtx := context.WithoutCancel(stop)
	start := time.Now()
	for seq := 1; l.Samples == 0 || seq <= l.Samples; seq++ {
		if seq > 1 {
			due := start.Add(time.Duration(seq-1) * l.Every)
			if err := wait(stop, time.Until(due)); err != nil {
				return nil
			}
		}
		if stop.Err() != nil {
			return nil
		}
		s := ScaleSample{Seq: seq}
		res, err := l.Measure(measureCtx, seq)
		if err != nil {
			s.At, s.Error = time.Now().UTC(), err.Error()
		} else {
			up := res.UptimeS
			s.At, s.UptimeS, s.ScaleReport = res.End.UTC(), &up, &res.Report
		}
		line, err := s.Line()
		if err != nil {
			return fmt.Errorf("sample %d: %w", seq, err)
		}
		if _, err := l.Out.Write(line); err != nil {
			return fmt.Errorf("write sample %d: %w", seq, err)
		}
	}
	return nil
}

// OpenSampleLog opens path for a run's lines: created when missing, and
// appended to, so an earlier run's lines stay as they were. Each sample is
// one write of one line.
func OpenSampleLog(path string) (*os.File, error) {
	return os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
}
