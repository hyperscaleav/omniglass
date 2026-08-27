import { For, Show } from "solid-js";
import { useSearchParams } from "@solidjs/router";

// TabRail: the workspace's facet switcher (#790). A facet is a URL fact
// (`?tab=<key>`, the #763 deep-link rule), so a reload or a pasted link lands
// on the same view; the default facet carries no param at all, keeping the
// canonical address clean. One facet is no workspace: the rail renders only
// once there are two.
// The rail's URL fact defaults to `?tab=`; a caller whose facet is a different
// noun names it (the fleet list's kind tabs ride `?kind=`, #798).
export default function TabRail(props: { tabs: { key: string; label: string }[]; param?: string; activeKey?: () => string }) {
  const [params, setParams] = useSearchParams();
  const name = () => props.param ?? "tab";
  const active = () => {
    // A page whose default facet is DERIVED (the ?edit=1 landing on
    // Configure, #800) passes its own resolver, so the rail and the body
    // cannot disagree about which facet is showing.
    if (props.activeKey) return props.activeKey();
    const raw = params[name()];
    const t = Array.isArray(raw) ? raw[0] : raw;
    return t && props.tabs.some((x) => x.key === t) ? t : props.tabs[0]?.key;
  };
  return (
    <Show when={props.tabs.length > 1}>
      <div data-testid="tab-rail" role="tablist" class="flex flex-wrap gap-1 border-b border-base-300 px-4 pt-2">
        <For each={props.tabs}>
          {(t) => (
            <button
              type="button"
              role="tab"
              aria-selected={active() === t.key ? "true" : "false"}
              class="cursor-pointer rounded-t-md px-3 py-1.5 text-sm"
              classList={{
                "border border-b-0 border-base-300 bg-base-100 font-medium": active() === t.key,
                "text-base-content/60 hover:text-base-content": active() !== t.key,
              }}
              onClick={() => setParams({ [name()]: t.key === props.tabs[0].key ? undefined : t.key })}
            >
              {t.label}
            </button>
          )}
        </For>
      </div>
    </Show>
  );
}
