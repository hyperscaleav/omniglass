import { For, Show } from "solid-js";
import { crumbLayout, type Crumb } from "../lib/breadcrumb";

// The breadcrumb primitive (#633), filling Page.tsx's breadcrumb slot: a
// trail whose last two crumbs never shrink while every earlier one may
// ellipsise to a stub. The rule is crumbLayout's arithmetic, pinned at the
// unit tier; this component only applies the styles. The fleet zoom renders
// a one-crumb trail; the later zooms extend the same trail with the ancestor
// chain.
export default function Breadcrumb(props: { crumbs: Crumb[] }) {
  const styles = () => crumbLayout(props.crumbs.length);
  return (
    <nav aria-label="Breadcrumb" data-testid="breadcrumb" class="flex min-w-0 items-center gap-1 text-xs text-base-content/60">
      <For each={props.crumbs}>
        {(crumb, i) => (
          <>
            <Show when={i() > 0}>
              <span class="flex-none text-base-content/30">/</span>
            </Show>
            <span
              class="truncate"
              style={styles()[i()]}
              title={crumb.title ?? crumb.label}
            >
              <Show when={crumb.onClick} fallback={crumb.label}>
                <button type="button" class="cursor-pointer truncate hover:text-base-content" onClick={crumb.onClick}>
                  {crumb.label}
                </button>
              </Show>
            </span>
          </>
        )}
      </For>
    </nav>
  );
}
