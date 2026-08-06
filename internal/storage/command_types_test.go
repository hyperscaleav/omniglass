package storage_test

import (
	"context"
	"errors"
	"strings"
	"testing"

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
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	// The seeded catalogs supply the registered targets (video-input, icmp-rtt-avg).
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}

	t.Run("a metric target survives the upsert", func(t *testing.T) {
		if err := gw.UpsertCommandType(ctx, storage.CommandType{
			Name: "set-volume-official", TargetMetricType: "icmp-rtt-avg", SettleWindowSeconds: 5, Official: true,
		}); err != nil {
			t.Fatalf("upsert: %v", err)
		}
		ct, err := gw.GetCommandType(ctx, "set-volume-official")
		if err != nil {
			t.Fatalf("get: %v", err)
		}
		if ct.TargetMetricType != "icmp-rtt-avg" {
			t.Errorf("TargetMetricType = %q, want icmp-rtt-avg (the seed upsert dropped the metric arm)", ct.TargetMetricType)
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
			Name: "both-arms", TargetPropertyType: "video-input", TargetMetricType: "icmp-rtt-avg", Official: true,
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
			Name: "set-gain", TargetMetricType: "icmp-rtt-avg", SettleWindowSeconds: 5,
		})
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if ct.TargetMetricType != "icmp-rtt-avg" || ct.TargetPropertyType != "" {
			t.Errorf("created = %+v, want the metric arm carried and the property arm empty", ct)
		}
	})

	t.Run("create refuses both arms with the sentinel", func(t *testing.T) {
		_, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "both-arms-crud", TargetPropertyType: "video-input", TargetMetricType: "icmp-rtt-avg",
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
		metric := "icmp-rtt-avg"
		if ct, err = gw.UpdateCommandType(ctx, "", "set-gain", storage.CommandTypePatch{TargetMetricType: &metric}); err != nil {
			t.Fatalf("patch back: %v", err)
		}
		if ct.TargetMetricType != "icmp-rtt-avg" || ct.TargetPropertyType != "" {
			t.Errorf("after switch back = %+v, want the metric arm set and the property arm cleared", ct)
		}
	})

	t.Run("patch refuses both arms with the sentinel", func(t *testing.T) {
		prop, metric := "video-input", "icmp-rtt-avg"
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
