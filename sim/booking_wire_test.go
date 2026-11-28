//go:build linux

package sim

import (
	"strconv"
	"testing"
	"time"

	"github.com/hyperscaleav/omniglass/sim/world"
)

// The booking service's wire script (#2836): it has no face to poll, only the
// batch it pushes for each system it is granted. Its transcript is that batch
// rendered at every half hour of a day, for a system the world places and one it
// does not, and the pinned room day of the batches a room's calendar makes.

const bookingProduct = "booking-service"

var bookingOwners = []string{"sys-a", "sys-b", "sys-c", "sys-nobody", ""}

// bookingRenders is the batch for each owner at each half hour of the pinned day,
// the day's windows on a device the world does not place, then the same device
// placed in a world.
func bookingRenders(t *testing.T, fam wireFamily, product string, placed bool) []map[string]string {
	t.Helper()
	d, at := roomDayEngine(t, fam.Engine(t), product, false)
	if placed {
		w, _ := parityWorld("d-1", "booking-service", parityCovers())
		d.worldSrc = func() *world.World { return w }
	}
	var out []map[string]string
	for tm := parityAt("00:00:00"); !tm.After(parityAt("23:30:00")); tm = tm.Add(30 * time.Minute) {
		*at = tm
		row := map[string]string{"at": tm.Format("15:04:05")}
		for _, o := range bookingOwners {
			b, err := d.RenderPush(o)
			if err != nil {
				row[o] = "error: " + err.Error()
				continue
			}
			row[o] = string(b)
		}
		out = append(out, row)
	}
	return out
}

func bookingRoomDay(d *Device, at *time.Time) []wireChange {
	w, _ := parityWorld("d-1", "booking-service", parityCovers())
	d.worldSrc = func() *world.World { return w }
	watch := &watchChanges{d: d, last: map[string]string{}}
	sample := func(hms string) {
		for _, o := range []string{"sys-a", "sys-b", "sys-c", "sys-nobody"} {
			b, err := d.RenderPush(o)
			v := string(b)
			if err != nil {
				v = "error: " + err.Error()
			}
			if watch.last[o] != v {
				watch.last[o] = v
				watch.out = append(watch.out, wireChange{At: hms, Path: "push." + o, Value: v})
			}
		}
	}
	sample("start")
	roomDayWalk(d, at, watch, "07:00:00", "15:00:00", time.Minute, func(string) {}, func(hms string) { sample(hms) })
	return watch.out
}

func bookingExtra(t *testing.T, fam wireFamily, product string) map[string]any {
	d, _ := roomDayEngine(t, fam.Engine(t), product, false)
	return map[string]any{
		"every":       strconv.FormatInt(int64(d.PushEvery()/time.Second), 10),
		"day":         bookingRenders(t, fam, product, false),
		"day_in_room": bookingRenders(t, fam, product, true),
	}
}

var bookingWire = wireFamily{
	Name: "booking-service", Products: []string{bookingProduct}, Engine: shippedEngineOf, Source: wireYAMLSource, Frozen: wireYAMLFrozen,
	RoomDay: bookingRoomDay, Extra: bookingExtra,
}

func init() { wireFamilies = append(wireFamilies, bookingWire) }
