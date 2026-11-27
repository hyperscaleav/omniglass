package sim

import (
	"fmt"
	"sort"
	"strconv"
	"text/template"
	"time"

	"github.com/hyperscaleav/omniglass/sim/calendar"
	"github.com/hyperscaleav/omniglass/sim/world"
)

// A meeting platform lists devices as well as rooms (#2790), as a real
// account's admin plane does: every codec, bar, controller, scheduling panel,
// camera and microphone in the rooms it covers, one record each, a page at a
// time. The world owns the ids and the kinds; the platform only reads them, so
// nothing here asks Omniglass what a device is.

// meetingTopic is the topic a meeting reads as: the lab calendar carries no
// topic of its own, so the platform names it for the person who booked it, and
// a walk-in, which nobody booked, plainly.
func meetingTopic(m map[string]any) string {
	if m == nil {
		return ""
	}
	if o, _ := m["organizer"].(string); o != "" {
		return "Meeting hosted by " + o
	}
	return "Walk-in meeting"
}

// listingFuncs are the template functions a platform that lists devices reads
// them by, through the device's clock:
//
//	meetingTopic m          -> the topic of a calendar meeting (map), "" for none
//	roomOffline system      -> whether the system's room reads offline: a codec or a
//	                           bar in it is unplugged by a scripted problem on the
//	                           calendar (#2769), a fact of the world, not of the
//	                           unplugged device's own state
//	devicePage held a n     -> one page of the account's devices: the devices of
//	                           the rooms the account covers (the world's) and the
//	                           ones it holds in state (held, a map by id) that no
//	                           room places, in id order, the n (a number or its
//	                           text; the default when unreadable) after the id a
//	                           (the last of the page before, empty for the
//	                           first), as {devices, next}; next is the token
//	                           that asks for the following page, empty on the last
func (d *Device) listingFuncs() template.FuncMap {
	return template.FuncMap{
		"meetingTopic": meetingTopic,
		"devicePage": func(held any, after any, size any) map[string]any {
			return devicePage(d.world().Listed(d.Hostname), held, after, size, d.clock().Unix(), d.unpluggedSince)
		},
		"roomOffline": d.roomOffline,
	}
}

// devicePage is one page of a merged, id-ordered device list: the world's
// listing and the held records (a map by id) no listing names. A device the
// world lists is online and last seen now; a held one reads as it is held.
// Pure: the listing, the held map, the token, the size and the instant decide
// it.
func devicePage(listed []world.Listing, held any, after any, size any, now int64, unplugged func(world.Listing) (int64, bool)) map[string]any {
	n := defaultPageSize
	if v, err := strconv.Atoi(fmt.Sprint(size)); err == nil && v > 0 {
		n = min(v, maxPageSize)
	}
	tok := ""
	if after != nil {
		if tok = fmt.Sprint(after); tok == "<no value>" {
			tok = ""
		}
	}
	var heldIDs []string
	hm, _ := held.(map[string]any)
	byID := make(map[string]bool, len(listed))
	for _, l := range listed {
		byID[l.ID] = true
	}
	for id := range hm {
		if !byID[id] {
			heldIDs = append(heldIDs, id)
		}
	}
	sort.Strings(heldIDs)
	i := sort.Search(len(listed), func(i int) bool { return listed[i].ID > tok })
	j := sort.SearchStrings(heldIDs, tok)
	if j < len(heldIDs) && heldIDs[j] == tok {
		j++
	}
	page := make([]map[string]any, 0, n)
	last := ""
	for len(page) < n && (i < len(listed) || j < len(heldIDs)) {
		if j >= len(heldIDs) || (i < len(listed) && listed[i].ID < heldIDs[j]) {
			l := listed[i]
			i++
			status, seen := "online", now
			if since, down := unplugged(l); down {
				status, seen = "offline", since
			}
			page = append(page, map[string]any{
				"id": l.ID, "name": l.Name, "room_id": l.System, "type": l.Type,
				"status": status, "firmware": l.Firmware, "last_seen": seen,
			})
			last = l.ID
			continue
		}
		id := heldIDs[j]
		j++
		r, _ := hm[id].(map[string]any)
		page = append(page, map[string]any{
			"id": id, "name": orStr(r["name"], id), "room_id": orStr(r["room_id"], ""), "type": orStr(r["type"], ""),
			"status": orStr(r["status"], "online"), "firmware": orStr(r["firmware"], ""), "last_seen": heldSeen(r["last_seen"], now),
		})
		last = id
	}
	next := ""
	if i < len(listed) || j < len(heldIDs) {
		next = last
	}
	return map[string]any{"devices": page, "next": next}
}

func orStr(v any, def string) string {
	if s, ok := v.(string); ok && s != "" {
		return s
	}
	return def
}

// heldSeen is a held device's last-seen time: the one it was held with, else now.
func heldSeen(v any, now int64) int64 {
	switch n := v.(type) {
	case int:
		if n > 0 {
			return int64(n)
		}
	case int64:
		if n > 0 {
			return n
		}
	case float64:
		if n > 0 {
			return int64(n)
		}
	}
	return now
}

// unpluggedSince says whether the arbiter has the listed device unplugged now,
// and if so since when (unix seconds): by an operator, or by a scripted problem
// on the lab calendar, whose own start the platform reads, so it learns what
// the world did and not what the dark device would say (#2799).
func (d *Device) unpluggedSince(l world.Listing) (int64, bool) {
	if d.arbSrc == nil {
		return 0, false
	}
	return d.arbSrc().Offline(l, d.clock())
}

// scriptedOffline is the arbiter's derivation for scripted problems: the lab
// calendar says a problem has the listed device unplugged, whichever simulator
// serves it.
func (e *Engine) scriptedOffline(l world.Listing, now time.Time) (int64, bool) {
	c := e.Calendar()
	if c == nil {
		return 0, false
	}
	room := calendar.Room{Site: l.Site, Building: l.Building, Room: l.Room}
	for _, e := range c.Active(room, now, calendar.ProblemKinds) {
		if p, ok := e.AsProblem(); ok && p.Impairment.Unplugged && p.Role == l.Slot {
			return e.Start.Unix(), true
		}
	}
	return 0, false
}

// roomOffline says whether the room of a system reads offline on the platform:
// a codec or a bar in it that a scripted problem has unplugged.
func (d *Device) roomOffline(system string) bool {
	if !d.calendarSrc().HasProblems() && (d.arbSrc == nil || len(d.arbSrc().Sources()) == 0) {
		return false // the scan is per room every tick: skip it where nothing impairs a device
	}
	for _, l := range d.world().Listed(d.Hostname) {
		if l.System != system || (l.Type != "codec" && l.Type != "bar") {
			continue
		}
		if _, down := d.unpluggedSince(l); down {
			return true
		}
	}
	return false
}
