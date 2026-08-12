package storage_test

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
	"github.com/jackc/pgx/v5"
)

// commandStatus reads the recorded status pair straight off the command row:
// the schema, not the struct, is the contract under test.
func commandStatus(t *testing.T, conn *pgx.Conn, id int64) (string, *time.Time) {
	t.Helper()
	var (
		status    string
		settledAt *time.Time
	)
	if err := conn.QueryRow(context.Background(),
		`select status, settled_at from command where id = $1`, id).Scan(&status, &settledAt); err != nil {
		t.Fatalf("read command %d status: %v", id, err)
	}
	return status, settledAt
}

// TestCommandStatusRecording drives the settle-check write path over the
// gateway: a command records its terminal outcome (settled/failed/timed-out)
// with the moment it settled, a fire-and-forget command settles at issue, a
// pending command stays issued until the settle window expires, and the
// intended row a command opens names it (command_id) beside the caused event.
func TestCommandStatusRecording(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("open gateway: %v", err)
	}
	defer gw.Close()
	// The caused event needs the seeded command-issued event_type.
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	conn := connectDSN(t, dsn)
	all := scope.Set{All: true}
	// The command row stamps its actor, so the issuer must be a real principal.
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: make([]byte, 32), Prefix: "root0000"}); err != nil {
		t.Fatalf("bootstrap owner: %v", err)
	}
	var actor string
	if err := conn.QueryRow(ctx, `select principal_id from human where username = 'root'`).Scan(&actor); err != nil {
		t.Fatalf("resolve actor: %v", err)
	}

	for _, name := range []string{"disp-1", "disp-2", "disp-3"} {
		if _, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: name}, all, all, all, all); err != nil {
			t.Fatalf("create component %s: %v", name, err)
		}
	}
	if _, err := gw.CreatePropertyType(ctx, "", storage.PropertyTypeSpec{Name: "video-input-x", DataType: "string"}); err != nil {
		t.Fatalf("create property type: %v", err)
	}
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "volume-x", DataType: "float"}); err != nil {
		t.Fatalf("create metric type: %v", err)
	}
	// disp-1 and disp-3 already report hdmi2; disp-2 reports nothing.
	for _, comp := range []string{"disp-1", "disp-3"} {
		if err := gw.InsertPropertySamples(ctx, []storage.PropertySampleWrite{{
			OwnerKind: "component", OwnerID: comp, Key: "video-input-x", Value: "hdmi2", TS: time.Now().UTC(),
		}}); err != nil {
			t.Fatalf("seed observed on %s: %v", comp, err)
		}
	}

	t.Run("fire-and-forget settles at issue", func(t *testing.T) {
		if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{Name: "ff-cmd"}); err != nil {
			t.Fatalf("create command type: %v", err)
		}
		cmd, err := gw.IssueCommand(ctx, actor, "component", "disp-1", "ff-cmd", "", nil, nil, all)
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		status, settledAt := commandStatus(t, conn, cmd.ID)
		if status != "settled" || settledAt == nil {
			t.Errorf("fire-and-forget: status=%q settled_at=%v, want settled with a moment", status, settledAt)
		}
		// The computed verdict is unchanged: no target means nothing to settle.
		verdict, err := gw.CommandSettlement(ctx, "component", "disp-1", "ff-cmd", "", all)
		if err != nil {
			t.Fatalf("settlement: %v", err)
		}
		if verdict != storage.SettlementNone {
			t.Errorf("fire-and-forget verdict = %q, want none", verdict)
		}
	})

	if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
		Name: "set-now", TargetPropertyType: "video-input-x", SettleWindowSeconds: 0,
	}); err != nil {
		t.Fatalf("create command type: %v", err)
	}

	t.Run("zero-window match settles and names the command", func(t *testing.T) {
		cmd, err := gw.IssueCommand(ctx, actor, "component", "disp-1", "set-now", "", json.RawMessage(`"hdmi2"`), nil, all)
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		status, settledAt := commandStatus(t, conn, cmd.ID)
		if status != "settled" || settledAt == nil {
			t.Errorf("matched command: status=%q settled_at=%v, want settled with a moment", status, settledAt)
		}
		// The intended row names its command; the caused event stays stamped.
		var eventID *int64
		if err := conn.QueryRow(ctx,
			`select event_id from property where command_id = $1 and provenance = 'intended'`, cmd.ID).Scan(&eventID); err != nil {
			t.Fatalf("intended row for command %d: %v", cmd.ID, err)
		}
		if eventID == nil || *eventID != cmd.CausedEventID {
			t.Errorf("intended row event_id = %v, want the caused event %d still stamped", eventID, cmd.CausedEventID)
		}
	})

	// The settle-check's `now` and the sample's `ts` are two ends of one
	// comparison, and they used to come from two clocks: `ts` defaults to
	// Postgres's now(), while `now` was time.Now() in the server process. Any
	// skew between them moved the verdict, and at a zero window there is no
	// margin at all to absorb it, so a command that genuinely failed could be
	// reported pending forever on a deployment whose database is on another
	// host (#667).
	//
	// The observable that pins the fix is settled_at, because the settle-check
	// stamps it with the very `now` it judged against. Written inside
	// IssueCommand's own transaction it must equal the intended sample's ts to
	// the microsecond, since now() is the transaction's timestamp and both
	// values are that one reading of that one clock. A Go-side now cannot land
	// on it by accident.
	t.Run("the settle-check judges against the database's clock", func(t *testing.T) {
		cmd, err := gw.IssueCommand(ctx, actor, "component", "disp-1", "set-now", "", json.RawMessage(`"hdmi2"`), nil, all)
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		var intendedTS time.Time
		if err := conn.QueryRow(ctx,
			`select ts from property where command_id = $1 and provenance = 'intended'`, cmd.ID).Scan(&intendedTS); err != nil {
			t.Fatalf("intended ts for command %d: %v", cmd.ID, err)
		}
		status, settledAt := commandStatus(t, conn, cmd.ID)
		if status == "issued" || settledAt == nil {
			t.Fatalf("zero-window command: status=%q settled_at=%v, want a terminal status with a moment", status, settledAt)
		}
		if !settledAt.Equal(intendedTS) {
			t.Errorf("settled_at %s and the intended sample's ts %s came from different clocks (%s apart), want one reading of the database's",
				settledAt.UTC(), intendedTS.UTC(), settledAt.Sub(intendedTS))
		}
	})

	t.Run("zero-window mismatch fails", func(t *testing.T) {
		cmd, err := gw.IssueCommand(ctx, actor, "component", "disp-1", "set-now", "", json.RawMessage(`"hdmi9"`), nil, all)
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		if status, _ := commandStatus(t, conn, cmd.ID); status != "failed" {
			t.Errorf("mismatched command status = %q, want failed", status)
		}
	})

	t.Run("expired window with nothing observed times out", func(t *testing.T) {
		cmd, err := gw.IssueCommand(ctx, actor, "component", "disp-2", "set-now", "", json.RawMessage(`"hdmi2"`), nil, all)
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		if status, _ := commandStatus(t, conn, cmd.ID); status != "timed-out" {
			t.Errorf("unanswered command status = %q, want timed-out", status)
		}
		// The computed verdict keeps its shipped meaning: past the window with
		// no matching observation is failed.
		verdict, err := gw.CommandSettlement(ctx, "component", "disp-2", "set-now", "", all)
		if err != nil {
			t.Fatalf("settlement: %v", err)
		}
		if verdict != storage.SettlementFailed {
			t.Errorf("unanswered verdict = %q, want failed (the shipped meaning)", verdict)
		}
	})

	// The zero-window verdict is terminal by construction, not by arithmetic
	// (#667), and this is the end-to-end shape of that: a sample stamped in the
	// FUTURE relative to the clock the check reads, which is what any skew
	// between the database and the server used to look like from inside Settle.
	// The old comparison read that as "still within the window" and answered
	// pending, forever, for the one setting whose whole meaning is "do not
	// wait". A zero window says settle immediately, so no timestamp of any
	// provenance can make it pending.
	t.Run("a zero window is terminal even against a sample stamped in the future", func(t *testing.T) {
		cmd, err := gw.IssueCommand(ctx, actor, "component", "disp-2", "set-now", "", json.RawMessage(`"hdmi7"`), nil, all)
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		mustExec(t, conn, `update property set ts = now() + interval '1 hour' where command_id = $1`, cmd.ID)
		verdict, err := gw.CommandSettlement(ctx, "component", "disp-2", "set-now", "", all)
		if err != nil {
			t.Fatalf("settlement: %v", err)
		}
		if verdict != storage.SettlementFailed {
			t.Errorf("zero-window verdict against a future sample = %q, want failed", verdict)
		}
	})

	t.Run("pending stays issued until the settle-check sweeps the expired window", func(t *testing.T) {
		if _, err := gw.CreateCommandType(ctx, "", storage.CommandTypeSpec{
			Name: "set-slow", TargetPropertyType: "video-input-x", SettleWindowSeconds: 300,
		}); err != nil {
			t.Fatalf("create command type: %v", err)
		}
		cmd, err := gw.IssueCommand(ctx, actor, "component", "disp-3", "set-slow", "", json.RawMessage(`"hdmi2"`), nil, all)
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		status, settledAt := commandStatus(t, conn, cmd.ID)
		if status != "issued" || settledAt != nil {
			t.Errorf("in-window command: status=%q settled_at=%v, want issued with no settled_at", status, settledAt)
		}
		// The window expires (backdate the command and its intended row), and
		// the next settle-check records the outcome.
		mustExec(t, conn, `update command set ts = ts - interval '1 hour' where id = $1`, cmd.ID)
		mustExec(t, conn, `update property set ts = ts - interval '1 hour' where command_id = $1`, cmd.ID)
		verdict, err := gw.CommandSettlement(ctx, "component", "disp-3", "set-slow", "", all)
		if err != nil {
			t.Fatalf("settlement: %v", err)
		}
		if verdict != storage.SettlementSettled {
			t.Errorf("expired matched verdict = %q, want settled", verdict)
		}
		status, settledAt = commandStatus(t, conn, cmd.ID)
		if status != "settled" || settledAt == nil {
			t.Errorf("swept command: status=%q settled_at=%v, want settled with a moment", status, settledAt)
		}
	})

	t.Run("metric target opens an intended metric row", func(t *testing.T) {
		// No metric-targeted command ships yet, so the command_type is a fixture.
		mustExec(t, conn, `insert into command_type (name, target_metric_type_id, settle_window_seconds)
			values ('set-volume-x', (select id from metric_type where name = 'volume-x'), 0)`)
		if err := gw.InsertMetricSamples(ctx, []storage.MetricSampleWrite{{
			OwnerKind: "component", OwnerID: "disp-1", Key: "volume-x", Value: 50, TS: time.Now().UTC(),
		}}); err != nil {
			t.Fatalf("seed observed metric: %v", err)
		}
		cmd, err := gw.IssueCommand(ctx, actor, "component", "disp-1", "set-volume-x", "", json.RawMessage(`50`), nil, all)
		if err != nil {
			t.Fatalf("issue: %v", err)
		}
		var (
			got     float64
			eventID *int64
		)
		if err := conn.QueryRow(ctx,
			`select value, event_id from metric where command_id = $1 and provenance = 'intended'`, cmd.ID).Scan(&got, &eventID); err != nil {
			t.Fatalf("intended metric row for command %d: %v", cmd.ID, err)
		}
		if got != 50 || eventID == nil || *eventID != cmd.CausedEventID {
			t.Errorf("intended metric row = (%v, event %v), want (50, event %d)", got, eventID, cmd.CausedEventID)
		}
		if status, _ := commandStatus(t, conn, cmd.ID); status != "settled" {
			t.Errorf("matched metric command status = %q, want settled", status)
		}
	})

	t.Run("metric target refuses a non-numeric value", func(t *testing.T) {
		var before int
		if err := conn.QueryRow(ctx, `select count(*) from command`).Scan(&before); err != nil {
			t.Fatalf("count commands: %v", err)
		}
		if _, err := gw.IssueCommand(ctx, actor, "component", "disp-1", "set-volume-x", "", json.RawMessage(`"loud"`), nil, all); err == nil {
			t.Fatal("non-numeric intended value for a metric target accepted, want a loud refusal")
		}
		var after int
		if err := conn.QueryRow(ctx, `select count(*) from command`).Scan(&after); err != nil {
			t.Fatalf("count commands: %v", err)
		}
		if after != before {
			t.Errorf("refused command still recorded: %d commands, want %d", after, before)
		}
	})
}
