package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestUpsertCommandTypeTargetArms drives the seed path's two-armed target: the
// boot-seed upsert must carry a metric target (the other arm of the exclusive
// arc, #596) and must refuse, naming the row, when a target names an
// unregistered classifier or when both arms are set. Before this slice the
// upsert dropped the metric arm on the floor and a bare subselect silently
// NULLed an unregistered property target.
func TestUpsertCommandTypeTargetArms(t *testing.T) {
	t.Parallel()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	// The seeded catalogs supply the registered targets (video-input, and uptime
	// on the metric arm: a collected key a command may write, ADR-0161).
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("a metric target survives the upsert", func(t *testing.T) {
		if err := gw.UpsertCommandType(ctx, storage.CommandType{
			Name: "set-volume-official", TargetMetricType: "uptime", SettleWindowSeconds: 5, Official: true,
		}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		ct, err := gw.GetCommandType(ctx, "set-volume-official")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if ct.TargetMetricType != "uptime" {
			t.Errorf("TargetMetricType = %q, want uptime (the seed upsert dropped the metric arm)", ct.TargetMetricType)
		}
	})

	t.Run("an unregistered property target refuses, naming the row", func(t *testing.T) {
		err := gw.UpsertCommandType(ctx, storage.CommandType{
			Name: "bad-prop-target", TargetPropertyType: "no-such-property", Official: true,
		})
		if err == nil {
			t.Fatal("upsert with an unregistered property target succeeded, want a refusal naming the row")
		}
		if !strings.Contains(err.Error(), "bad-prop-target") || !strings.Contains(err.Error(), "no-such-property") {
			t.Errorf("error %q does not name the row and the target", err)
		}
	})

	t.Run("an unregistered metric target refuses, naming the row", func(t *testing.T) {
		err := gw.UpsertCommandType(ctx, storage.CommandType{
			Name: "bad-metric-target", TargetMetricType: "no-such-metric", Official: true,
		})
		if err == nil {
			t.Fatal("upsert with an unregistered metric target succeeded, want a refusal naming the row")
		}
		if !strings.Contains(err.Error(), "bad-metric-target") || !strings.Contains(err.Error(), "no-such-metric") {
			t.Errorf("error %q does not name the row and the target", err)
		}
	})

	t.Run("both arms refuse with the arc named", func(t *testing.T) {
		err := gw.UpsertCommandType(ctx, storage.CommandType{
			Name: "both-arms", TargetPropertyType: "video-input", TargetMetricType: "uptime", Official: true,
		})
		if err == nil {
			t.Fatal("upsert with both target arms succeeded, want the arc refusal")
		}
		if !strings.Contains(err.Error(), "never both") {
			t.Errorf("error %q does not name the exclusive arc", err)
		}
	})
}

// TestCommandTypeTargetSpecPatch drives the metric arm through the CRUD paths:
// a create round-trips a metric target by name, both arms refuse with the named
// sentinel (the DB CHECK is the backstop, not the message), and a patch treats
// the target as one fact with two forms: setting either arm replaces it
// wholesale (the other arm clears), the empty string clears it outright.
func TestCommandTypeTargetSpecPatch(t *testing.T) {
	t.Parallel()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("create round-trips a metric target", func(t *testing.T) {
		ct, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "set-gain", TargetMetricType: "uptime", SettleWindowSeconds: 5,
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if ct.TargetMetricType != "uptime" || ct.TargetPropertyType != "" {
			t.Errorf("created = %+v, want the metric arm carried and the property arm empty", ct)
		}
	})

	t.Run("create refuses both arms with the sentinel", func(t *testing.T) {
		_, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "both-arms-crud", TargetPropertyType: "video-input", TargetMetricType: "uptime",
		})
		if !errors.Is(err, storage.ErrCommandTypeTargetArc) {
			t.Fatalf("create with both arms = %v, want ErrCommandTypeTargetArc", err)
		}
	})

	t.Run("create refuses an unregistered metric target as invalid", func(t *testing.T) {
		_, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "bad-metric-crud", TargetMetricType: "no-such-metric",
		})
		if !errors.Is(err, storage.ErrCommandTypeInvalid) {
			t.Fatalf("create with an unregistered metric target = %v, want ErrCommandTypeInvalid", err)
		}
		if !strings.Contains(err.Error(), "no-such-metric") {
			t.Errorf("error %q does not name the target", err)
		}
	})

	t.Run("patch switches the arm and clears the other", func(t *testing.T) {
		prop := "video-input"
		ct, err := gw.UpdateCommandType(ctx, "", "set-gain", storage.CommandTypePatch{TargetPropertyType: &prop})
		if err != nil {
			t.Fatalf("patch: %v", err)
		}
		if ct.TargetPropertyType != "video-input" || ct.TargetMetricType != "" {
			t.Errorf("after arm switch = %+v, want the property arm set and the metric arm cleared", ct)
		}
		metric := "uptime"
		if ct, err = gw.UpdateCommandType(ctx, "", "set-gain", storage.CommandTypePatch{TargetMetricType: &metric}); err != nil {
			t.Fatalf("patch back: %v", err)
		}
		if ct.TargetMetricType != "uptime" || ct.TargetPropertyType != "" {
			t.Errorf("after switch back = %+v, want the metric arm set and the property arm cleared", ct)
		}
	})

	t.Run("patch refuses both arms with the sentinel", func(t *testing.T) {
		prop, metric := "video-input", "uptime"
		_, err := gw.UpdateCommandType(ctx, "", "set-gain", storage.CommandTypePatch{
			TargetPropertyType: &prop, TargetMetricType: &metric,
		})
		if !errors.Is(err, storage.ErrCommandTypeTargetArc) {
			t.Fatalf("patch with both arms = %v, want ErrCommandTypeTargetArc", err)
		}
	})

	t.Run("the empty string clears the target", func(t *testing.T) {
		empty := ""
		ct, err := gw.UpdateCommandType(ctx, "", "set-gain", storage.CommandTypePatch{TargetMetricType: &empty})
		if err != nil {
			t.Fatalf("clear: %v", err)
		}
		if ct.TargetMetricType != "" || ct.TargetPropertyType != "" {
			t.Errorf("after clear = %+v, want fire-and-forget", ct)
		}
	})
}

// TestCommandTypeRefusesANegativeSettleWindow: the window is a DURATION, and a
// negative one is not a shorter duration, it is a value with no meaning that the
// column, the API body, the console and the seed loader all accepted (#718).
//
// It is invisible rather than merely untidy. `Settle` tests `windowSeconds > 0`,
// so -5 behaves exactly as 0 does: terminal by construction, no waiting
// (ADR-0108). An operator who typed a negative into the console's window field
// would be shown it back on the row and get behaviour that has nothing to do
// with the number, forever, with no refusal anywhere to say so.
//
// Zero is deliberately NOT refused here: it is the documented way to say "settle
// immediately", a statement of intent that ADR-0108 made terminal by
// construction, and the shipped `reboot` type carries it. The floor is the
// smallest one that admits every meaning the field has.
func TestCommandTypeRefusesANegativeSettleWindow(t *testing.T) {
	t.Parallel()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("create refuses it, naming the field", func(t *testing.T) {
		_, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "neg-window", TargetPropertyType: "video-input", SettleWindowSeconds: -5,
		})
		if !errors.Is(err, storage.ErrCommandTypeInvalid) {
			t.Fatalf("create with a negative window = %v, want ErrCommandTypeInvalid", err)
		}
		if !strings.Contains(err.Error(), "settle_window_seconds") {
			t.Errorf("error %q does not name the field an operator has to change", err)
		}
		// Nothing was written: the refusal is not a half-create.
		if _, err := gw.GetCommandType(ctx, "neg-window"); !errors.Is(err, storage.ErrCommandTypeNotFound) {
			t.Errorf("get after the refusal = %v, want ErrCommandTypeNotFound", err)
		}
	})

	t.Run("update refuses it, and the row keeps its window", func(t *testing.T) {
		if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "patch-window", TargetPropertyType: "video-input", SettleWindowSeconds: 15,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		neg := -1
		if _, err := gw.UpdateCommandType(ctx, "", "patch-window", storage.CommandTypePatch{SettleWindowSeconds: &neg}); !errors.Is(err, storage.ErrCommandTypeInvalid) {
			t.Fatalf("patch to a negative window = %v, want ErrCommandTypeInvalid", err)
		}
		ct, err := gw.GetCommandType(ctx, "patch-window")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if ct.SettleWindowSeconds != 15 {
			t.Errorf("window after the refused patch = %d, want the 15 it had", ct.SettleWindowSeconds)
		}
	})

	t.Run("the seed upsert refuses it too, naming the row", func(t *testing.T) {
		err := gw.UpsertCommandType(ctx, storage.CommandType{
			Name: "neg-official", SettleWindowSeconds: -30, Official: true,
		})
		if err == nil {
			t.Fatal("upsert with a negative window succeeded, want a refusal naming the row")
		}
		if !strings.Contains(err.Error(), "neg-official") {
			t.Errorf("error %q does not name the row, which is how a seed defect is found", err)
		}
	})

	t.Run("zero and a positive window are both still accepted", func(t *testing.T) {
		// Zero with no target: the shipped fire-and-forget shape (reboot).
		if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{Name: "ff-zero"}); err != nil {
			t.Errorf("create of a fire-and-forget type: %v", err)
		}
		// Zero WITH a target: "settle immediately", terminal by construction
		// (ADR-0108), which this floor deliberately leaves reachable.
		if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "settle-now", TargetPropertyType: "video-input", SettleWindowSeconds: 0,
		}); err != nil {
			t.Errorf("create of a zero-window settleable type: %v", err)
		}
		if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "settle-later", TargetPropertyType: "video-input", SettleWindowSeconds: 30,
		}); err != nil {
			t.Errorf("create of a windowed settleable type: %v", err)
		}
		// And the seeded rows, which a boot re-runs on every start, are untouched.
		// The disruption columns (#1651) ride beside the window and leave it
		// alone: reboot holds its endpoints for a 15 s quiet and a 300 s
		// backstop, and set-input is not disruptive.
		type seeded struct {
			window, quiet, hold int
			disruptive          bool
		}
		for name, want := range map[string]seeded{
			"set-input": {window: 15},
			"reboot":    {window: 0, disruptive: true, quiet: 15, hold: 300},
		} {
			ct, err := gw.GetCommandType(ctx, name)
			if err != nil {
				t.Fatalf("get %s: %v", name, err)
			}
			got := seeded{window: ct.SettleWindowSeconds, quiet: ct.QuietSeconds, hold: ct.HoldSeconds, disruptive: ct.Disruptive}
			if got != want {
				t.Errorf("seeded %s = %+v, want %+v", name, got, want)
			}
		}
	})
}

// TestCommandTypeDisruption drives the three disruption columns (#1651): a
// disruptive type closes its endpoints' gates at delivery for quiet_seconds,
// then each reopens on its own first up verdict, with hold_seconds the
// backstop. The migration defaults every existing row to non-disruptive; the
// upsert round-trips the columns; and create and patch refuse the shapes that
// mean nothing: a negative duration, a quiet or hold on a type that disrupts
// nothing, and a backstop that does not outlast the quiet.
func TestCommandTypeDisruption(t *testing.T) {
	t.Parallel()
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("a row written without the columns is not disruptive", func(t *testing.T) {
		conn, err := pgx.Connect(ctx, dsn)
		if err != nil {
			t.Fatalf("connect: %v", err)
		}
		defer conn.Close(ctx)
		if _, err := conn.Exec(ctx, `insert into command_type (name) values ('raw-row')`); err != nil {
			t.Fatalf("raw insert: %v", err)
		}
		ct, err := gw.GetCommandType(ctx, "raw-row")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if ct.Disruptive || ct.QuietSeconds != 0 || ct.HoldSeconds != 0 {
			t.Errorf("defaults = disruptive %v quiet %d hold %d, want false 0 0", ct.Disruptive, ct.QuietSeconds, ct.HoldSeconds)
		}
	})

	t.Run("the upsert round-trips the columns", func(t *testing.T) {
		if err := gw.UpsertCommandType(ctx, storage.CommandType{
			Name: "power-cycle-official", Disruptive: true, QuietSeconds: 20, HoldSeconds: 120, Official: true,
		}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		ct, err := gw.GetCommandType(ctx, "power-cycle-official")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !ct.Disruptive || ct.QuietSeconds != 20 || ct.HoldSeconds != 120 {
			t.Errorf("round trip = disruptive %v quiet %d hold %d, want true 20 120", ct.Disruptive, ct.QuietSeconds, ct.HoldSeconds)
		}
		// Authoritative on conflict: a re-seed that clears it clears it.
		if err := gw.UpsertCommandType(ctx, storage.CommandType{Name: "power-cycle-official", Official: true}); err != nil {
			t.Fatalf("re-upsert: %v", err)
		}
		if ct, _ := gw.GetCommandType(ctx, "power-cycle-official"); ct.Disruptive || ct.QuietSeconds != 0 || ct.HoldSeconds != 0 {
			t.Errorf("after re-upsert = %+v, want non-disruptive", ct)
		}
	})

	refusals := []struct {
		name        string
		disruptive  bool
		quiet, hold int
		field       string
	}{
		{"negative quiet", true, -1, 60, "quiet_seconds"},
		{"negative hold", true, 0, -1, "hold_seconds"},
		{"quiet without disruption", false, 10, 0, "quiet_seconds"},
		{"hold without disruption", false, 0, 60, "hold_seconds"},
		{"hold equal to quiet", true, 30, 30, "hold_seconds"},
		{"hold under quiet", true, 30, 10, "hold_seconds"},
	}
	for _, tc := range refusals {
		t.Run("create refuses "+tc.name, func(t *testing.T) {
			name := "create-" + strings.ReplaceAll(tc.name, " ", "-")
			_, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
				Name: name, Disruptive: tc.disruptive, QuietSeconds: tc.quiet, HoldSeconds: tc.hold,
			})
			if !errors.Is(err, storage.ErrCommandTypeInvalid) {
				t.Fatalf("create = %v, want ErrCommandTypeInvalid", err)
			}
			if !strings.Contains(err.Error(), tc.field) {
				t.Errorf("error %q does not name %s", err, tc.field)
			}
			if _, err := gw.GetCommandType(ctx, name); !errors.Is(err, storage.ErrCommandTypeNotFound) {
				t.Errorf("get after the refusal = %v, want ErrCommandTypeNotFound", err)
			}
		})
		t.Run("the seed upsert refuses "+tc.name, func(t *testing.T) {
			err := gw.UpsertCommandType(ctx, storage.CommandType{
				Name: "seed-bad", Disruptive: tc.disruptive, QuietSeconds: tc.quiet, HoldSeconds: tc.hold, Official: true,
			})
			if !errors.Is(err, storage.ErrCommandTypeInvalid) || !strings.Contains(err.Error(), "seed-bad") {
				t.Fatalf("upsert = %v, want ErrCommandTypeInvalid naming the row", err)
			}
		})
	}

	t.Run("create accepts a disruptive type and patch checks the merged row", func(t *testing.T) {
		if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "cycle", Disruptive: true, QuietSeconds: 10, HoldSeconds: 60,
		}); err != nil {
			t.Fatalf("create: %v", err)
		}
		short := 10
		if _, err := gw.UpdateCommandType(ctx, "", "cycle", storage.CommandTypePatch{HoldSeconds: &short}); !errors.Is(err, storage.ErrCommandTypeInvalid) {
			t.Fatalf("patch hold to the quiet = %v, want ErrCommandTypeInvalid", err)
		}
		neg := -5
		if _, err := gw.UpdateCommandType(ctx, "", "cycle", storage.CommandTypePatch{QuietSeconds: &neg}); !errors.Is(err, storage.ErrCommandTypeInvalid) {
			t.Fatalf("patch quiet negative = %v, want ErrCommandTypeInvalid", err)
		}
		off := false
		if _, err := gw.UpdateCommandType(ctx, "", "cycle", storage.CommandTypePatch{Disruptive: &off}); !errors.Is(err, storage.ErrCommandTypeInvalid) {
			t.Fatalf("patch disruptive off, keeping quiet and hold = %v, want ErrCommandTypeInvalid", err)
		}
		ct, err := gw.GetCommandType(ctx, "cycle")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if !ct.Disruptive || ct.QuietSeconds != 10 || ct.HoldSeconds != 60 {
			t.Errorf("after refused patches = %+v, want the row unchanged", ct)
		}
		zero := 0
		ct, err = gw.UpdateCommandType(ctx, "", "cycle", storage.CommandTypePatch{Disruptive: &off, QuietSeconds: &zero, HoldSeconds: &zero})
		if err != nil {
			t.Fatalf("patch to non-disruptive: %v", err)
		}
		if ct.Disruptive || ct.QuietSeconds != 0 || ct.HoldSeconds != 0 {
			t.Errorf("after patch = %+v, want non-disruptive", ct)
		}
	})
}
