import { createSignal, onCleanup, type Accessor } from "solid-js";

// createClock is a ticking now (#934): the current time, re-read every
// `every` milliseconds for the life of the owning component. An age read
// against it moves with time itself, not with the data: a poll that returns
// the same answer changes nothing a query can see, and a node that has died
// leaves its last answer standing, so only the clock can say it is old.
export function createClock(every: number): Accessor<number> {
  const [now, setNow] = createSignal(Date.now());
  const id = setInterval(() => setNow(Date.now()), every);
  onCleanup(() => clearInterval(id));
  return now;
}

// CLOCK_TICK_MS is the cadence an age on screen moves at: a second, the
// finest grain any age is written in.
export const CLOCK_TICK_MS = 1_000;
