import type { JSX } from "solid-js";
import { Show } from "solid-js";

// Page scaffold: a title + optional subtitle and right-aligned actions, then the
// page body, stacked with the density gap.
export default function Page(props: {
  title: string;
  subtitle?: string;
  actions?: JSX.Element;
  children: JSX.Element;
}) {
  return (
    <section class="og-stack flex flex-col">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div class="min-w-0">
          <h1 class="text-2xl font-semibold tracking-tight">{props.title}</h1>
          <Show when={props.subtitle}>
            <p class="mt-1 max-w-2xl text-sm text-base-content/60">{props.subtitle}</p>
          </Show>
        </div>
        <Show when={props.actions}>
          <div class="flex flex-none items-center gap-2">{props.actions}</div>
        </Show>
      </div>
      {props.children}
    </section>
  );
}
