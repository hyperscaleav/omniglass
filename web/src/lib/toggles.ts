import { createSignal, createUniqueId } from "solid-js";

// createToggles is the open state of a list's disclosures, by row key: any
// number open at once, kept outside the rows so a poll that hands back fresh
// row objects does not close one. panelId names the region a row's toggle
// opens (aria-controls), unique per list. The alarm timelines (#950) and a
// multi-endpoint vital's per-endpoint values (#1020) open this way.
export function createToggles(name: string) {
  const prefix = createUniqueId();
  const [opened, setOpened] = createSignal<ReadonlySet<string>>(new Set());
  const isOpen = (key: string) => opened().has(key);
  const toggle = (key: string) =>
    setOpened((prev) => {
      const next = new Set(prev);
      if (!next.delete(key)) next.add(key);
      return next;
    });
  const panelId = (key: string) => `${name}-${prefix}-${key}`;
  return { isOpen, toggle, panelId };
}
