import { For, Show } from "solid-js";
import { A, useLocation } from "@solidjs/router";
import { navItems, type NavItem } from "../lib/nav";
import { useMe } from "../lib/auth";
import { PanelLeft } from "./icons";

// The navigation rail: a daisyUI `menu` with collapsible clusters, the brand
// lockup, a collapse toggle, and an identity footer. Routing and active state go
// through the Solid router (`<A>` with activeClass), styled by daisyUI.
const BASE = "/web";

export default function Sidebar(props: { collapsed: boolean; onToggle: () => void }) {
  const location = useLocation();
  const me = useMe();
  const rel = () => {
    const p = location.pathname.startsWith(BASE) ? location.pathname.slice(BASE.length) : location.pathname;
    return p === "" ? "/" : p;
  };
  const ident = () => {
    const m = me.data;
    if (!m) return { name: "—", role: "" };
    return { name: m.human?.username ?? m.service?.label ?? m.principal.kind, role: m.grants[0]?.role ?? m.principal.kind };
  };

  return (
    <aside
      class="sticky top-0 flex h-screen flex-none flex-col border-r border-base-300 bg-base-200 transition-[width] duration-200"
      classList={{ "w-16": props.collapsed, "w-60": !props.collapsed }}
    >
      <div class="flex h-14 items-center gap-2" classList={{ "flex-col pt-3 justify-center": props.collapsed, "justify-between px-4 pr-2": !props.collapsed }}>
        <Show when={!props.collapsed} fallback={<BrandMark />}><Lockup /></Show>
        <button class="btn btn-ghost btn-sm btn-square text-base-content/50" onClick={props.onToggle} title={props.collapsed ? "Expand" : "Collapse"} aria-label="Toggle sidebar">
          <PanelLeft size={16} />
        </button>
      </div>

      <ul class="menu min-h-0 w-full flex-1 flex-nowrap gap-0.5 overflow-y-auto [&_li>*]:rounded-field">
        <For each={navItems}>
          {(item) => (
            <Show when={item.children} fallback={<Leaf item={item} collapsed={props.collapsed} />}>
              <Group item={item} rel={rel()} collapsed={props.collapsed} />
            </Show>
          )}
        </For>
      </ul>

      <div class="border-t border-base-300 p-3">
        <div class="flex items-center gap-2.5" classList={{ "justify-center": props.collapsed }}>
          <div class="avatar avatar-placeholder">
            <div class="w-7 rounded-full bg-gradient-to-br from-primary to-info text-primary-content">
              <span class="font-data text-[11px] font-bold uppercase">{ident().name.slice(0, 2)}</span>
            </div>
          </div>
          <Show when={!props.collapsed}>
            <div class="min-w-0 leading-tight">
              <div class="truncate font-data text-xs font-semibold">{ident().name}</div>
              <div class="text-[11px] capitalize text-base-content/40">{ident().role}</div>
            </div>
          </Show>
        </div>
      </div>
    </aside>
  );
}

function Leaf(props: { item: NavItem; collapsed: boolean }) {
  const Icon = props.item.icon;
  return (
    <li>
      <A href={props.item.path!} end={props.item.path === "/"} activeClass="menu-active" class="gap-3" classList={{ "tooltip tooltip-right justify-center": props.collapsed }} data-tip={props.collapsed ? props.item.label : undefined}>
        <Icon size={17} />
        <Show when={!props.collapsed}><span>{props.item.label}</span></Show>
      </A>
    </li>
  );
}

function Group(props: { item: NavItem; rel: string; collapsed: boolean }) {
  const Icon = props.item.icon;
  const childActive = () => (props.item.children ?? []).some((c) => props.rel === c.path);
  return (
    <Show
      when={!props.collapsed}
      fallback={
        <li>
          <A href={props.item.children![0].path} class="tooltip tooltip-right justify-center" classList={{ "menu-active": childActive() }} data-tip={props.item.label}>
            <Icon size={17} />
          </A>
        </li>
      }
    >
      <li>
        <details open>
          <summary class="gap-3"><Icon size={17} />{props.item.label}</summary>
          <ul>
            <For each={props.item.children}>
              {(c) => (
                <li><A href={c.path} activeClass="menu-active">{c.label}</A></li>
              )}
            </For>
          </ul>
        </details>
      </li>
    </Show>
  );
}

function Lockup() {
  return (
    <div class="flex min-w-0 items-center gap-2.5">
      <BrandMark />
      <span class="og-wordmark font-data text-lg font-bold tracking-tight">omni<span class="text-primary">glass</span></span>
    </div>
  );
}

function BrandMark() {
  return (
    <svg width="26" height="26" viewBox="0 0 160 160" class="flex-none" aria-hidden="true">
      <line x1="80" y1="22" x2="24" y2="128" stroke="var(--color-primary)" stroke-linecap="round" stroke-width="9" />
      <line x1="80" y1="22" x2="136" y2="128" stroke="var(--color-primary)" stroke-linecap="round" stroke-width="9" />
      <line x1="24" y1="128" x2="136" y2="128" stroke="var(--color-primary)" stroke-linecap="round" stroke-width="9" />
      <circle cx="80" cy="93" fill="var(--color-primary)" r="11" />
    </svg>
  );
}
