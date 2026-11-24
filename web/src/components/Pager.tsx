import { Show } from "solid-js";
import Button from "./Button";
import { ChevronLeft, ChevronRight } from "./icons";
import { pageCount } from "../lib/platform_sections";

// Pager: the previous and next steps and the range a bounded list shows
// ("26-50 of 201"), for a table that renders one page of rows rather than all
// of them (#2446). It owns no state: the caller holds the zero-based page and
// slices its own list (pageSlice), so the list stays a pure function of it.
// With one page it shows the count and no steps.
export default function Pager(props: { total: number; page: number; size: number; onPage: (page: number) => void; label?: string }) {
  const last = () => pageCount(props.total, props.size) - 1;
  const page = () => Math.min(Math.max(props.page, 0), last());
  const from = () => (props.total === 0 ? 0 : page() * props.size + 1);
  const to = () => Math.min(props.total, (page() + 1) * props.size);
  return (
    <nav aria-label={props.label ?? "Pages"} class="flex items-center gap-2 text-xs text-base-content/60">
      <span class="tnum" data-testid="pager-range">{props.total === 0 ? "0" : `${from()}-${to()}`} of {props.total}</span>
      <Show when={last() > 0}>
        <Button square size="xs" icon={ChevronLeft} label="Previous page" disabled={page() === 0} onClick={() => props.onPage(page() - 1)} />
        <Button square size="xs" icon={ChevronRight} label="Next page" disabled={page() >= last()} onClick={() => props.onPage(page() + 1)} />
      </Show>
    </nav>
  );
}
