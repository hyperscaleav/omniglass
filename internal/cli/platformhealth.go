package cli

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/hyperscaleav/omniglass/internal/labfootprint"
	"github.com/hyperscaleav/omniglass/internal/redact"
)

// `omniglass platform health` (#2055) reads Omniglass's own health from the
// API alone: the platform view the console's Platform health page draws,
// reduced to the figures that say whether the platform is keeping up, with a
// nonzero exit when it is not. It needs only a server URL and a token, so it
// is the troubleshooting read for any deployment, where `lab measure` drives
// a lab it started.

// platformViewPath is the read behind the command.
const platformViewPath = "/api/v1/views/platform"

// platformView is the part of the platform view the command reads.
type platformView struct {
	ReadAt      time.Time             `json:"read_at"`
	FiguresRead bool                  `json:"figures_read"`
	System      *platformViewSystem   `json:"system,omitempty"`
	Servers     []platformViewServer  `json:"servers"`
	Outposts    []platformViewOutpost `json:"outposts"`
	Pipeline    *platformViewPipeline `json:"pipeline,omitempty"`
}

type platformViewSystem struct {
	Name    string `json:"name"`
	Verdict string `json:"verdict"`
}

type platformViewServer struct {
	Name      string              `json:"name"`
	Version   string              `json:"version,omitempty"`
	StartedAt *time.Time          `json:"started_at,omitempty"`
	Verdict   string              `json:"verdict"`
	Groups    []platformViewPanel `json:"groups"`
	Alarms    []platformViewAlarm `json:"alarms"`
}

type platformViewPanel struct {
	Name    string               `json:"name"`
	Keys    []platformViewKey    `json:"keys"`
	Figures []platformViewFigure `json:"figures"`
}

type platformViewKey struct {
	Name  string `json:"name"`
	Label string `json:"label"`
	Unit  string `json:"unit,omitempty"`
}

type platformViewFigure struct {
	Key        string    `json:"key"`
	Instance   string    `json:"instance"`
	Value      float64   `json:"value"`
	ObservedAt time.Time `json:"observed_at"`
}

type platformViewAlarm struct {
	Rule     string `json:"rule,omitempty"`
	Severity string `json:"severity"`
	Message  string `json:"message"`
}

type platformViewOutpost struct {
	Name    string                               `json:"name"`
	Verdict string                               `json:"verdict,omitempty"`
	Figures map[string]platformViewOutpostFigure `json:"figures,omitempty"`
	Loss    *float64                             `json:"loss,omitempty"`
}

type platformViewOutpostFigure struct {
	Value float64 `json:"value"`
}

type platformViewPipeline struct {
	Hops          []platformViewHop   `json:"hops"`
	EndToEndTotal platformViewEndLoss `json:"end_to_end_total"`
}

type platformViewHop struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Total struct {
		Newest   platformHopSpan `json:"newest"`
		LastHour platformHopSpan `json:"last_hour"`
	} `json:"total"`
}

// platformHopSpan is a hop's work over a span: what came in, what went on,
// and the share lost, absent when nothing came in.
type platformHopSpan struct {
	In   uint64   `json:"in"`
	Out  uint64   `json:"out"`
	Loss *float64 `json:"loss,omitempty"`
}

type platformViewEndLoss struct {
	Newest   *float64 `json:"newest,omitempty"`
	LastHour *float64 `json:"last_hour,omitempty"`
}

func decodePlatformView(body []byte) (platformView, error) {
	var v platformView
	if err := json.Unmarshal(body, &v); err != nil {
		return platformView{}, fmt.Errorf("read the platform view: %w", err)
	}
	return v, nil
}

// platformHeadlineKeys are the server figures the reading carries, in the
// order it prints them: whether ingest keeps up, what is queued behind it,
// and how the database is holding.
var platformHeadlineKeys = []string{
	"ingest-lag", "ingest-lag-max", "ingest-lane-busy", "db-pool-wait-count",
	"bus-consumer-pending", "bus-consumer-ack-pending", "calc-pending-depth", "outbox-queued-depth",
	"db-size", "retention-behind", "retention-pass-failure-count", "metric-oldest-day-age",
}

// platformFailureWords mark a figure that counts something going wrong: a
// key with one of them among its words is a failure figure, worth naming
// whenever it is above zero.
var platformFailureWords = map[string]bool{"failure": true, "refusal": true, "slow": true, "expired": true}

// platformReading is one reading of the platform's health: what the command
// prints, and each line of a sampling run.
type platformReading struct {
	ReadAt     time.Time                       `json:"read_at"`
	Healthy    bool                            `json:"healthy"`
	Reasons    []string                        `json:"reasons"`
	NotVisible []string                        `json:"not_visible"`
	System     *platformViewSystem             `json:"system,omitempty"`
	Servers    []platformServerReading         `json:"servers"`
	Failures   []platformFailure               `json:"failures"`
	Outposts   platformOutpostReading          `json:"outposts"`
	Pipeline   *platformPipelineReading        `json:"pipeline,omitempty"`
	Window     *platformWindow                 `json:"window,omitempty"`
	Page       *labfootprint.SystemPageFigures `json:"page,omitempty"`
}

type platformServerReading struct {
	Name          string              `json:"name"`
	Verdict       string              `json:"verdict"`
	Version       string              `json:"version,omitempty"`
	StartedAt     *time.Time          `json:"started_at,omitempty"`
	UptimeSeconds *float64            `json:"uptime_seconds,omitempty"`
	Alarms        []platformViewAlarm `json:"alarms"`
	// Backlog is the telemetry stream's head less its ack floor: the items
	// published that ingest has not yet acknowledged.
	Backlog *float64              `json:"backlog,omitempty"`
	Figures []platformFigureValue `json:"figures"`
}

type platformFigureValue struct {
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Instance string  `json:"instance,omitempty"`
	Value    float64 `json:"value"`
	Unit     string  `json:"unit,omitempty"`
}

// platformFailure is a nonzero failure figure and where it was read: a
// server's name or an outpost's.
type platformFailure struct {
	Source   string  `json:"source"`
	Key      string  `json:"key"`
	Instance string  `json:"instance,omitempty"`
	Value    float64 `json:"value"`
}

type platformOutpostReading struct {
	Count      int                      `json:"count"`
	Lossy      []platformOutpostLoss    `json:"lossy"`
	NotHealthy []platformOutpostVerdict `json:"not_healthy"`
}

type platformOutpostLoss struct {
	Name string  `json:"name"`
	Loss float64 `json:"loss"`
}

type platformOutpostVerdict struct {
	Name    string `json:"name"`
	Verdict string `json:"verdict"`
}

type platformPipelineReading struct {
	EndToEnd platformViewEndLoss  `json:"end_to_end"`
	Hops     []platformHopReading `json:"hops"`
}

type platformHopReading struct {
	ID       string          `json:"id"`
	Label    string          `json:"label"`
	Newest   platformHopSpan `json:"newest"`
	LastHour platformHopSpan `json:"last_hour"`
}

// readPlatformHealth reduces a platform view to its reading. The platform is
// healthy when every verdict it can see reads healthy, no alarm is open, and
// the newest windows lost nothing end to end; a part the caller cannot see is
// named, never read as zero, and a health it cannot see is not healthy.
func readPlatformHealth(v platformView) platformReading {
	r := platformReading{
		ReadAt:     v.ReadAt,
		Reasons:    []string{},
		NotVisible: []string{},
		System:     v.System,
		Servers:    []platformServerReading{},
		Failures:   []platformFailure{},
		Outposts:   platformOutpostReading{Count: len(v.Outposts), Lossy: []platformOutpostLoss{}, NotHealthy: []platformOutpostVerdict{}},
	}
	if v.System == nil {
		r.NotVisible = append(r.NotVisible, "system verdict")
		r.Reasons = append(r.Reasons, "the platform system's verdict is not visible to you")
	} else if v.System.Verdict != "healthy" {
		r.Reasons = append(r.Reasons, fmt.Sprintf("the platform system reads %s", v.System.Verdict))
	}
	if !v.FiguresRead {
		if len(v.Servers) == 0 {
			r.NotVisible = append(r.NotVisible, "servers")
		}
		if v.Pipeline == nil {
			r.NotVisible = append(r.NotVisible, "pipeline")
		}
		r.NotVisible = append(r.NotVisible, "outpost figures")
		r.Reasons = append(r.Reasons, "the platform's figures are not visible to you")
	}
	for _, s := range v.Servers {
		r.Servers = append(r.Servers, readPlatformServer(s, v.ReadAt))
		if s.Verdict != "healthy" {
			r.Reasons = append(r.Reasons, fmt.Sprintf("server %s reads %s", s.Name, s.Verdict))
		}
		for _, a := range s.Alarms {
			what := a.Rule
			if what == "" {
				what = a.Message
			}
			r.Reasons = append(r.Reasons, fmt.Sprintf("alarm open on %s: %s (%s)", s.Name, what, a.Severity))
		}
		for _, p := range s.Groups {
			for _, f := range p.Figures {
				if f.Value > 0 && platformFailureKey(f.Key) {
					r.Failures = append(r.Failures, platformFailure{Source: s.Name, Key: f.Key, Instance: f.Instance, Value: f.Value})
				}
			}
		}
	}
	for _, n := range v.Outposts {
		if n.Loss != nil && *n.Loss > 0 {
			r.Outposts.Lossy = append(r.Outposts.Lossy, platformOutpostLoss{Name: n.Name, Loss: *n.Loss})
		}
		if n.Verdict != "" && n.Verdict != "healthy" {
			r.Outposts.NotHealthy = append(r.Outposts.NotHealthy, platformOutpostVerdict{Name: n.Name, Verdict: n.Verdict})
			r.Reasons = append(r.Reasons, fmt.Sprintf("outpost %s reads %s", n.Name, n.Verdict))
		}
		keys := make([]string, 0, len(n.Figures))
		for k := range n.Figures {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			if f := n.Figures[k]; f.Value > 0 && platformFailureKey(k) {
				r.Failures = append(r.Failures, platformFailure{Source: n.Name, Key: k, Value: f.Value})
			}
		}
	}
	sort.SliceStable(r.Outposts.Lossy, func(i, j int) bool { return r.Outposts.Lossy[i].Loss > r.Outposts.Lossy[j].Loss })
	if p := v.Pipeline; p != nil {
		pr := &platformPipelineReading{EndToEnd: p.EndToEndTotal, Hops: []platformHopReading{}}
		for _, h := range p.Hops {
			pr.Hops = append(pr.Hops, platformHopReading{ID: h.ID, Label: h.Label, Newest: h.Total.Newest, LastHour: h.Total.LastHour})
		}
		r.Pipeline = pr
		if l := p.EndToEndTotal.Newest; l != nil && *l > 0 {
			r.Reasons = append(r.Reasons, fmt.Sprintf("the newest windows lost %s of their work end to end", formatPlatformNumber(*l)))
		}
	}
	r.Healthy = len(r.Reasons) == 0
	return r
}

// readPlatformServer is one server's part of the reading: its identity, its
// alarms, its headline figures at every instance, and its telemetry backlog.
func readPlatformServer(s platformViewServer, readAt time.Time) platformServerReading {
	out := platformServerReading{
		Name: s.Name, Verdict: s.Verdict, Version: s.Version, StartedAt: s.StartedAt,
		Alarms: s.Alarms, Figures: []platformFigureValue{},
	}
	if out.Alarms == nil {
		out.Alarms = []platformViewAlarm{}
	}
	if s.StartedAt != nil && !readAt.IsZero() {
		up := readAt.Sub(*s.StartedAt).Seconds()
		out.UptimeSeconds = &up
	}
	keys := map[string]platformViewKey{}
	for _, p := range s.Groups {
		for _, k := range p.Keys {
			keys[k.Name] = k
		}
	}
	for _, key := range platformHeadlineKeys {
		for _, f := range serverFigures(s, key) {
			k := keys[key]
			label := k.Label
			if label == "" {
				label = key
			}
			out.Figures = append(out.Figures, platformFigureValue{Key: key, Label: label, Instance: f.Instance, Value: f.Value, Unit: k.Unit})
		}
	}
	head, okHead := serverFigure(s, "bus-stream-last-seq", "")
	floor, okFloor := serverFigure(s, "bus-consumer-ack-floor", "")
	if okHead && okFloor {
		b := head.Value - floor.Value
		out.Backlog = &b
	}
	return out
}

// serverFigures is every instance of key a server's panels carry.
func serverFigures(s platformViewServer, key string) []platformViewFigure {
	var out []platformViewFigure
	for _, p := range s.Groups {
		for _, f := range p.Figures {
			if f.Key == key {
				out = append(out, f)
			}
		}
	}
	return out
}

func serverFigure(s platformViewServer, key, instance string) (platformViewFigure, bool) {
	for _, f := range serverFigures(s, key) {
		if f.Instance == instance {
			return f, true
		}
	}
	return platformViewFigure{}, false
}

func platformFailureKey(key string) bool {
	for _, w := range strings.Split(key, "-") {
		if platformFailureWords[w] {
			return true
		}
	}
	return false
}

// platformWindowKeys are the figures a window reads as growth: the database
// and its tables, and the telemetry stream's head and ack floor, whose rates
// are what ingest is asked to carry and what it does carry.
var platformWindowKeys = []string{"db-size", "table-bytes", "bus-stream-last-seq", "bus-consumer-ack-floor"}

// platformWindow is how the growth figures moved between two readings.
type platformWindow struct {
	Seconds float64                `json:"seconds"`
	Figures []platformWindowFigure `json:"figures"`
}

type platformWindowFigure struct {
	Server   string  `json:"server"`
	Key      string  `json:"key"`
	Label    string  `json:"label"`
	Instance string  `json:"instance,omitempty"`
	Unit     string  `json:"unit,omitempty"`
	Start    float64 `json:"start"`
	End      float64 `json:"end"`
	Delta    float64 `json:"delta"`
	// PerMinute is the delta over the time between the two samples, absent
	// when both readings saw the same sample.
	PerMinute *float64 `json:"per_minute,omitempty"`
}

// platformWindowFigures is the growth between two readings of the platform,
// matched by server, key and instance. A figure only one reading carries is
// left out.
func platformWindowFigures(start, end platformView) *platformWindow {
	w := &platformWindow{Seconds: end.ReadAt.Sub(start.ReadAt).Seconds(), Figures: []platformWindowFigure{}}
	for _, es := range end.Servers {
		var ss *platformViewServer
		for i := range start.Servers {
			if start.Servers[i].Name == es.Name {
				ss = &start.Servers[i]
			}
		}
		if ss == nil {
			continue
		}
		keys := map[string]platformViewKey{}
		for _, p := range es.Groups {
			for _, k := range p.Keys {
				keys[k.Name] = k
			}
		}
		for _, key := range platformWindowKeys {
			for _, ef := range serverFigures(es, key) {
				sf, ok := serverFigure(*ss, key, ef.Instance)
				if !ok {
					continue
				}
				label := keys[key].Label
				if label == "" {
					label = key
				}
				f := platformWindowFigure{
					Server: es.Name, Key: key, Label: label, Instance: ef.Instance, Unit: keys[key].Unit,
					Start: sf.Value, End: ef.Value, Delta: ef.Value - sf.Value,
				}
				if span := ef.ObservedAt.Sub(sf.ObservedAt).Minutes(); span > 0 {
					rate := f.Delta / span
					f.PerMinute = &rate
				}
				w.Figures = append(w.Figures, f)
			}
		}
	}
	return w
}

// platformHealthOpts are `platform health`'s flags.
type platformHealthOpts struct {
	json        bool
	window      time.Duration
	every       time.Duration
	appendPath  string
	samples     int
	pageClients int
	pageLoads   int
	pageSystem  string
}

func platformHealthOptsFrom(cmd *cobra.Command) platformHealthOpts {
	f := cmd.Flags()
	var o platformHealthOpts
	o.json, _ = f.GetBool("json")
	o.window, _ = f.GetDuration("window")
	o.every, _ = f.GetDuration("every")
	o.appendPath, _ = f.GetString("append")
	o.samples, _ = f.GetInt("samples")
	o.pageClients, _ = f.GetInt("page-clients")
	o.pageLoads, _ = f.GetInt("page-loads")
	o.pageSystem, _ = f.GetString("page-system")
	return o
}

func (o platformHealthOpts) check() error {
	switch {
	case o.window < 0:
		return fmt.Errorf("--window %s: want 0 for one reading, or the span to read deltas and rates over", o.window)
	case o.every < 0:
		return fmt.Errorf("--every %s: want 0 for one reading, or the time between samples", o.every)
	case o.appendPath != "" && o.every == 0:
		return errors.New("--append writes one line a sample: add --every to sample on an interval, or use --json for one reading")
	case o.samples < 0:
		return fmt.Errorf("--samples %d: want the number of samples, or 0 to sample until interrupted", o.samples)
	case o.samples != 0 && o.every == 0:
		return fmt.Errorf("--samples %d counts a sampling run's samples: add --every to sample on an interval", o.samples)
	case o.every > 0 && o.every < o.window:
		return fmt.Errorf("--every %s is shorter than --window %s: each sample reads one whole window, so give --every at least the window", o.every, o.window)
	case o.pageClients < 0:
		return fmt.Errorf("--page-clients %d: want a count of concurrent clients, or 0 to skip the system page load", o.pageClients)
	case o.pageClients > 0 && o.pageLoads < 1:
		return fmt.Errorf("--page-loads %d: want at least one timed load a client", o.pageLoads)
	case o.json && o.every > 0:
		return errors.New("--json reads once: a sampling run already writes one JSON line a sample, so drop --json")
	}
	return nil
}

func newPlatformCmd() *cobra.Command {
	platform := &cobra.Command{
		Use:   "platform",
		Short: "Read Omniglass's own health from a running server",
	}
	health := &cobra.Command{
		Use:   "health",
		Short: "Read whether the platform is keeping up, and exit nonzero when it is not",
		Long: "Reads the platform view (the read behind the console's Platform health page) and reduces it to\n" +
			"whether Omniglass is keeping up: the system and server verdicts, open alarms, each pipeline\n" +
			"hop's in, out and loss with the end to end, the outposts with loss or not healthy, ingest lag,\n" +
			"the queues, the database and retention, and every failure figure above zero. It exits nonzero\n" +
			"when a verdict is not healthy, an alarm is open, or the newest windows lost work end to end.\n\n" +
			"It reads through the API alone with --server and --token, so it works against any deployment.\n" +
			"The view is gated by outpost:read; a part the token cannot read prints as not visible to you,\n" +
			"never as zero. --window reads twice and adds the database's and the telemetry stream's growth\n" +
			"and rates; --every samples on an interval and writes one JSON line a sample, to --append or\n" +
			"stdout; --page-clients times the console's system page under concurrent clients.",
		Example: "  omniglass platform health\n" +
			"  omniglass platform health --json\n" +
			"  omniglass platform health --window 1m --page-clients 4\n" +
			"  omniglass platform health --every 5m --window 1m --append health.jsonl",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			o := platformHealthOptsFrom(cmd)
			if err := o.check(); err != nil {
				return err
			}
			server, token := connection(cmd)
			base, err := redact.ParseBaseURL(server, "--server")
			if err != nil {
				return err
			}
			c := platformClient{base: strings.TrimRight(base.String(), "/"), token: token, http: &http.Client{Timeout: 10 * time.Second}}
			if o.every > 0 {
				return samplePlatformHealth(cmd, c, o)
			}
			r, err := c.reading(cmd.Context(), o)
			if err != nil {
				return err
			}
			if o.json {
				if err := printJSON(cmd, r); err != nil {
					return err
				}
			} else {
				r.table(cmd.OutOrStdout())
			}
			if !r.Healthy {
				return fmt.Errorf("the platform is not healthy: %s", strings.Join(r.Reasons, "; "))
			}
			return nil
		},
	}
	f := health.Flags()
	f.Bool("json", false, "print the reading as JSON")
	f.Duration("window", 0, "read twice this far apart and add the growth and rates between (0 reads once)")
	f.Duration("every", 0, "sample on this interval, one JSON line a sample (0 reads once)")
	f.String("append", "", "append each sample's line to this file (with --every)")
	f.Int("samples", 0, "stop after this many samples (with --every; 0 runs until interrupted)")
	f.Int("page-clients", 0, "time the console's system page with this many concurrent clients (0 skips it)")
	f.Int("page-loads", 1, "timed system page loads a client, spread across --window")
	f.String("page-system", "", "the system whose page to load (default the fleet view's first)")
	platform.AddCommand(health)
	return platform
}

// samplePlatformHealth is a sampling run: a reading every --every, each one
// JSON line to --append or stdout. A sample that fails is an error line and
// the run goes on; only a line that cannot be written ends it.
func samplePlatformHealth(cmd *cobra.Command, c platformClient, o platformHealthOpts) error {
	out := cmd.OutOrStdout()
	if o.appendPath != "" {
		f, err := labfootprint.OpenSampleLog(o.appendPath)
		if err != nil {
			return err
		}
		defer f.Close()
		out = f
	}
	stop, release := signal.NotifyContext(cmd.Context(), os.Interrupt, syscall.SIGTERM)
	defer release()
	go func() {
		<-stop.Done()
		release()
	}()
	sched := labfootprint.SampleSchedule{Every: o.every, Samples: o.samples}
	err := sched.Run(stop, func(ctx context.Context, seq int) error {
		s := platformHealthSample{Seq: seq}
		r, err := c.reading(ctx, o)
		s.At = time.Now().UTC()
		if err != nil {
			s.Error = err.Error()
		} else {
			s.platformReading = &r
		}
		line, err := json.Marshal(s)
		if err != nil {
			return fmt.Errorf("sample %d: %w", seq, err)
		}
		if _, err := out.Write(append(line, '\n')); err != nil {
			return fmt.Errorf("write sample %d: %w", seq, err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	if o.appendPath != "" {
		fmt.Fprintf(cmd.ErrOrStderr(), "appended to %s\n", o.appendPath)
	}
	return nil
}

// platformHealthSample is one line of a sampling run: its number, when it
// was taken, and its reading or the error that took its place.
type platformHealthSample struct {
	Seq   int       `json:"seq"`
	At    time.Time `json:"at"`
	Error string    `json:"error,omitempty"`
	*platformReading
}

// UnmarshalJSON reads a sample line back: encoding/json cannot fill an
// embedded pointer to an unexported type, so the reading is read on its own,
// and only for a line that carries one.
func (s *platformHealthSample) UnmarshalJSON(b []byte) error {
	var head struct {
		Seq   int             `json:"seq"`
		At    time.Time       `json:"at"`
		Error string          `json:"error,omitempty"`
		Read  json.RawMessage `json:"read_at"`
	}
	if err := json.Unmarshal(b, &head); err != nil {
		return err
	}
	*s = platformHealthSample{Seq: head.Seq, At: head.At, Error: head.Error}
	if head.Read == nil {
		return nil
	}
	s.platformReading = &platformReading{}
	return json.Unmarshal(b, s.platformReading)
}

// platformClient reads the API as the command's caller.
type platformClient struct {
	base  string
	token string
	http  *http.Client
}

// fetch is a labfootprint.PageFetch with the caller's token.
func (c platformClient) fetch(ctx context.Context, url string) (int, string, []byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return 0, "", nil, err
	}
	if c.token != "" {
		req.Header.Set("Authorization", "Bearer "+c.token)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return 0, "", nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	return resp.StatusCode, resp.Status, body, err
}

// get reads path and refuses any answer but 200, naming the path, the
// status and the server's reason.
func (c platformClient) get(ctx context.Context, path string) ([]byte, error) {
	code, status, body, err := c.fetch(ctx, c.base+path)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	if code != http.StatusOK {
		if len(body) > 512 {
			body = body[:512]
		}
		return nil, fmt.Errorf("GET %s: %s: %s", path, status, strings.TrimSpace(string(body)))
	}
	return body, nil
}

func (c platformClient) view(ctx context.Context) (platformView, error) {
	body, err := c.get(ctx, platformViewPath)
	if err != nil {
		return platformView{}, err
	}
	return decodePlatformView(body)
}

// reading is one reading: the view, read again after --window with the
// growth between, and the system page timed across the window when asked.
func (c platformClient) reading(ctx context.Context, o platformHealthOpts) (platformReading, error) {
	var pages func() labfootprint.PageRun
	if o.pageClients > 0 {
		fleet, err := c.get(ctx, "/api/v1/views/fleet")
		if err != nil {
			return platformReading{}, err
		}
		system, err := labfootprint.PickPageSystem(fleet, o.pageSystem)
		if err != nil {
			return platformReading{}, err
		}
		gap := o.window / time.Duration(o.pageLoads)
		done := make(chan labfootprint.PageRun, 1)
		go func() {
			done <- labfootprint.RunPageClients(ctx, o.pageClients, o.pageLoads, gap, func(ctx context.Context) labfootprint.PageLoad {
				return labfootprint.LoadSystemPage(ctx, c.fetch, c.base, system)
			})
		}()
		pages = func() labfootprint.PageRun { return <-done }
	}
	start, err := c.view(ctx)
	if err != nil {
		return platformReading{}, err
	}
	end := start
	if o.window > 0 {
		if err := waitFor(ctx, o.window); err != nil {
			return platformReading{}, err
		}
		if end, err = c.view(ctx); err != nil {
			return platformReading{}, err
		}
	}
	r := readPlatformHealth(end)
	if o.window > 0 {
		r.Window = platformWindowFigures(start, end)
	}
	if pages != nil {
		r.Page = labfootprint.SummarizePage(pages())
	}
	return r, nil
}

// waitFor returns after d, or with ctx's error when ctx ends first.
func waitFor(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// platformNotVisible is what a part the caller cannot read prints as.
const platformNotVisible = "not visible to you"

// platformTableRows caps the outpost lists in the table; --json carries them all.
const platformTableRows = 10

// table prints the reading for a person.
func (r platformReading) table(w io.Writer) {
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	defer tw.Flush()
	system := platformNotVisible
	if r.System != nil {
		system = r.System.Name + "  " + r.System.Verdict
	}
	fmt.Fprintf(tw, "platform\t%s\n", system)
	fmt.Fprintf(tw, "read at\t%s\n", r.ReadAt.UTC().Format(time.RFC3339))

	fmt.Fprintln(tw, "\nSERVER\tVERDICT\tVERSION\tUP")
	if len(r.Servers) == 0 {
		fmt.Fprintf(tw, "%s\n", r.hidden("servers", "none"))
	}
	for _, s := range r.Servers {
		up := "-"
		if s.UptimeSeconds != nil {
			up = (time.Duration(*s.UptimeSeconds) * time.Second).String()
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Name, s.Verdict, orDash(s.Version), up)
	}
	for _, s := range r.Servers {
		fmt.Fprintf(tw, "\n%s\n", s.Name)
		for _, f := range s.Figures {
			label := f.Label
			if f.Instance != "" {
				label += " (" + f.Instance + ")"
			}
			fmt.Fprintf(tw, "  %s\t%s\n", label, formatPlatformValue(f.Value, f.Unit))
		}
		if s.Backlog != nil {
			fmt.Fprintf(tw, "  Telemetry Unacked\t%s\n", formatPlatformValue(*s.Backlog, ""))
		}
	}

	fmt.Fprintln(tw, "\nALARMS")
	alarms := 0
	for _, s := range r.Servers {
		for _, a := range s.Alarms {
			alarms++
			fmt.Fprintf(tw, "  %s\t%s\t%s\t%s\n", s.Name, orDash(a.Rule), a.Severity, a.Message)
		}
	}
	if alarms == 0 {
		fmt.Fprintf(tw, "  %s\n", r.hidden("servers", "none open"))
	}

	fmt.Fprintln(tw, "\nPIPELINE\tNEWEST IN\tOUT\tLOSS\tLAST HOUR IN\tOUT\tLOSS")
	if r.Pipeline == nil {
		fmt.Fprintf(tw, "  %s\n", r.hidden("pipeline", "none"))
	} else {
		for _, h := range r.Pipeline.Hops {
			fmt.Fprintf(tw, "  %s\t%d\t%d\t%s\t%d\t%d\t%s\n", h.Label,
				h.Newest.In, h.Newest.Out, formatLoss(h.Newest.Loss),
				h.LastHour.In, h.LastHour.Out, formatLoss(h.LastHour.Loss))
		}
		fmt.Fprintf(tw, "  end to end\t\t\t%s\t\t\t%s\n", formatLoss(r.Pipeline.EndToEnd.Newest), formatLoss(r.Pipeline.EndToEnd.LastHour))
	}

	fmt.Fprintf(tw, "\nOUTPOSTS\t%d, %d with loss, %d not healthy\n", r.Outposts.Count, len(r.Outposts.Lossy), len(r.Outposts.NotHealthy))
	for i, n := range r.Outposts.Lossy {
		if i == platformTableRows {
			fmt.Fprintf(tw, "  and %d more with loss\n", len(r.Outposts.Lossy)-i)
			break
		}
		fmt.Fprintf(tw, "  %s\tloss %s\n", n.Name, formatPlatformNumber(n.Loss))
	}
	for i, n := range r.Outposts.NotHealthy {
		if i == platformTableRows {
			fmt.Fprintf(tw, "  and %d more not healthy\n", len(r.Outposts.NotHealthy)-i)
			break
		}
		fmt.Fprintf(tw, "  %s\t%s\n", n.Name, n.Verdict)
	}

	fmt.Fprintln(tw, "\nFAILURES")
	if len(r.Failures) == 0 {
		fmt.Fprintf(tw, "  %s\n", r.hidden("outpost figures", "none"))
	}
	for _, f := range r.Failures {
		key := f.Key
		if f.Instance != "" {
			key += " (" + f.Instance + ")"
		}
		fmt.Fprintf(tw, "  %s\t%s\t%s\n", f.Source, key, formatPlatformNumber(f.Value))
	}

	if r.Window != nil {
		fmt.Fprintf(tw, "\nWINDOW\t%s\n", (time.Duration(r.Window.Seconds * float64(time.Second))).Round(time.Second))
		for _, f := range r.Window.Figures {
			label := f.Label
			if f.Instance != "" {
				label += " (" + f.Instance + ")"
			}
			rate := "-"
			if f.PerMinute != nil {
				rate = formatPlatformValue(*f.PerMinute, f.Unit) + " a minute"
			}
			fmt.Fprintf(tw, "  %s %s\t%s\t%s\n", f.Server, label, formatPlatformValue(f.Delta, f.Unit), rate)
		}
	}
	if r.Page != nil {
		fmt.Fprintf(tw, "\nSYSTEM PAGE\tp50 %s ms  p95 %s ms  p99 %s ms  (%d loads, %d clients, %d failed)\n",
			formatPlatformNumber(r.Page.Ms.P50), formatPlatformNumber(r.Page.Ms.P95), formatPlatformNumber(r.Page.Ms.P99),
			r.Page.Ms.N, r.Page.Clients, r.Page.Errors)
	}

	if r.Healthy {
		fmt.Fprintln(tw, "\nhealthy")
		return
	}
	fmt.Fprintln(tw, "\nnot healthy")
	for _, reason := range r.Reasons {
		fmt.Fprintf(tw, "  %s\n", reason)
	}
}

// hidden is what an empty section prints: not visible to you when the
// caller could not read it, otherwise empty.
func (r platformReading) hidden(part, empty string) string {
	for _, p := range r.NotVisible {
		if p == part {
			return platformNotVisible
		}
	}
	return empty
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}

func formatLoss(l *float64) string {
	if l == nil {
		return "-"
	}
	return formatPlatformNumber(*l)
}

// formatPlatformValue prints a figure in its unit: bytes in IEC units, a
// ratio or a count bare, any other unit after the number.
func formatPlatformValue(v float64, unit string) string {
	switch unit {
	case "bytes":
		const kib, mib, gib = 1 << 10, 1 << 20, 1 << 30
		a := math.Abs(v)
		switch {
		case a >= gib:
			return fmt.Sprintf("%.2f GiB", v/gib)
		case a >= mib:
			return fmt.Sprintf("%.0f MiB", v/mib)
		case a >= kib:
			return fmt.Sprintf("%.0f KiB", v/kib)
		}
		return fmt.Sprintf("%.0f B", v)
	case "", "none", "ratio":
		return formatPlatformNumber(v)
	}
	return formatPlatformNumber(v) + " " + unit
}

// formatPlatformNumber prints a whole number whole and any other to three
// decimal places at most.
func formatPlatformNumber(v float64) string {
	if v == math.Trunc(v) {
		return strconv.FormatFloat(v, 'f', 0, 64)
	}
	return strconv.FormatFloat(math.Round(v*1000)/1000, 'f', -1, 64)
}
