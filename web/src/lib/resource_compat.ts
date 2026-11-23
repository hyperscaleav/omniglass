// SPIKE (#2290): a 1.x-shaped createResource over Solid 2 primitives, so the seven
// call sites boot without a Loading-boundary redesign. Not for merge as is.
import { createEffect, createSignal, type Accessor } from "solid-js";

type Fetcher<S, T> = (src: S) => Promise<T> | T;
export function createResource<T, S = true>(
  a: (() => S | false | null | undefined) | Fetcher<true, T>,
  b?: Fetcher<S, T>,
): [Accessor<T | undefined> & { loading: boolean; error: unknown }, { refetch: () => void; mutate: (v: T | undefined) => void }] {
  const source = (b ? a : () => true) as () => S | false | null | undefined;
  const fetcher = (b ?? a) as Fetcher<S, T>;
  const [value, setValue] = createSignal<T | undefined>(undefined, { ownedWrite: true });
  const [loading, setLoading] = createSignal(false, { ownedWrite: true });
  const [error, setError] = createSignal<unknown>(undefined, { ownedWrite: true });
  const [tick, setTick] = createSignal(0);
  createEffect(
    () => [source(), tick()] as const,
    ([src]) => {
      if (src === false || src === null || src === undefined) return;
      setLoading(true);
      Promise.resolve(fetcher(src as S)).then(
        (v) => { setValue(() => v); setLoading(false); },
        (e) => { setError(e); setLoading(false); },
      );
    },
  );
  const read = (() => value()) as Accessor<T | undefined> & { loading: boolean; error: unknown };
  Object.defineProperty(read, "loading", { get: loading });
  Object.defineProperty(read, "error", { get: error });
  return [read, { refetch: () => setTick((n) => n + 1), mutate: (v) => setValue(() => v) }];
}
