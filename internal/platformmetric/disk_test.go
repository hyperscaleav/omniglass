package platformmetric_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/internal/platformmetric"
	"github.com/hyperscaleav/omniglass/internal/storage"
	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// TestDiskUsedRatioIsUsedOverTotal: the ratio is the bytes in use (total
// less free) over the filesystem's total, 0 empty and 1 full, and a
// filesystem that reports no size has no ratio to read.
func TestDiskUsedRatioIsUsedOverTotal(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		d    platformmetric.Disk
		want float64
	}{
		{platformmetric.Disk{TotalBytes: 1000, FreeBytes: 250}, 0.75},
		{platformmetric.Disk{TotalBytes: 1000, FreeBytes: 1000}, 0},
		{platformmetric.Disk{TotalBytes: 1000, FreeBytes: 0}, 1},
		// A free figure past the total (a filesystem shrunk between the
		// two reads statfs makes) reads empty, never a negative share.
		{platformmetric.Disk{TotalBytes: 1000, FreeBytes: 1200}, 0},
	} {
		got, err := tc.d.UsedRatio()
		if err != nil || got != tc.want {
			t.Errorf("%+v used ratio = %v (%v), want %v", tc.d, got, err, tc.want)
		}
	}
	if _, err := (platformmetric.Disk{}).UsedRatio(); err == nil {
		t.Error("a filesystem of no size read a ratio, want an error")
	}
}

// diskWindows reports n windows through a reporter whose disk sampler reads
// read, beside a sampler that always gauges the pool's size, and returns each
// window's rows.
func diskWindows(t *testing.T, n int, read func(string) (platformmetric.Disk, error), log *slog.Logger) [][]storage.PlatformMetric {
	t.Helper()
	rec := platformmetric.NewRecorder(time.Now)
	store := &fakeStore{}
	rp := platformmetric.NewReporter(rec, store, "server-1", func(context.Context) time.Duration { return time.Minute }, log,
		platformmetric.DiskSampler("/var/lib/omniglass", read, log),
		func(_ context.Context, r *platformmetric.Recorder) { r.Gauge(writerclass.DBPoolMax, "", 4) })
	for range n {
		if err := rp.Report(context.Background()); err != nil {
			t.Fatalf("report: %v", err)
		}
	}
	return store.writes()
}

// diskValue is the window's data-dir-disk-used-ratio, if it carries one.
func diskValue(rows []storage.PlatformMetric) (float64, bool) {
	for _, r := range rows {
		if r.Key == writerclass.DataDirDiskUsedRatio {
			return r.Value, true
		}
	}
	return 0, false
}

// TestDiskSamplerGaugesEveryWindow (#1707): the sampler reads the data
// directory's filesystem each window and gauges its used ratio, so a full
// disk and an empty one both write a value, every window.
func TestDiskSamplerGaugesEveryWindow(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		d    platformmetric.Disk
		want float64
	}{
		{"full", platformmetric.Disk{TotalBytes: 1 << 30, FreeBytes: 0}, 1},
		{"empty", platformmetric.Disk{TotalBytes: 1 << 30, FreeBytes: 1 << 30}, 0},
	} {
		var dirs []string
		w := diskWindows(t, 3, func(dir string) (platformmetric.Disk, error) {
			dirs = append(dirs, dir)
			return tc.d, nil
		}, nil)
		if len(w) != 3 {
			t.Fatalf("%s disk: %d windows written, want 3", tc.name, len(w))
		}
		for i, rows := range w {
			if v, ok := diskValue(rows); !ok || v != tc.want {
				t.Errorf("%s disk window %d: used ratio = %v (present %v), want %v", tc.name, i, v, ok, tc.want)
			}
		}
		for _, d := range dirs {
			if d != "/var/lib/omniglass" {
				t.Errorf("%s disk: read %q, want the data directory", tc.name, d)
			}
		}
	}
}

// TestDiskSamplerReadErrorIsLoggedAndSkipsOnlyItsValue (#1707): a statfs that
// fails is logged and its window carries no disk value, while the other
// samplers' figures still land. A failure that persists logs once, not every
// window, and a read that recovers gauges again.
func TestDiskSamplerReadErrorIsLoggedAndSkipsOnlyItsValue(t *testing.T) {
	t.Parallel()
	var buf bytes.Buffer
	log := slog.New(slog.NewTextHandler(&buf, nil))
	calls := 0
	w := diskWindows(t, 3, func(string) (platformmetric.Disk, error) {
		calls++
		if calls < 3 {
			return platformmetric.Disk{}, errors.New("statfs: no such file or directory")
		}
		return platformmetric.Disk{TotalBytes: 100, FreeBytes: 40}, nil
	}, log)
	if len(w) != 3 {
		t.Fatalf("%d windows written, want 3: a failed disk read must not cost the window", len(w))
	}
	for i, rows := range w[:2] {
		if v, ok := diskValue(rows); ok {
			t.Errorf("window %d carries a disk value %v from a failed read", i, v)
		}
		pool := false
		for _, r := range rows {
			pool = pool || r.Key == writerclass.DBPoolMax
		}
		if !pool {
			t.Errorf("window %d lost the other sampler's figure: %+v", i, rows)
		}
	}
	if v, ok := diskValue(w[2]); !ok || v != 0.6 {
		t.Errorf("recovered window: used ratio = %v (present %v), want 0.6", v, ok)
	}
	out := buf.String()
	if n := strings.Count(out, "no such file or directory"); n != 1 {
		t.Errorf("the failing read logged %d times, want once for the streak:\n%s", n, out)
	}
	if !strings.Contains(out, "/var/lib/omniglass") {
		t.Errorf("the log does not name the directory it could not read:\n%s", out)
	}
}

// TestReadDiskReadsARealFilesystem: the statfs edge reads the filesystem a
// real directory sits on, and a directory that is not there is an error.
func TestReadDiskReadsARealFilesystem(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	d, err := platformmetric.ReadDisk(dir)
	if err != nil {
		t.Fatalf("read disk %s: %v", dir, err)
	}
	if d.TotalBytes == 0 || d.FreeBytes > d.TotalBytes {
		t.Errorf("disk = %+v, want a sized filesystem with no more free than its total", d)
	}
	if _, err := platformmetric.ReadDisk(filepath.Join(dir, "absent")); err == nil {
		t.Error("reading a directory that is not there returned no error")
	}
}
