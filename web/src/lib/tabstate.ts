import { createSignal } from "solid-js";
import { useSearchParams } from "@solidjs/router";

// The detail body's tab state (#2423). Which facet shows is held by the HOST:
// a page keeps it in the URL (`?tab=`, the #763 deep-link rule, the default
// facet carrying no param), a blade in a signal of its own, so a blade over a
// page never rewrites the page's address. The resolver is one for every kind:
// a retired address maps onto the tab that absorbed it, and a bare edit intent
// (`?edit=1`, #800) lands on Configure, whose own hook then begins the edit.

export type TabDef = { key: string; label: string };
export type DetailHost = "page" | "blade";

export type TabState = {
  active: () => string;
  select: (key: string) => void;
};

export type TabOptions = {
  // Retired tab keys and the tab each now lands on.
  legacy?: Record<string, string>;
};

export function resolveTab(tabs: TabDef[], raw: string | undefined, opts: TabOptions & { edit?: string } = {}): string {
  const t = raw ? (opts.legacy?.[raw] ?? raw) : raw;
  if (t && tabs.some((x) => x.key === t)) return t;
  if (opts.edit === "1" && tabs.some((x) => x.key === "configure")) return "configure";
  return tabs[0]?.key ?? "";
}

const first = (v: string | string[] | undefined) => (Array.isArray(v) ? v[0] : v);

function createUrlTabState(tabs: () => TabDef[], opts: TabOptions): TabState {
  const [params, setParams] = useSearchParams();
  return {
    active: () => resolveTab(tabs(), first(params.tab), { ...opts, edit: first(params.edit) }),
    // Choosing a tab also ends an ?edit=1 landing (the intent is consumed
    // once): left in the address it pinned the page to Configure, so the
    // bare address resolved straight back to it on every return.
    select: (key) => setParams({ tab: key === tabs()[0]?.key ? undefined : key, edit: undefined }),
  };
}

function createSignalTabState(tabs: () => TabDef[], opts: TabOptions): TabState {
  const [raw, setRaw] = createSignal<string | undefined>(undefined);
  return {
    active: () => resolveTab(tabs(), raw(), opts),
    select: (key) => setRaw(key),
  };
}

export function createTabState(host: DetailHost, tabs: () => TabDef[], opts: TabOptions = {}): TabState {
  return host === "page" ? createUrlTabState(tabs, opts) : createSignalTabState(tabs, opts);
}
