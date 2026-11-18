package lab

import (
	"fmt"
	"time"
)

// The soak verdict: whether a build has run a week under the 3,000-system
// corporate soak without failing silently. The platform judges itself, so the
// verdict is the platform's own alarm history and its health page's loss
// ratios, read through its API, and not a figure an outside harness derives
// again. This file is the pure judge; soak_read.go gathers its input.
const (
	// SoakMinWindow is the least a soak may span and still pass: a week.
	SoakMinWindow = 7 * 24 * time.Hour

	// SoakAlarmTolerance is the longest a platform alarm may stay open and
	// still pass. A restart or a rollover raises and clears one in minutes;
	// one open longer than this was a condition, not a blip. It is the
	// default of `lab soak verdict --tolerance`.
	SoakAlarmTolerance = 30 * time.Minute

	// SoakLossCeiling is the highest loss ratio a pass tolerates on the
	// platform health page: none. A healthy build loses nothing at any hop.
	SoakLossCeiling = 0.0
)

// SoakAlarm is one alarm on the platform, as its alarm history serves it.
// ClearedAt is nil while the alarm is open.
type SoakAlarm struct {
	Component string
	Rule      string
	Severity  string
	Message   string
	RaisedAt  time.Time
	ClearedAt *time.Time
}

// SoakLoss is one loss ratio the platform health page shows, with where on the
// page it is read.
type SoakLoss struct {
	Where string
	Ratio float64
}

// SoakReading is everything the verdict judges: what the platform said about
// itself over the window, read through its API.
type SoakReading struct {
	// Build is the version the platform's servers run, and Since and Until
	// are the window the alarm history was read over.
	Build        string
	Since, Until time.Time
	Alarms       []SoakAlarm
	// AlarmsTruncated is set when the history read returned as many alarms as
	// the API will serve, so older ones may be missing from it.
	AlarmsTruncated bool
	// Losses is every loss ratio the page carries at the end of the window.
	Losses []SoakLoss
}

// SoakVerdict is pass or fail, and for a fail every reason, in the order the
// reading was judged.
type SoakVerdict struct {
	Pass    bool
	Reasons []string
}

// JudgeSoak judges a reading. It passes when the soak spans at least
// SoakMinWindow, no platform alarm is still open, none was open longer than
// tolerance, and no loss ratio on the page is above SoakLossCeiling. A reading
// that leaves the question unanswerable (no build, no loss ratio to read, an
// alarm history the API cut short) fails, since the soak exists to catch what
// fails silently. Every reason is named, not only the first.
func JudgeSoak(r SoakReading, tolerance time.Duration) SoakVerdict {
	var why []string
	if r.Build == "" {
		why = append(why, "the platform reports no build, so there is no build to pass")
	}
	if ran := r.Until.Sub(r.Since); ran < SoakMinWindow {
		why = append(why, fmt.Sprintf("the soak ran %s, under the %s a release needs", ran.Round(time.Minute), SoakMinWindow))
	}
	if r.AlarmsTruncated {
		why = append(why, "the alarm history reached the API's cap, so older alarms may be missing")
	}
	for _, a := range r.Alarms {
		why = append(why, judgeSoakAlarm(a, r.Since, r.Until, tolerance)...)
	}
	if len(r.Losses) == 0 {
		why = append(why, "the platform health page carries no loss ratio to read")
	}
	for _, l := range r.Losses {
		if l.Ratio > SoakLossCeiling {
			why = append(why, fmt.Sprintf("loss %.4g at %s, above %g", l.Ratio, l.Where, SoakLossCeiling))
		}
	}
	return SoakVerdict{Pass: len(why) == 0, Reasons: why}
}

// judgeSoakAlarm is the reasons one alarm fails the soak, none for a clean
// one. An alarm still open fails whenever it was raised; one cleared counts
// only when it was raised in the window, since an alarm that came and went
// before the soak began is not the soak's.
func judgeSoakAlarm(a SoakAlarm, since, until time.Time, tolerance time.Duration) []string {
	who := fmt.Sprintf("%s alarm %q on %s raised %s", a.Severity, a.Rule, a.Component, a.RaisedAt.UTC().Format(time.RFC3339))
	if a.ClearedAt == nil {
		return []string{fmt.Sprintf("%s is still open (%s)", who, until.Sub(a.RaisedAt).Round(time.Minute))}
	}
	if a.RaisedAt.Before(since) {
		return nil
	}
	if open := a.ClearedAt.Sub(a.RaisedAt); open > tolerance {
		return []string{fmt.Sprintf("%s was open for %s, over the %s tolerance", who, open.Round(time.Minute), tolerance)}
	}
	return nil
}
