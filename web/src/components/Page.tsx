import type { JSX } from "solid-js";
import { createEffect, onCleanup, Show } from "solid-js";
import { namePage } from "../lib/handover";

// Page scaffold: an optional breadcrumb slot, a title (string or outpost) + optional
// subtitle and right-aligned actions, then the page body, stacked with the
// density gap. Inventory pages built on TreeList drop the H1 (the top bar
// already labels the page) and pass their own header in children. A detail
// page passes its entity's name or address, which omnibot is handed as the
// page in place of the uuid its route holds (#2162).
export default function Page(props: {
  title: string | JSX.Element;
  name?: string;
  subtitle?: string;
  breadcrumb?: JSX.Element;
  actions?: JSX.Element;
  children: JSX.Element;
}) {
  createEffect(() => namePage(props.name));
  onCleanup(() => namePage(undefined));
  return (
    <section class="og-stack flex flex-col">
      <div data-testid="page-header">
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
