import { Show, type JSX } from "solid-js";

// A system's name with its address beside it (#2757), the one rendering of the
// pair (lib/system_name.ts decides the address). The name reads as it always
// has; the address is context, muted and set off by a middle dot, and it gives
// way first when the row is narrow. A system with no address to add (named for
// its room, or placed nowhere) is its name alone.
export default function SystemName(props: { label: string; address?: string; class?: string }): JSX.Element {
  return (
    <span data-testid="system-name" class={`inline-flex min-w-0 items-baseline gap-1.5 ${props.class ?? ""}`}>
      <span data-system-label class="truncate">{props.label}</span>
      <Show when={props.address}>
        {(a) => (
          <>
            <span aria-hidden="true" class="flex-none text-base-content/30">·</span>
            <span data-system-address class="min-w-0 shrink-[999] truncate text-[0.85em] text-base-content/55">{a()}</span>
          </>
        )}
      </Show>
    </span>
  );
}
