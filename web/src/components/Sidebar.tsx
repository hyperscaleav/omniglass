import { For, Show, createSignal } from "solid-js";
import { useLocation, useNavigate } from "@solidjs/router";
import { navItems, type NavItem, type NavChild } from "../lib/nav";
import { useMe } from "../lib/auth";
import { ChevronDown, PanelLeft } from "./icons";

// The navigation rail from the design: brand lockup, a collapsible toggle, the
// nav tree (leaves and collapsible clusters), and an identity footer. Active
// state and routing go through the Solid router; styling mirrors the mockup via
// the theme.css tokens.
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
    const name = m.human?.username ?? m.service?.label ?? m.principal.kind;
    const role = m.grants[0]?.role ?? m.principal.kind;
    return { name, role };
  };

  return (
    <aside
      style={{
        position: "sticky", top: 0, height: "100vh", display: "flex", "flex-direction": "column",
        "border-right": "1px solid var(--line)", background: "var(--raised)",
        width: props.collapsed ? "64px" : "236px", flex: "none", transition: "width .2s ease",
      }}
    >
      <div style={{ display: "flex", "align-items": "center", height: "56px", padding: props.collapsed ? "12px 0 0" : "0 8px 0 16px", gap: "8px", "justify-content": props.collapsed ? "center" : "space-between", "flex-direction": props.collapsed ? "column" : "row" }}>
        <Show when={!props.collapsed} fallback={<BrandMark />}>
          <Lockup />
        </Show>
        <button class="btn btn-ghost btn-sm btn-icon" onClick={props.onToggle} title={props.collapsed ? "Expand" : "Collapse"} aria-label="Toggle sidebar" style={{ color: "var(--text-faint)" }}>
          <PanelLeft size={16} />
        </button>
      </div>

      <nav style={{ flex: 1, "min-height": 0, "overflow-y": "auto", padding: "4px 8px", display: "flex", "flex-direction": "column", gap: "2px" }}>
        <For each={navItems}>
          {(item) => (
            <Show when={item.children} fallback={<NavRow item={item} active={rel() === item.path} collapsed={props.collapsed} />}>
              <NavGroup item={item} rel={rel()} collapsed={props.collapsed} />
            </Show>
          )}
        </For>
      </nav>

      <div style={{ "border-top": "1px solid var(--line)", padding: "10px" }}>
        <div style={{ display: "flex", "align-items": "center", gap: "10px", padding: props.collapsed ? 0 : "4px 6px", "justify-content": props.collapsed ? "center" : "flex-start" }}>
          <span class="mono" style={{ width: "28px", height: "28px", "border-radius": "99px", background: "linear-gradient(135deg, var(--primary), var(--info))", color: "#04201d", display: "inline-flex", "align-items": "center", "justify-content": "center", "font-size": "11.5px", "font-weight": 700, flex: "none", "text-transform": "uppercase" }}>
            {ident().name.slice(0, 2)}
          </span>
          <Show when={!props.collapsed}>
            <div style={{ "min-width": 0, "line-height": 1.3 }}>
              <div class="mono" style={{ "font-size": "12.5px", "font-weight": 600, overflow: "hidden", "text-overflow": "ellipsis" }}>{ident().name}</div>
              <div style={{ "font-size": "11px", color: "var(--text-faint)", "text-transform": "capitalize" }}>{ident().role}</div>
            </div>
          </Show>
        </div>
      </div>
    </aside>
  );
}

function Lockup() {
  return (
    <div style={{ display: "flex", "align-items": "center", gap: "9px", "min-width": 0 }}>
      <BrandMark />
      <span class="mono" style={{ "font-size": "18px", "font-weight": 700, "letter-spacing": "-0.02em" }}>
        <span style={{ color: "var(--text)" }}>omni</span><span style={{ color: "var(--primary)" }}>glass</span>
      </span>
    </div>
  );
}

function BrandMark() {
  return (
    <svg width="26" height="26" viewBox="0 0 160 160" style={{ flex: "none" }} aria-hidden="true">
      <line x1="80" y1="22" x2="24" y2="128" stroke="var(--primary)" stroke-linecap="round" stroke-width="9" />
      <line x1="80" y1="22" x2="136" y2="128" stroke="var(--primary)" stroke-linecap="round" stroke-width="9" />
      <line x1="24" y1="128" x2="136" y2="128" stroke="var(--primary)" stroke-linecap="round" stroke-width="9" />
      <circle cx="80" cy="93" fill="var(--primary)" r="11" />
    </svg>
  );
}

function NavRow(props: { item: NavItem; active: boolean; collapsed: boolean }) {
  const navigate = useNavigate();
  const Icon = props.item.icon;
  return (
    <button
      onClick={() => navigate(props.item.path!)}
      title={props.collapsed ? props.item.label : undefined}
      style={rowStyle(props.active, props.collapsed)}
    >
      <Icon size={17} />
      <Show when={!props.collapsed}><span style={{ flex: 1, "text-align": "left" }}>{props.item.label}</span></Show>
    </button>
  );
}

function NavGroup(props: { item: NavItem; rel: string; collapsed: boolean }) {
  const navigate = useNavigate();
  const [open, setOpen] = createSignal(true);
  const childActive = () => (props.item.children ?? []).some((c) => props.rel === c.path);
  const Icon = props.item.icon;

  return (
    <Show
      when={!props.collapsed}
      fallback={
        <button onClick={() => navigate(props.item.children![0].path)} title={props.item.label} style={rowStyle(childActive(), true)}>
          <Icon size={17} />
        </button>
      }
    >
      <div>
        <button onClick={() => setOpen(!open())} style={{ display: "flex", "align-items": "center", gap: "11px", width: "100%", padding: "8px 10px", "border-radius": "var(--r-field)", border: "none", cursor: "pointer", "font-size": "13.5px", "font-family": "var(--font-ui)", background: "transparent", color: "var(--text-soft)" }}>
          <Icon size={17} />
          <span style={{ flex: 1, "text-align": "left" }}>{props.item.label}</span>
          <span style={{ color: "var(--text-faint)", display: "inline-flex", transform: open() ? "none" : "rotate(-90deg)", transition: "transform .15s ease" }}><ChevronDown size={14} /></span>
        </button>
        <Show when={open()}>
          <ul style={{ "list-style": "none", margin: "1px 0 0", padding: 0, "margin-left": "18px", "border-left": "1px solid var(--line)" }}>
            <For each={props.item.children}>
              {(c: NavChild) => (
                <li>
                  <button
                    onClick={() => navigate(c.path)}
                    style={{
                      display: "block", width: "100%", "text-align": "left", padding: "6px 10px", "margin-left": "1px", border: "none", cursor: "pointer",
                      "font-size": "13px", "font-family": "var(--font-ui)", "border-radius": "var(--r-field)",
                      background: props.rel === c.path ? "color-mix(in oklch, var(--primary) 15%, transparent)" : "transparent",
                      color: props.rel === c.path ? "var(--primary)" : "var(--text-dim)",
                      "font-weight": props.rel === c.path ? 600 : 400,
                    }}
                  >
                    {c.label}
                  </button>
                </li>
              )}
            </For>
          </ul>
        </Show>
      </div>
    </Show>
  );
}

function rowStyle(active: boolean, collapsed: boolean) {
  return {
    display: "flex", "align-items": "center", gap: "11px", width: "100%",
    padding: collapsed ? "9px 0" : "8px 10px", "justify-content": collapsed ? "center" : "flex-start",
    "border-radius": "var(--r-field)", border: "none", cursor: "pointer", "font-size": "13.5px", "font-family": "var(--font-ui)", "text-align": "left" as const,
    background: active ? "color-mix(in oklch, var(--primary) 15%, transparent)" : "transparent",
    color: active ? "var(--primary)" : "var(--text-soft)", "font-weight": active ? 600 : 400, transition: "background .12s ease, color .12s ease",
  };
}
