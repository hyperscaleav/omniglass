import type { JSX } from "solid-js";
import { Show } from "solid-js";

// Page scaffold: an optional breadcrumb slot, a title (string or node) + optional
// subtitle and right-aligned actions, then the page body, stacked with the
// density gap. Inventory pages built on ListView drop the H1 (the top bar
// already labels the page) and pass their own header in children.
export default function Page(props: {
  title: string | JSX.Element;
  subtitle?: string;
  breadcrumb?: JSX.Element;
  actions?: JSX.Element;
  children: JSX.Element;
}) {
  return (
    <section class="og-stack flex flex-col">
      <div>
        <Show when={props.breadcrumb}>
          <div class="mb-1 min-h-[18px]">{props.breadcrumb}</div>
        </Show>
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
      </div>
      {props.children}
    </section>
  );
}
