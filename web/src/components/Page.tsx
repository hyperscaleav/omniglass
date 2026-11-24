import type { JSX } from "solid-js";
import { createEffect, createRenderEffect, onCleanup, Show } from "solid-js";
import { namePage, setPageHeader } from "../lib/handover";
import { navByPath } from "../lib/nav";
import type { Crumb } from "../lib/breadcrumb";

// Page scaffold: its title (string or element) and hint go to the top bar, the
// page's one header row, with the trail above a detail page's title; optional
// right-aligned actions, then the page body, stacked with the density gap. Inventory pages built on TreeList drop the H1 (the top bar
// already labels the page) and pass their own header in children. A detail
// page passes its entity's name or address, which omnibot is handed as the
// page in place of the uuid its route holds (#2162).
export default function Page(props: {
  // A route (base-relative, e.g. "/roles") fills the title and the one-line hint
  // from the nav metadata, so the page says what the sidebar says; an explicit
  // title or subtitle wins.
  route?: string;
  // A page whose body fills the shell's height (a list in fill mode): the section takes
  // the full height and the body the rest under the header.
  fill?: boolean;
  title?: string | JSX.Element;
  name?: string;
  subtitle?: string;
  // A detail page's path (each crumb a link), read by the top bar before the h1.
  trail?: Crumb[];
  actions?: JSX.Element;
  children: JSX.Element;
}) {
  const meta = () => (props.route ? navByPath[props.route] : undefined);
  const title = () => props.title ?? meta()?.label ?? "";
  const subtitle = () => props.subtitle ?? meta()?.hint;
  createEffect(() => namePage(props.name));
  onCleanup(() => namePage(undefined));
  // The top bar is the page header (#2575): the title and hint go there.
  createRenderEffect(() => setPageHeader({ title: title(), hint: subtitle(), trail: props.trail }));
  onCleanup(() => setPageHeader(undefined));
  return (
    <section class="og-stack flex flex-col" classList={{ "h-full min-h-0": props.fill }}>
      <Show when={props.actions}>
        <div class="flex flex-wrap items-center justify-end gap-2">{props.actions}</div>
      </Show>
      {props.children}
    </section>
  );
}
