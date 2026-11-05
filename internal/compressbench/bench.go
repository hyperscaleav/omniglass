package compressbench

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// Options is one benchmark: the datasets, how many runs each cell is the
// median of, the timed repetitions within a run, and an optional name filter.
type Options struct {
	Specs []Spec
	Runs  int
	Reps  int
	// Only, when set, keeps the candidates whose name has one of these
	// prefixes ("F4", "raw").
	Only []string
	Log  io.Writer
}

func (o Options) keep(name string) bool {
	if len(o.Only) == 0 {
		return true
	}
	for _, p := range o.Only {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// Servers is a running server for each image, started on first use.
type Servers struct {
	by map[string]*Server
}

// Get is the server for img, starting it if it is not running.
func (s *Servers) Get(ctx context.Context, img Image) (*Server, error) {
	if s.by == nil {
		s.by = map[string]*Server{}
	}
	if sv, ok := s.by[img.Key]; ok {
		return sv, nil
	}
	sv, err := Start(ctx, img)
	if err != nil {
		return nil, err
	}
	s.by[img.Key] = sv
	return sv, nil
}

// Stop stops every server.
func (s *Servers) Stop(ctx context.Context) {
	for _, sv := range s.by {
		_ = sv.Stop(ctx)
	}
}

// Bench runs every candidate at every spec. A candidate that does not give
// back exactly what it was given is reported in its cell and in the error,
// and the rest still run.
func Bench(ctx context.Context, servers *Servers, o Options) (Report, error) {
	r := Report{Runs: o.Runs, Reps: o.Reps, Specs: o.Specs}
	var failed []error
	for _, spec := range o.Specs {
		samples := Generate(spec)
		rd := ReadFor(spec, o.Reps)
		var cands []Candidate
		for _, c := range Candidates(spec) {
			if o.keep(c.Name) {
				cands = append(cands, c)
			}
		}
		if o.Log != nil {
			fmt.Fprintf(o.Log, "%s: %d series, %d samples\n", CadenceLabel(spec.Cadence), spec.Series, len(samples))
		}
		tpls := map[string]string{}
		for _, c := range cands {
			if _, ok := tpls[c.Image.Key]; ok {
				continue
			}
			sv, err := servers.Get(ctx, c.Image)
			if err != nil {
				return r, err
			}
			tpl, err := sv.Template(ctx, spec, samples)
			if err != nil {
				return r, fmt.Errorf("stage %s on %s: %w", CadenceLabel(spec.Cadence), c.Image.Key, err)
			}
			tpls[c.Image.Key] = tpl
		}
		runs := make([][]Measure, len(cands))
		errs := make([]string, len(cands))
		// Every run builds every candidate once before the next run starts, so
		// a slow stretch on a shared host spreads over all of them.
		for run := 1; run <= o.Runs; run++ {
			for i, c := range cands {
				if errs[i] != "" {
					continue
				}
				sv, _ := servers.Get(ctx, c.Image)
				m, err := sv.RunOnce(ctx, tpls[c.Image.Key], run, c, spec, samples, rd, run == 1, o.Log)
				if err != nil {
					if ctx.Err() != nil {
						return r, ctx.Err()
					}
					errs[i] = err.Error()
					failed = append(failed, err)
					if o.Log != nil {
						fmt.Fprintf(o.Log, "  %s at %s: %v\n", c.Name, CadenceLabel(spec.Cadence), err)
					}
					continue
				}
				runs[i] = append(runs[i], m)
			}
		}
		for i, c := range cands {
			cell := Summarize(c.Name, spec.Cadence, runs[i], len(c.Compress) > 0)
			cell.Error = errs[i]
			if cell.Samples == 0 {
				cell.Samples = len(samples)
			}
			r.Cells = append(r.Cells, cell)
			if c.Cagg != nil {
				var cm []Measure
				for _, m := range runs[i] {
					if m.Cagg != nil {
						cm = append(cm, *m.Cagg)
					}
				}
				cc := Summarize(c.Cagg.Name, spec.Cadence, cm, true)
				cc.Samples = len(samples)
				cc.Error = errs[i]
				r.Cells = append(r.Cells, cc)
			}
		}
		for key, tpl := range tpls {
			sv, _ := servers.Get(ctx, imageByKey(key))
			_ = sv.DropTemplate(ctx, tpl)
		}
	}
	return r, errors.Join(failed...)
}

func imageByKey(key string) Image {
	for _, img := range []Image{ImagePlain, ImageDeltax, ImageTimescaleDB} {
		if img.Key == key {
			return img
		}
	}
	return Image{Key: key}
}
