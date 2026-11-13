package platformmetric

import (
	"context"
	"errors"
	"log/slog"

	"github.com/hyperscaleav/omniglass/internal/writerclass"
)

// Disk is what a filesystem holds at a moment: its total bytes and the bytes
// still free on it.
type Disk struct {
	TotalBytes uint64
	FreeBytes  uint64
}

// errNoDiskSize is a filesystem that reports no size, so has no share used.
var errNoDiskSize = errors.New("filesystem reports a total of 0 bytes")

// UsedRatio is the share of the filesystem in use: the bytes not free over
// the total, 0 empty and 1 full. A free figure past the total (the filesystem
// changed between the two reads) reads empty, never a negative share. Pure.
func (d Disk) UsedRatio() (float64, error) {
	if d.TotalBytes == 0 {
		return 0, errNoDiskSize
	}
	if d.FreeBytes >= d.TotalBytes {
		return 0, nil
	}
	return float64(d.TotalBytes-d.FreeBytes) / float64(d.TotalBytes), nil
}

// DiskSampler gauges the used ratio of the filesystem holding dir each window
// (#1707), read through read (ReadDisk in the server). A full disk and an
// empty one both write a value. A read that fails costs only its window's
// disk value, never the other samplers', and is logged when the reads start
// failing rather than every window, so a data directory that is not there
// does not fill the log. One reporter calls it, a window at a time. A nil log
// is slog.Default.
func DiskSampler(dir string, read func(string) (Disk, error), log *slog.Logger) Sampler {
	if log == nil {
		log = slog.Default()
	}
	failing := false
	return func(_ context.Context, r *Recorder) {
		d, err := read(dir)
		var ratio float64
		if err == nil {
			ratio, err = d.UsedRatio()
		}
		if err != nil {
			if !failing {
				log.Warn("platform metrics: read the data directory's disk", "dir", dir, "err", err)
			}
			failing = true
			return
		}
		failing = false
		r.Gauge(writerclass.DataDirDiskUsedRatio, "", ratio)
	}
}
