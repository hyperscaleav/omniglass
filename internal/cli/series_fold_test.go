package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/hyperscaleav/omniglass/internal/api"
	"github.com/hyperscaleav/omniglass/internal/auth"
	"github.com/hyperscaleav/omniglass/internal/scope"
	"github.com/hyperscaleav/omniglass/internal/seed"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/storage/storagetest"
)

// TestSeriesReadsTheSameOnceFolded is #1572's API and CLI acceptance, end to
// end: a component's series over the last two weeks, a dense day among sparse
// ones, read raw and bucketed (every resolution the console asks for) through
// the generated CLI against a real server over real Postgres, answers the same
// once the production passes fold its days into a day block and a week block,
// and the same again once the raw rows those blocks cover are gone, which only
// a read that takes a folded day from its blocks can. Skipped under -short.
func TestSeriesReadsTheSameOnceFolded(t *testing.T) {
	dsn := storagetest.NewDSN(t)
	ctx := context.Background()
	gw, err := storage.NewPG(ctx, dsn)
	if err != nil {
		t.Fatalf("gw: %v", err)
	}
	defer gw.Close()
	if err := seed.Run(ctx, gw); err != nil {
		t.Fatalf("seed: %v", err)
	}
	tok, hash, prefix, err := auth.NewBearerToken()
	if err != nil {
		t.Fatalf("token: %v", err)
	}
	future := time.Now().Add(time.Hour)
	if _, err := gw.BootstrapOwner(ctx, storage.OwnerSpec{Username: "root", SecretHash: hash, Prefix: prefix, ExpiresAt: &future}); err != nil {
		t.Fatalf("bootstrap: %v", err)
	}
	all := scope.Set{All: true}
	comp, err := gw.CreateComponent(ctx, "", storage.ComponentSpec{Name: "fold-c1"}, all, all, all, all)
	if err != nil {
		t.Fatalf("component: %v", err)
	}
	if _, err := gw.CreateMetricType(ctx, "", storage.MetricTypeSpec{Name: "fold-rtt", Label: "Fold RTT", DataType: "float"}); err != nil {
		t.Fatalf("metric type: %v", err)
	}
	// Every day from 13 days back to today holds samples, inside the metric
	// lane's 14 days: 5 a day, but 200 on one day, which folds into a day
	// block; the sparse days of a whole ISO week fold into its week block.
	today := time.Now().UTC().Truncate(24 * time.Hour)
	var samples []storage.MetricSampleWrite
	for d := 13; d >= 0; d-- {
		n := 5
		if d == 9 {
			n = 200
		}
		for i := range n {
			samples = append(samples, storage.MetricSampleWrite{OwnerKind: "component", OwnerID: comp.ID, Key: "fold-rtt",
				Value: float64(d*1000 + i), Source: "t", TS: today.AddDate(0, 0, -d).Add(time.Hour + time.Duration(i)*time.Minute)})
		}
	}
	if err := gw.InsertMetricSamples(ctx, storage.PushWriter(), samples); err != nil {
		t.Fatalf("samples: %v", err)
	}
	server := httptest.NewServer(api.NewHandler(gw))
	defer server.Close()
	srv := server.URL

	reads := func() map[string]any {
		t.Helper()
		out := map[string]any{}
		for _, q := range [][]string{
			{"--hours", "720", "--limit", "2000"},
			{"--hours", "336", "--resolution", "600"},
			{"--hours", "720", "--resolution", "3600"},
			{"--hours", "720", "--resolution", "86400"},
		} {
			args := append([]string{"component", "metric", "sample", "list", "fold-c1", "fold-rtt"}, q...)
			got, err := runCLI(t, append(args, "--server", srv, "--token", tok)...)
			if err != nil {
				t.Fatalf("omniglass %s: %v (%s)", strings.Join(args, " "), err, got)
			}
			var body map[string]any
			if err := json.Unmarshal([]byte(got), &body); err != nil {
				t.Fatalf("omniglass %s printed %q, not a JSON object", strings.Join(args, " "), got)
			}
			// The grid and covered_from move with the clock; the buckets
			// holding samples are what a fold could change.
			delete(body, "covered_from")
			if b, ok := body["buckets"].([]any); ok {
				var held []any
				for _, x := range b {
					if x.(map[string]any)["count"].(float64) > 0 {
						held = append(held, x)
					}
				}
				body["buckets"] = held
			}
			out[strings.Join(q, " ")] = body
		}
		return out
	}
	same := func(when string, want, got map[string]any) {
		t.Helper()
		for q, w := range want {
			if !reflect.DeepEqual(w, got[q]) {
				t.Errorf("%s, `%s` reads otherwise than before the fold:\n got %v\nwant %v", when, q, got[q], w)
			}
		}
	}

	before := reads()
	if s := before["--hours 720 --limit 2000"].(map[string]any)["samples"].([]any); len(s) != len(samples) {
		t.Fatalf("the raw read holds %d samples, want %d", len(s), len(samples))
	}
	if _, err := gw.CompactMetricTrends(ctx, 24*20, 14); err != nil {
		t.Fatalf("trends: %v", err)
	}
	fold, err := gw.CompactMetricBlocks(ctx, storage.BlockFold{MaxDays: 20, LaneDays: 14, Verify: true})
	if err != nil {
		t.Fatalf("blocks: %v", err)
	}
	if fold.Weeks == 0 {
		t.Fatalf("the fold closed no week: %+v", fold)
	}
	conn, err := pgx.Connect(ctx, dsn)
	if err != nil {
		t.Fatalf("connect: %v", err)
	}
	defer conn.Close(ctx)
	var raw, spans int
	if err := conn.QueryRow(ctx, `select (select count(*) from metric), (select count(distinct span) from metric_block)`).Scan(&raw, &spans); err != nil {
		t.Fatal(err)
	}
	if raw != len(samples) || spans != 2 {
		t.Fatalf("the fold left %d raw rows of %d and blocks of %d spans, want a day block and a week block", raw, len(samples), spans)
	}
	same("once the days fold", before, reads())

	// Every day before the week mark is covered: the dense day by its day
	// block, a sparse day by its week's block.
	tag, err := conn.Exec(ctx, `delete from metric where ts < (select week_through from metric_block_mark)`)
	if err != nil {
		t.Fatal(err)
	}
	if tag.RowsAffected() < 200 {
		t.Fatalf("deleted %d folded raw rows, want the dense day's among them", tag.RowsAffected())
	}
	same(fmt.Sprintf("with %d folded raw rows gone", tag.RowsAffected()), before, reads())
}
