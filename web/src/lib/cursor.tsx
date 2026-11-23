import { createContext, createSignal, useContext, type Accessor, type JSX } from "solid-js";

// The shared cursor (#2424): one instant on the page's time axis, or none,
// that every chart under a provider reads and any of them sets. Hovering one
// chart (or stepping it with the arrow keys) sets it; each chart then draws
// its crosshair and value at that instant; leaving clears it everywhere.
//
// The API, small on purpose (the overlay timeline, #2425, builds on it):
//
//   Cursor                 { at, set }: at() is the instant in epoch ms or
//                          undefined; set(t) moves it, set(undefined) clears
//   createCursor()         a fresh cursor
//   <CursorProvider>       shares one cursor with everything beneath it
//                          (pass `cursor` to share one you hold); inside
//                          another provider it shares the outer one, so a
//                          page's timeline and its metrics grid read one
//                          instant (#2425)
//   useCursor()            the provider's cursor, else a private one, so a
//                          chart alone on a page still has its crosshair
//
// The cursor holds the pointer's instant, not a sample's: each reader snaps
// it to its own nearest point (nearestByTime in lib/chart.ts), since two
// series are rarely sampled at the same moments.
export type Cursor = { at: Accessor<number | undefined>; set: (t: number | undefined) => void };

export function createCursor(): Cursor {
  const [at, set] = createSignal<number | undefined>(undefined);
  return { at, set: (t) => set(t) };
}

const CursorContext = createContext<Cursor>();

export function CursorProvider(props: { cursor?: Cursor; children: JSX.Element }): JSX.Element {
  const cursor = props.cursor ?? useContext(CursorContext) ?? createCursor();
  return <CursorContext.Provider value={cursor}>{props.children}</CursorContext.Provider>;
}

export function useCursor(): Cursor {
  return useContext(CursorContext) ?? createCursor();
}
