// Package behave is the shared, typed behavior of a simulated room's devices
// (#2830, epic #2826): each primitive is a pure function of the room's state
// (calendar.StateAt, or the engine's memo of it), the device's previous typed
// state, typed parameters and the instant, and returns the device's next typed
// state. A device family composes the primitives it needs; none of them knows
// a profile, a protocol or the family registry's Spec, so they take and return
// plain typed values.
//
// Four rules hold for every primitive:
//
//   - Deterministic. The output is a function of its inputs. Randomness comes
//     only through the Source passed in (one per device, seeded from its id),
//     and a primitive draws exactly as the profile behavior it replaces did,
//     in the same order, so a device that moves to Go keeps its seeded run.
//   - No calendar, no change. A device whose room has no calendar
//     (RoomState.HasCalendar false) keeps the behavior it had before it read
//     one: each primitive carries that fallback, never the caller.
//   - No magic numbers. Every rate, band and threshold is a field of a typed
//     parameter struct whose Default value is the number the profiles use
//     today; the room's own timing (wake lead, cooldown, sharing stretch)
//     arrives already resolved in the RoomState from the meeting knobs.
//   - No I/O. The clock is the instant argument; the calendar is read only by
//     the few readers that say so (BookingsAt, SpansBetween, BuildingSpan).
package behave
