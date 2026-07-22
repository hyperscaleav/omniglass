// The timeline primitive: turning recorded state EDGES into the spans a strip
// renders. Availability (an interface up or down) and health (a system healthy,
// degraded, or in outage) are the same shape underneath: a list of the moments a
// value CHANGED, never a sample, so the stretch between two edges is exactly how
// long that state lasted. Keeping the derivation here means the reachability strip
// and the health strip speak one idiom and cannot drift apart.

// One recorded edge: the moment the value became this.
export type Edge<T extends string = string> = { ts: string; value: T };

// A span is one contiguous stretch in a single state: its share of the window
// (0..1, so a strip renders it as a flex weight) and the wall-clock bounds it
// covers, which is what "how long did this last" reads from.
export type Span<T extends string = string> = { value: T; weight: number; from: number; to: number };

// spans builds the strip from the edges over a window ending at now. Each edge
// opens a span that runs to the next edge (or to now for the last one), weighted by
// its duration. With no edges the current value (when there is one) fills the whole
// strip; its bounds collapse to now rather than inventing a duration nobody
// recorded.
export function spans<T extends string>(edges: Edge<T>[], current: T | null, now: number = Date.now()): Span<T>[] {
  if (edges.length === 0) {
    return current ? [{ value: current, weight: 1, from: now, to: now }] : [];
  }
  const start = new Date(edges[0].ts).getTime();
  const total = Math.max(now - start, 1);
  const out: Span<T>[] = [];
  for (let i = 0; i < edges.length; i++) {
    const from = new Date(edges[i].ts).getTime();
    const to = i + 1 < edges.length ? new Date(edges[i + 1].ts).getTime() : now;
    const weight = Math.max(to - from, 0) / total;
    if (weight > 0) out.push({ value: edges[i].value, weight, from, to });
  }
  return out;
}

// share is the fraction of the window spent in the states the predicate accepts, as
// a whole-number percent. Null when the window holds nothing at all, which is not
// the same as zero.
export function share<T extends string>(list: Span<T>[], match: (v: T) => boolean): number | null {
  if (list.length === 0) return null;
  return Math.round(list.filter((s) => match(s.value)).reduce((a, s) => a + s.weight, 0) * 100);
}

// durationText renders an elapsed span in the coarsest useful unit, which is how an
// operator reads "how long was it down" weeks later. Never more than two units.
export function durationText(ms: number): string {
  const secs = Math.max(Math.round(ms / 1000), 0);
  if (secs < 60) return `${secs}s`;
  const mins = Math.round(secs / 60);
  if (mins < 60) return `${mins}m`;
  const hours = Math.floor(mins / 60);
  if (hours < 24) {
    const rem = mins % 60;
    return rem ? `${hours}h ${rem}m` : `${hours}h`;
  }
  const days = Math.floor(hours / 24);
  const rem = hours % 24;
  return rem ? `${days}d ${rem}h` : `${days}d`;
}
