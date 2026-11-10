import { For, Show, createMemo, type Accessor, type JSX } from "solid-js";

// KeyedFor renders a list by a key of each item, not by the item itself.
// Solid's For keeps a row while its item is the same object, but a polled read
// hands back fresh objects every time, so a For over them rebuilds every row on
// every poll: an expanded row closes, a tooltip vanishes under the pointer, a
// disclosure the operator closed opens again. KeyedFor keeps a row mounted for
// as long as its key is in the list and hands it the key's newest item, so the
// row's own state survives the poll and its figures follow it.
export default function KeyedFor<T extends object>(props: {
  each: readonly T[] | null | undefined;
  by: (item: T) => string;
  fallback?: JSX.Element;
  children: (item: Accessor<T>, index: Accessor<number>) => JSX.Element;
}) {
  const items = () => props.each ?? [];
  const keys = createMemo(() => items().map(props.by), undefined, {
    equals: (a, b) => a.length === b.length && a.every((k, i) => k === b[i]),
  });
  const byKey = createMemo(() => new Map(items().map((it) => [props.by(it), it])));
  return (
    <For each={keys()} fallback={props.fallback}>
      {(key, index) => <Show when={byKey().get(key)}>{(item) => props.children(item, index)}</Show>}
    </For>
  );
}
