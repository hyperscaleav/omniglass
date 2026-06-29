import type { JSX } from "solid-js";

// LiveCount: an "N / total" indicator with the teal pulse, signalling the app
// streams live (no manual refresh button).
export default function LiveCount(props: { children: JSX.Element }) {
  return (
    <div class="flex flex-none items-center gap-2 text-xs text-base-content/50">
      <span class="og-pulse inline-block size-2 rounded-full bg-primary" />
      <span class="tnum whitespace-nowrap">{props.children}</span>
    </div>
  );
}
