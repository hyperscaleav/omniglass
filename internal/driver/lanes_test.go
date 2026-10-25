package driver_test

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/driver"
)

// A driver emit lands on any lane (#1145): a metric, a property, an event
// (its message and attributes) or a log line (its message, severity,
// facility, source, attributes and time). These pin the spec's write gate
// and the interpreter for the two new lanes.

// laneCatalog is catalog() with the event catalog joined and a typed event
// whose payload schema closes its fields.
func laneCatalog() fakeCatalog {
	c := catalog()
	c.lanes["input-changed"] = "event"
	c.lanes["call-started"] = "event"
	c.lanes["strict-event"] = "event"
	c.lanes["collection-failed"] = "event"
	for _, k := range []string{"command-issued", "command-settled", "command-failed", "command-timed-out"} {
		c.lanes[k] = "event"
	}
	c.schemas = map[string][]byte{
		"strict-event": []byte(`{"type":"object","properties":{"input":{"type":"string"},"count":{"type":"integer"}},"required":["input"],"additionalProperties":false}`),
	}
	return c
}

// listenerSpec is a tcp driver with one listener carrying emits.
func listenerSpec(emits ...driver.Emit) *driver.Spec {
	return &driver.Spec{
		Version:   1,
		Transport: "tcp",
		Inputs:    []driver.Input{{Name: "host", Kind: "string", Required: true}},
		Listeners: []driver.Listener{{Name: "events", Arm: []string{"SUBSCRIBE INPUT"}, Match: driver.Match{Prefix: "EVT "}, Emits: emits}},
	}
}

func rx(re string) *driver.Extract { return &driver.Extract{Regex: re} }

func TestAnEventEmitWrites(t *testing.T) {
	s := listenerSpec(driver.Emit{
		Name:       "input-changed",
		Message:    &driver.Field{Value: "Input changed"},
		Attributes: map[string]driver.Field{"input": {Extract: rx(`^EVT INPUT (\S+)$`)}},
	})
	if err := s.Validate(laneCatalog()); err != nil {
		t.Fatalf("an event emit refused: %v", err)
	}
	// The message defaults to the whole claimed line: an event emit needs no
	// extract at all.
	s = listenerSpec(driver.Emit{Name: "input-changed"})
	if err := s.Validate(laneCatalog()); err != nil {
		t.Fatalf("an event emit with the whole line as its message refused: %v", err)
	}
}

func TestReservedEventsAreRefusedNamingTheWriter(t *testing.T) {
	for _, name := range []string{"collection-failed", "command-issued", "command-settled", "command-failed", "command-timed-out"} {
		err := listenerSpec(driver.Emit{Name: name}).Validate(laneCatalog())
		if err == nil || !strings.Contains(err.Error(), name) {
			t.Fatalf("emitting %s = %v, want a refusal naming it", name, err)
		}
	}
}

func TestAnEventEmitsAttributesMeetItsSchema(t *testing.T) {
	ok := listenerSpec(driver.Emit{Name: "strict-event", Attributes: map[string]driver.Field{
		"input": {Extract: rx(`(\S+)$`)},
		"count": {Value: "1", Transform: &driver.Transform{Cast: "int"}},
	}})
	if err := ok.Validate(laneCatalog()); err != nil {
		t.Fatalf("attributes the schema allows refused: %v", err)
	}
	extra := listenerSpec(driver.Emit{Name: "strict-event", Attributes: map[string]driver.Field{
		"input": {Extract: rx(`(\S+)$`)}, "colour": {Value: "red"},
	}})
	if err := extra.Validate(laneCatalog()); err == nil || !strings.Contains(err.Error(), "colour") {
		t.Fatalf("a field the schema forbids = %v, want a refusal naming it", err)
	}
	missing := listenerSpec(driver.Emit{Name: "strict-event", Attributes: map[string]driver.Field{"count": {Value: "1"}}})
	if err := missing.Validate(laneCatalog()); err == nil || !strings.Contains(err.Error(), "input") {
		t.Fatalf("a required field left out = %v, want a refusal naming it", err)
	}
}

func TestALogEmitWrites(t *testing.T) {
	s := listenerSpec(driver.Emit{
		Log:        &driver.LogEmit{Severity: &driver.Field{Extract: rx(`^LOG (\S+)`)}, Facility: &driver.Field{Value: "panel"}, Source: "nvp"},
		Message:    &driver.Field{Extract: rx(`^LOG \S+ (.*)$`)},
		Attributes: map[string]driver.Field{"origin": {Value: "panel"}},
		TS:         &driver.TimeField{Extract: driver.Extract{Regex: `at (\S+)$`}, Layout: "rfc3339"},
	})
	if err := s.Validate(laneCatalog()); err != nil {
		t.Fatalf("a log emit refused: %v", err)
	}
	// The bare form: the whole line, no severity.
	if err := listenerSpec(driver.Emit{Log: &driver.LogEmit{}}).Validate(laneCatalog()); err != nil {
		t.Fatalf("a bare log emit refused: %v", err)
	}
}

func TestEmitShapesAreRefusedByLane(t *testing.T) {
	cases := map[string]struct {
		emit driver.Emit
		want string
	}{
		"a log emit naming a key":       {driver.Emit{Name: "video-input", Log: &driver.LogEmit{}}, "log line has no catalog name"},
		"a log emit with an extract":    {driver.Emit{Log: &driver.LogEmit{}, Extract: driver.Extract{Regex: "x"}}, "message"},
		"an event emit with an extract": {driver.Emit{Name: "input-changed", Extract: driver.Extract{Regex: "x"}}, "message"},
		"a property emit with a message": {driver.Emit{Name: "video-input", Extract: driver.Extract{Regex: "x"}, Message: &driver.Field{Value: "m"}},
			"message and attributes ride an event or a log line"},
		"a metric emit with attributes": {driver.Emit{Name: "uptime", Extract: driver.Extract{Regex: "x"}, Attributes: map[string]driver.Field{"a": {Value: "b"}}},
			"message and attributes ride an event or a log line"},
		"a property emit with no extract": {driver.Emit{Name: "video-input"}, "exactly one extract"},
		"a field with a value and an extract": {driver.Emit{Name: "input-changed", Message: &driver.Field{Value: "v", Extract: rx("x")}},
			"one of value or extract"},
		"a field with nothing":        {driver.Emit{Name: "input-changed", Attributes: map[string]driver.Field{"a": {}}}, "one of value or extract"},
		"an uncompilable field regex": {driver.Emit{Log: &driver.LogEmit{Severity: &driver.Field{Extract: rx("(")}}}, "regex"},
		"a time with an unknown layout": {driver.Emit{Name: "video-input", Extract: driver.Extract{Regex: "x"},
			TS: &driver.TimeField{Extract: driver.Extract{Regex: "x"}, Layout: "sometime"}}, "layout"},
		"a time with an unknown zone": {driver.Emit{Log: &driver.LogEmit{},
			TS: &driver.TimeField{Extract: driver.Extract{Regex: "x"}, Layout: "rfc3164", Zone: "Mars/Olympus"}}, "zone"},
		"a time with no extract":   {driver.Emit{Log: &driver.LogEmit{}, TS: &driver.TimeField{Layout: "rfc3339"}}, "exactly one extract"},
		"an attribute named oddly": {driver.Emit{Log: &driver.LogEmit{}, Attributes: map[string]driver.Field{"": {Value: "x"}}}, "attribute"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			err := listenerSpec(c.emit).Validate(laneCatalog())
			if err == nil || !strings.Contains(err.Error(), c.want) {
				t.Fatalf("Validate = %v, want an error containing %q", err, c.want)
			}
		})
	}
}

// TestEveryExtractAnEmitCarriesMeetsTheTransport: a line response carries no
// json, so a log emit whose severity reads a jsonpath is refused over tcp,
// as a metric's extract would be.
func TestEveryExtractAnEmitCarriesMeetsTheTransport(t *testing.T) {
	s := validSpec()
	s.Polls[0].Emits = append(s.Polls[0].Emits, driver.Emit{Log: &driver.LogEmit{Severity: &driver.Field{Extract: &driver.Extract{JSONPath: "level"}}}})
	if err := s.Validate(laneCatalog()); err == nil || !strings.Contains(err.Error(), "regex") {
		t.Fatalf("a jsonpath severity over tcp = %v, want it refused", err)
	}
}

func TestUnmatchedLogIsAPolicy(t *testing.T) {
	s := listenerSpec(driver.Emit{Name: "video-input", Extract: driver.Extract{Regex: `^EVT INPUT (\S+)$`}})
	s.Unmatched = driver.UnmatchedLog
	if err := s.Validate(laneCatalog()); err != nil {
		t.Fatalf("unmatched: log refused: %v", err)
	}
}

func lineFn(emits ...driver.BakedEmit) *driver.BakedFunction {
	return &driver.BakedFunction{Driver: "newtron-nvp", Function: "events", Emits: emits}
}

func TestInterpretAnEvent(t *testing.T) {
	arrival := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	fn := lineFn(driver.BakedEmit{
		Name: "input-changed", Lane: "event",
		Message: &driver.Field{Value: "Input changed"},
		Attributes: map[string]driver.Field{
			"input": {Extract: rx(`^EVT INPUT (\S+)$`)},
			"count": {Value: "3", Transform: &driver.Transform{Cast: "int"}},
		},
	})
	out, faults := fn.Interpret(driver.LineMessage("EVT INPUT hdmi2").Payload(), arrival)
	if len(faults) != 0 || len(out) != 1 {
		t.Fatalf("out %+v faults %v", out, faults)
	}
	e := out[0]
	if e.Lane != "event" || e.Name != "input-changed" || e.Text != "Input changed" || !e.TS.Equal(arrival) {
		t.Fatalf("event = %+v", e)
	}
	var payload map[string]any
	if err := json.Unmarshal(e.Attributes, &payload); err != nil || payload["input"] != "hdmi2" || payload["count"] != float64(3) {
		t.Fatalf("payload = %s, %v (want input as text, count as a number)", e.Attributes, err)
	}
	// With no message declared, the whole line is the message.
	whole := lineFn(driver.BakedEmit{Name: "input-changed", Lane: "event"})
	out, _ = whole.Interpret(driver.LineMessage("EVT INPUT hdmi2").Payload(), arrival)
	if len(out) != 1 || out[0].Text != "EVT INPUT hdmi2" || out[0].Attributes != nil {
		t.Fatalf("whole-line event = %+v", out)
	}
}

func TestInterpretALogLine(t *testing.T) {
	arrival := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	fn := lineFn(driver.BakedEmit{
		Lane: "log",
		Log: &driver.LogEmit{
			Severity: &driver.Field{Extract: rx(`^LOG (\S+)`)},
			Facility: &driver.Field{Value: "panel"},
			Source:   "nvp",
		},
		Message:    &driver.Field{Extract: rx(`^LOG \S+ (.*)$`)},
		Attributes: map[string]driver.Field{"zone": {Value: "front"}},
	})
	out, faults := fn.Interpret(driver.LineMessage("LOG WARN backlight driver reset").Payload(), arrival)
	if len(faults) != 0 || len(out) != 1 {
		t.Fatalf("out %+v faults %v", out, faults)
	}
	l := out[0]
	if l.Lane != "log" || l.Name != "" || l.Text != "backlight driver reset" || l.Severity != "warning" || l.Facility != "panel" || l.Source != "nvp" {
		t.Fatalf("log line = %+v", l)
	}
	if !strings.Contains(string(l.Attributes), `"zone":"front"`) {
		t.Fatalf("attributes = %s", l.Attributes)
	}

	// A severity the table does not know is kept, not guessed at.
	out, _ = fn.Interpret(driver.LineMessage("LOG LOUD something").Payload(), arrival)
	if out[0].Severity != "" || !strings.Contains(string(out[0].Attributes), `"severity_raw":"LOUD"`) {
		t.Fatalf("unknown severity = %+v %s", out[0], out[0].Attributes)
	}

	// A bare log emit is the whole line.
	bare := lineFn(driver.BakedEmit{Lane: "log", Log: &driver.LogEmit{}})
	out, _ = bare.Interpret(driver.LineMessage("anything at all").Payload(), arrival)
	if out[0].Text != "anything at all" || out[0].Severity != "" {
		t.Fatalf("bare log line = %+v", out[0])
	}
}

func TestALongLogLineIsCappedAndMarked(t *testing.T) {
	fn := lineFn(driver.BakedEmit{Lane: "log", Log: &driver.LogEmit{}})
	long := strings.Repeat("x", 40<<10)
	out, _ := fn.Interpret(driver.LineMessage(long).Payload(), time.Now())
	if len(out[0].Text) != 16<<10 || !strings.Contains(string(out[0].Attributes), `"truncated":true`) {
		t.Fatalf("a 40 KiB line = %d bytes, attributes %s", len(out[0].Text), out[0].Attributes)
	}
}

func TestInterpretAPayloadTime(t *testing.T) {
	arrival := time.Date(2026, 1, 1, 0, 0, 30, 0, time.UTC)
	cases := map[string]struct {
		line, layout, zone string
		want               time.Time
	}{
		"rfc3339":          {"at 2025-12-31T23:59:00Z", "rfc3339", "", time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC)},
		"rfc3164 new year": {"at Dec 31 23:59:00", "rfc3164", "", time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC)},
		"rfc3164 zone":     {"at Jan  1 01:00:00", "rfc3164", "Europe/Paris", time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)},
		"unix":             {"at 1767225570", "unix", "", time.Unix(1767225570, 0).UTC()},
		"unix-ms":          {"at 1767225570250", "unix-ms", "", time.UnixMilli(1767225570250).UTC()},
		"go layout":        {"at 31/12/2025-23:59", "go:02/01/2006-15:04", "", time.Date(2025, 12, 31, 23, 59, 0, 0, time.UTC)},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			re := `at (.+)$`
			fn := lineFn(driver.BakedEmit{Lane: "log", Log: &driver.LogEmit{},
				TS: &driver.TimeField{Extract: driver.Extract{Regex: re}, Layout: c.layout, Zone: c.zone}})
			out, faults := fn.Interpret(driver.LineMessage(c.line).Payload(), arrival)
			if len(faults) != 0 || len(out) != 1 {
				t.Fatalf("out %+v faults %v", out, faults)
			}
			if !out[0].TS.Equal(c.want) {
				t.Fatalf("ts = %v, want %v", out[0].TS, c.want)
			}
		})
	}
}

// TestAPayloadTimeAheadOfArrivalIsClamped: past five minutes ahead, the
// record lands at arrival and keeps the time it claimed; inside the window
// it lands as claimed.
func TestAPayloadTimeAheadOfArrivalIsClamped(t *testing.T) {
	arrival := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	logFn := lineFn(driver.BakedEmit{Lane: "log", Log: &driver.LogEmit{},
		TS: &driver.TimeField{Extract: driver.Extract{Regex: `at (\S+)$`}, Layout: "rfc3339"}})
	out, _ := logFn.Interpret(driver.LineMessage("boot at 2026-10-25T13:00:00Z").Payload(), arrival)
	if !out[0].TS.Equal(arrival) || !out[0].Clamped || !strings.Contains(string(out[0].Attributes), `"ts_claimed":"2026-10-25T13:00:00Z"`) {
		t.Fatalf("an hour ahead = %+v %s", out[0], out[0].Attributes)
	}
	out, _ = logFn.Interpret(driver.LineMessage("boot at 2026-10-25T12:04:00Z").Payload(), arrival)
	if !out[0].TS.Equal(arrival.Add(4*time.Minute)) || out[0].Clamped {
		t.Fatalf("four minutes ahead = %+v", out[0])
	}

	metricFn := lineFn(driver.BakedEmit{Name: "uptime", Lane: "metric", Extract: driver.Extract{Regex: `^(\d+)`},
		TS: &driver.TimeField{Extract: driver.Extract{Regex: `at (\S+)$`}, Layout: "rfc3339"}})
	out, _ = metricFn.Interpret(driver.LineMessage("42 at 2026-10-25T13:00:00Z").Payload(), arrival)
	if !out[0].TS.Equal(arrival) || !out[0].Clamped || out[0].Value != 42 {
		t.Fatalf("a metric an hour ahead = %+v", out[0])
	}
}

// TestAnUnparsableTimeLandsAtArrivalAndFaults: the record is not lost for a
// bad time, and the fault names the layout.
func TestAnUnparsableTimeLandsAtArrivalAndFaults(t *testing.T) {
	arrival := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	fn := lineFn(driver.BakedEmit{Lane: "log", Log: &driver.LogEmit{},
		TS: &driver.TimeField{Extract: driver.Extract{Regex: `at (\S+)$`}, Layout: "rfc3339"}})
	out, faults := fn.Interpret(driver.LineMessage("boot at yesterday").Payload(), arrival)
	if len(out) != 1 || !out[0].TS.Equal(arrival) {
		t.Fatalf("out = %+v", out)
	}
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "rfc3339") {
		t.Fatalf("faults = %v", faults)
	}
}

// TestALogLineIsNeverLostToAClassificationMiss: a log line is raw arrival,
// so an extract that misses (its message, its severity, an attribute) still
// lands the line (the whole text as its message, the missed field left out)
// and reports the miss as a fault, which names it.
func TestALogLineIsNeverLostToAClassificationMiss(t *testing.T) {
	fn := lineFn(driver.BakedEmit{
		Lane:       "log",
		Log:        &driver.LogEmit{Severity: &driver.Field{Extract: rx(`^\[(\w+)\]`)}},
		Message:    &driver.Field{Extract: rx(`^\[\w+\] (.*)$`)},
		Attributes: map[string]driver.Field{"code": {Extract: rx(`code=(\d+)`)}},
	})
	out, faults := fn.Interpret(driver.LineMessage("an unbracketed line").Payload(), time.Now())
	if len(out) != 1 || out[0].Text != "an unbracketed line" || out[0].Severity != "" || out[0].Attributes != nil {
		t.Fatalf("out = %+v, want the whole line with nothing classified", out)
	}
	if len(faults) != 1 || !strings.Contains(faults[0].Error(), "message") || !strings.Contains(faults[0].Error(), "severity") || !strings.Contains(faults[0].Error(), "code") {
		t.Fatalf("faults = %v, want one naming every field that missed", faults)
	}
	// An event is a semantic assertion: an attribute that misses lands
	// nothing, as a metric that misses does.
	ev := lineFn(driver.BakedEmit{Name: "input-changed", Lane: "event", Attributes: map[string]driver.Field{"code": {Extract: rx(`code=(\d+)`)}}})
	out, faults = ev.Interpret(driver.LineMessage("no code here").Payload(), time.Now())
	if len(out) != 0 || len(faults) != 1 {
		t.Fatalf("an event whose attribute missed = %+v %v, want a fault and nothing landed", out, faults)
	}
}

// TestANonFiniteValueFaultsAndNeverPanics (adversarial review, finding 1): a
// component reporting NaN or Inf where an attribute casts to a number, or a
// scale pushing past the float range, is a fault naming the field, never a
// panic that takes the node down. A log line still lands without it.
func TestANonFiniteValueFaultsAndNeverPanics(t *testing.T) {
	arrival := time.Now().UTC()
	for _, raw := range []string{"NaN", "inf", "-Infinity", "1e308"} {
		ev := lineFn(driver.BakedEmit{Name: "input-changed", Lane: "event", Attributes: map[string]driver.Field{
			"temp": {Extract: rx(`temp=(\S+)`), Transform: &driver.Transform{Cast: "float", Scale: 10}},
		}})
		out, faults := ev.Interpret(driver.LineMessage("temp="+raw).Payload(), arrival)
		if len(out) != 0 || len(faults) != 1 || !strings.Contains(faults[0].Error(), "finite") {
			t.Fatalf("event with temp=%s = %+v %v, want a fault naming a non-finite value", raw, out, faults)
		}
		lg := lineFn(driver.BakedEmit{Lane: "log", Log: &driver.LogEmit{}, Attributes: map[string]driver.Field{
			"temp": {Extract: rx(`temp=(\S+)`), Transform: &driver.Transform{Cast: "float", Scale: 10}},
		}})
		out, faults = lg.Interpret(driver.LineMessage("temp="+raw).Payload(), arrival)
		if len(out) != 1 || out[0].Attributes != nil || len(faults) != 1 {
			t.Fatalf("log line with temp=%s = %+v %v, want the line without the field, and a fault", raw, out, faults)
		}
	}
}

// TestANulByteNeverReachesTheRecord (finding 2): Postgres text refuses a NUL
// and jsonb refuses \u0000, so an event or a log line carrying one (telnet's
// CR NUL, NUL padding) has it replaced where it is read, never landed.
func TestANulByteNeverReachesTheRecord(t *testing.T) {
	arrival := time.Now().UTC()
	ev := lineFn(driver.BakedEmit{Name: "input-changed", Lane: "event", Attributes: map[string]driver.Field{"raw": {Extract: rx(`^(.*)$`)}}})
	out, _ := ev.Interpret(driver.LineMessage("\x00EVT\x00").Payload(), arrival)
	if len(out) != 1 || strings.Contains(out[0].Text, "\x00") || strings.Contains(string(out[0].Attributes), `\u0000`) {
		t.Fatalf("event = %q %s, want no NUL", out[0].Text, out[0].Attributes)
	}
	lg := lineFn(driver.BakedEmit{Lane: "log", Log: &driver.LogEmit{Facility: &driver.Field{Extract: rx(`^(\S+)`)}}})
	out, _ = lg.Interpret(driver.LineMessage("a\x00b rest\r\x00").Payload(), arrival)
	if len(out) != 1 || strings.Contains(out[0].Text, "\x00") || strings.Contains(out[0].Facility, "\x00") {
		t.Fatalf("log line = %q facility %q, want no NUL", out[0].Text, out[0].Facility)
	}
}

// TestEachEmitCarriesItsIndex (finding 3): an interpreted emit names its
// place in the function, so two emits that share a name (two log lines, two
// of one event type) are told apart by whatever remembers them.
func TestEachEmitCarriesItsIndex(t *testing.T) {
	fn := lineFn(
		driver.BakedEmit{Lane: "log", Log: &driver.LogEmit{}, Message: &driver.Field{Value: "first"}},
		driver.BakedEmit{Lane: "log", Log: &driver.LogEmit{}, Message: &driver.Field{Value: "second"}},
	)
	out, _ := fn.Interpret(driver.LineMessage("x").Payload(), time.Now())
	if len(out) != 2 || out[0].Emit != 0 || out[1].Emit != 1 {
		t.Fatalf("out = %+v, want each emit's index", out)
	}
}

// TestALayoutWithoutADateIsRefused (finding 4): a time-only Go layout would
// land every line on 1 January; a layout must carry a month and a day (the
// year may be left out, and is the one nearest arrival).
func TestALayoutWithoutADateIsRefused(t *testing.T) {
	for _, layout := range []string{"go:15:04:05", "go:2006 15:04"} {
		s := listenerSpec(driver.Emit{Log: &driver.LogEmit{}, TS: &driver.TimeField{Extract: driver.Extract{Regex: `(.*)`}, Layout: layout}})
		if err := s.Validate(laneCatalog()); err == nil || !strings.Contains(err.Error(), "month and a day") {
			t.Fatalf("layout %s = %v, want a refusal naming the missing date", layout, err)
		}
	}
	ok := listenerSpec(driver.Emit{Log: &driver.LogEmit{}, TS: &driver.TimeField{Extract: driver.Extract{Regex: `(.*)`}, Layout: "go:Jan 2 15:04"}})
	if err := ok.Validate(laneCatalog()); err != nil {
		t.Fatalf("a yearless layout with a date refused: %v", err)
	}
}

// TestAnOutOfRangeTimeLandsAtArrivalAndFaults (finding 5): a unix time that
// is not finite, or one no timestamp can hold, is unparsable: the record lands
// at arrival and the fault names the layout.
func TestAnOutOfRangeTimeLandsAtArrivalAndFaults(t *testing.T) {
	arrival := time.Date(2026, 10, 25, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct{ raw, layout string }{{"NaN", "unix"}, {"Inf", "unix"}, {"1e300", "unix"}, {"-1e15", "unix"}, {"-9999999999999999", "unix-ms"}} {
		fn := lineFn(driver.BakedEmit{Lane: "log", Log: &driver.LogEmit{},
			TS: &driver.TimeField{Extract: driver.Extract{Regex: `at (\S+)$`}, Layout: c.layout}})
		out, faults := fn.Interpret(driver.LineMessage("boot at "+c.raw).Payload(), arrival)
		if len(out) != 1 || !out[0].TS.Equal(arrival) || len(faults) != 1 || !strings.Contains(faults[0].Error(), c.layout) {
			t.Fatalf("%s %s = %+v %v, want the line at arrival and a fault", c.layout, c.raw, out, faults)
		}
	}
}
