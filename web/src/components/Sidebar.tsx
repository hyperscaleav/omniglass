import { For, Show, createMemo } from "solid-js";
import { A, useLocation, useNavigate } from "@solidjs/router";
import { navItems, filterNav, type NavItem } from "../lib/nav";
import { useMe, useLogout, can } from "../lib/auth";
import { PanelLeft, LogOut } from "./icons";
import { BrandMark, Wordmark } from "./Brand";
import Button from "./Button";

// The navigation rail: a daisyUI `menu` with collapsible clusters, the brand
// lockup, a collapse toggle, and an identity footer. Routing and active state go
// through the Solid router (`<A>` with activeClass), styled by daisyUI.
const BASE = "/web";

export default function Sidebar(props: { collapsed: boolean; onToggle: () => void }) {
  const location = useLocation();
  const navigate = useNavigate();
  const me = useMe();
  const logout = useLogout();
  const rel = () => {
    const p = location.pathname.startsWith(BASE) ? location.pathname.slice(BASE.length) : location.pathname;
    return p === "" ? "/" : p;
  };
  const ident = () => {
    const m = me.data;
    if (!m) return { name: "—", role: "" };
    // Prefer the display name (falls through an empty one to the username), so
    // setting it updates both the label and the initials avatar below.
    const name = m.human?.display_name || m.human?.username || m.service?.label || m.principal.kind;
    return { name, role: m.grants[0]?.role ?? m.principal.kind };
  };
  // The tabs this principal may see: the ungated ones (Home, Explore, Learn, and
  // the still-stubbed sections) plus those whose required permission it holds (a
  // bare <resource>:read, or an explicit tier like audit:read:admin).
  const items = createMemo(() => filterNav(navItems, (tokens) => can(me.data, ...tokens)));

  return (
    <aside
      class="sticky top-0 flex h-screen flex-none flex-col border-r border-base-300 bg-base-200 transition-[width] duration-200"
      classList={{ "w-16": props.collapsed, "w-60": !props.collapsed }}
    >
      {/* The brand mark stays in this h-14 row in both states (vertically centered,
          horizontally centered when collapsed to match the nav icons): collapsing
          only drops the wordmark, never moves the mark. The toggle moves to its own
          row below when collapsed, since the mark and toggle cannot share the w-16. */}
      <div class="flex h-14 items-center gap-2" classList={{ "justify-center": props.collapsed, "justify-between px-4 pr-2": !props.collapsed }}>
        <Show when={!props.collapsed} fallback={<BrandMark />}><Lockup /></Show>
        <Show when={!props.collapsed}>
          <Button square icon={PanelLeft} onClick={props.onToggle} title="Collapse" label="Toggle sidebar" class="text-base-content/50" />
        </Show>
      </div>
      <Show when={props.collapsed}>
        <div class="flex justify-center pb-1">
          <Button square icon={PanelLeft} onClick={props.onToggle} title="Expand" label="Toggle sidebar" class="text-base-content/50" />
        </div>
      </Show>

      <ul class="menu min-h-0 w-full flex-1 flex-nowrap gap-0.5 overflow-y-auto [&_li>*]:rounded-field">
        <For each={items()}>
          {(item) => (
            <Show when={item.children} fallback={<Leaf item={item} collapsed={props.collapsed} />}>
              <Group item={item} rel={rel()} collapsed={props.collapsed} />
            </Show>
          )}
        </For>
      </ul>

      <div class="border-t border-base-300 p-3">
        <div class="flex items-center gap-2.5" classList={{ "justify-center": props.collapsed }}>
          <A
            href="/profile"
            class="flex min-w-0 items-center gap-2.5 rounded-field hover:bg-base-300"
            classList={{ "flex-1 p-1 -m-1": !props.collapsed, "tooltip tooltip-right": props.collapsed }}
            data-tip={props.collapsed ? "Your profile" : undefined}
            title="Your profile"
          >
            <div class="avatar avatar-placeholder">
              <div class="w-7 rounded-full bg-linear-to-br from-primary to-info text-primary-content">
                <span class="font-data text-[11px] font-bold uppercase">{ident().name.slice(0, 2)}</span>
              </div>
            </div>
            <Show when={!props.collapsed}>
              <div class="min-w-0 flex-1 leading-tight">
                <div class="truncate font-data text-xs font-semibold">{ident().name}</div>
                <div class="text-[11px] capitalize text-base-content/40">{ident().role}</div>
              </div>
            </Show>
          </A>
          <Show when={!props.collapsed}>
            <Button
              square
              icon={LogOut}
              title="Sign out"
              label="Sign out"
              class="flex-none text-base-content/50"
              onClick={async () => {
                await logout();
                navigate("/login");
              }}
            />
          </Show>
        </div>
      </div>
    </aside>
  );
}

// Soon: the marker on a nav item whose backend has not landed. The item stays
// navigable (its stub page explains what is coming); it just reads as pending.
function Soon() {
  return <span class="ml-auto flex-none rounded bg-base-content/5 px-1 py-px text-[9px] font-semibold uppercase tracking-wider text-base-content/40">soon</span>;
}

function Leaf(props: { item: NavItem; collapsed: boolean }) {
  const Icon = props.item.icon;
  const live = () => props.item.live;
  return (
    <li>
      <A
        href={props.item.path!}
        end={props.item.path === "/"}
        activeClass="menu-active"
        class="gap-3"
        classList={{ "tooltip tooltip-right justify-center": props.collapsed, "opacity-45": !live() }}
        data-tip={props.collapsed ? (live() ? props.item.label : `${props.item.label} · soon`) : undefined}
      >
        <Icon size={17} />
        <Show when={!props.collapsed}>
          <span class="flex-1 truncate">{props.item.label}</span>
          <Show when={!live()}><Soon /></Show>
        </Show>
      </A>
    </li>
  );
}

function Group(props: { item: NavItem; rel: string; collapsed: boolean }) {
  const Icon = props.item.icon;
  // Children are already permission-filtered by filterNav upstream.
  const children = () => props.item.children ?? [];
  const childActive = () => children().some((c) => props.rel === c.path);
  return (
    <Show
      when={!props.collapsed}
      fallback={
        <li>
          <A href={children()[0].path} class="tooltip tooltip-right justify-center" classList={{ "menu-active": childActive() }} data-tip={props.item.label}>
            <Icon size={17} />
          </A>
        </li>
      }
    >
      <li>
        <details open>
          <summary class="gap-3"><Icon size={17} />{props.item.label}</summary>
          {/* Keep the submenu guide line dropping straight down the icon column
              (the margin positions daisyUI's ::before rule under the icon center),
              and pad so child labels land on the parent-label rail at 49px. */}
          <ul class="ms-5 ps-2.25">
            <For each={children()}>
              {(c) => (
                <li>
                  <A href={c.path} activeClass="menu-active" classList={{ "opacity-45": !c.live }}>
                    <span class="flex-1 truncate">{c.label}</span>
                    <Show when={!c.live}><Soon /></Show>
                  </A>
                </li>
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
      <Wordmark class="text-lg" />
    </div>
  );
}
