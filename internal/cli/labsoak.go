package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/lab"
)

// newLabSoakCmd is `omniglass lab soak`: the release gate's side of the 3,000
// system soak. The soak itself is a kept scale lab under a service manager
// (the lab guide's soak run); this group judges it.
func newLabSoakCmd() *cobra.Command {
	group := &cobra.Command{
		Use:   "soak",
		Short: "Judge a 3,000-system soak from the platform's own alarm history",
	}
	group.AddCommand(newLabSoakVerdictCmd())
	return group
}

// soakVerdictOpts are `lab soak verdict`'s flags.
type soakVerdictOpts struct {
	since     string
	until     string
	tolerance time.Duration
	recordDir string
}

// window parses --since and --until against now: --until defaults to now.
func (o soakVerdictOpts) window(now time.Time) (since, until time.Time, err error) {
	if o.since == "" {
		return since, until, errors.New("--since: the time the soak started, an RFC 3339 timestamp such as 2026-11-20T08:00:00Z")
	}
	if since, err = time.Parse(time.RFC3339, o.since); err != nil {
		return since, until, fmt.Errorf("--since %q: want an RFC 3339 timestamp such as 2026-11-20T08:00:00Z", o.since)
	}
	until = now
	if o.until != "" {
		if until, err = time.Parse(time.RFC3339, o.until); err != nil {
			return since, until, fmt.Errorf("--until %q: want an RFC 3339 timestamp, or leave it for now", o.until)
		}
	}
	if !until.After(since) {
		return since, until, fmt.Errorf("--until %s is not after --since %s", until.UTC().Format(time.RFC3339), since.UTC().Format(time.RFC3339))
	}
	if o.tolerance < 0 {
		return since, until, fmt.Errorf("--tolerance %s: want zero or more", o.tolerance)
	}
	return since, until, nil
}

func newLabSoakVerdictCmd() *cobra.Command {
	var o soakVerdictOpts
	cmd := &cobra.Command{
		Use:   "verdict",
		Short: "Print pass or fail for a soak: no platform alarm left open or open too long, every loss ratio at 0",
		Long: "Judges a soak from what the platform says about itself, and nothing else: it reads, through the API " +
			"(--server and --token, or $OMNIGLASS_SERVER and $OMNIGLASS_TOKEN), the platform system's alarm history " +
			"and the Platform health page, and prints pass or fail with every reason. The token needs read over " +
			"nodes, systems and components, all of them, since a verdict from part of the page would pass what it " +
			"could not see.\n\n" +
			"Pass: the soak ran at least 7 days (--since to --until), no platform alarm is still open, none raised in the " +
			"window was open longer than --tolerance, and every loss ratio on the page (each hop, kind and node, the " +
			"newest window and the last hour, the end to end, the worklist pull) is 0. A reading that cannot answer " +
			"fails: no build reported, no loss ratio on the page, an alarm history the API cut short, or two builds " +
			"among the servers.\n\n" +
			"With --record-dir it also writes the release record, one file named for the build, which the release " +
			"commits: " + lab.SoakRecordDir + " from the repository's root. The exit status is 1 on a fail.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			c := clientFromCmd(cmd)
			get := func(ctx context.Context, path string, into any) error {
				res, err := c.Do(ctx, "GET", path, nil)
				if err != nil {
					return err
				}
				if !res.OK() {
					return fmt.Errorf("status %d: %s", res.Status, res.Body)
				}
				return json.Unmarshal(res.Body, into)
			}
			return runSoakVerdict(cmd.Context(), cmd.OutOrStdout(), get, o, time.Now().UTC(), soakHost())
		},
	}
	f := cmd.Flags()
	f.StringVar(&o.since, "since", "", "when the soak started, an RFC 3339 timestamp; required")
	f.StringVar(&o.until, "until", "", "when the soak ended, an RFC 3339 timestamp; empty is now")
	f.DurationVar(&o.tolerance, "tolerance", lab.SoakAlarmTolerance, "the longest a platform alarm may stay open and still pass")
	f.StringVar(&o.recordDir, "record-dir", "", "also write the release record into this directory, one file named for the build ("+lab.SoakRecordDir+" is where releases keep them)")
	return cmd
}

// runSoakVerdict reads the soak through get, prints the verdict and writes the
// record when asked. A fail is an error, so the exit status carries it.
func runSoakVerdict(ctx context.Context, out io.Writer, get lab.SoakGetter, o soakVerdictOpts, now time.Time, host string) error {
	since, until, err := o.window(now)
	if err != nil {
		return err
	}
	reading, err := lab.ReadSoak(ctx, get, since, until)
	if err != nil {
		return err
	}
	verdict := lab.JudgeSoak(reading, o.tolerance)
	fmt.Fprint(out, soakVerdictText(reading, verdict, o.tolerance))
	if o.recordDir != "" {
		path := filepath.Join(o.recordDir, lab.SoakRecordFile(reading.Build))
		rec := lab.SoakRecord{Build: reading.Build, Host: host, Since: since, Until: until, Tolerance: o.tolerance, Verdict: verdict}
		if err := os.MkdirAll(o.recordDir, 0o755); err != nil {
			return err
		}
		if err := os.WriteFile(path, []byte(lab.RenderSoakRecord(rec)), 0o644); err != nil {
			return err
		}
		fmt.Fprintf(out, "wrote %s\n", path)
	}
	if !verdict.Pass {
		return fmt.Errorf("the soak of %s failed", reading.Build)
	}
	return nil
}

// soakVerdictText is the verdict as the operator reads it.
func soakVerdictText(r lab.SoakReading, v lab.SoakVerdict, tolerance time.Duration) string {
	head := "PASS"
	if !v.Pass {
		head = "FAIL"
	}
	s := fmt.Sprintf("%s  build %s, %s to %s (%s), %d platform alarms read, %d loss ratios read, alarm tolerance %s\n",
		head, r.Build, r.Since.UTC().Format(time.RFC3339), r.Until.UTC().Format(time.RFC3339),
		r.Until.Sub(r.Since).Round(time.Minute), len(r.Alarms), len(r.Losses), tolerance)
	for _, why := range v.Reasons {
		s += "  - " + why + "\n"
	}
	return s
}

// soakHost is this host's fingerprint, for the record: where the soak ran.
func soakHost() string {
	host, _ := os.Hostname()
	id, _ := os.ReadFile("/etc/machine-id")
	return lab.SoakHostFingerprint(host, runtime.GOOS, runtime.GOARCH, runtime.NumCPU(), string(id))
}
