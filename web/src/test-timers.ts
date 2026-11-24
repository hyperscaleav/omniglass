// Timers that outlive their test (#2637). Kobalte's tooltip keeps module-level
// timers (the skip delay, the warm-up and cool-down) on window.setTimeout, and
// one still pending when its file ends fires after the environment is torn
// down: "window is not defined", an unhandled error that fails the whole run
// though no assertion failed. The setup tracks every pending timeout and sweeps
// them when a test ends, so none outlives its test, rather than silencing
// unhandled errors.
const pending = new Set<ReturnType<typeof setTimeout>>();

/** Wrap setTimeout so each pending timeout is known until it fires or is cleared. */
export function trackTimeouts(): void {
  const real = globalThis.setTimeout;
  globalThis.setTimeout = ((fn: (...a: unknown[]) => void, ms?: number, ...args: unknown[]) => {
    const id: ReturnType<typeof setTimeout> = real(() => {
      pending.delete(id);
      fn(...args);
    }, ms);
    pending.add(id);
    return id;
  }) as typeof setTimeout;
}

/** How many timeouts are pending now. */
export const pendingTimeouts = (): number => pending.size;

/** Clear every pending timeout. */
export function sweepTimeouts(): void {
  for (const id of pending) clearTimeout(id);
  pending.clear();
}
