import { createEffect, createMemo, createSignal, on, untrack } from "solid-js";
import { useSearchParams } from "@solidjs/router";
import type { BladeController } from "./blades";

// An open drawer has an address (#2429). Opening a row on a list page holds the
// row in the URL as `?open=<row address>`, the same address the row's API route
// takes (a name or a uuid, ADR-0062), so a link, a reload and Back all mean what
// they say: the link lands the drawer open, the reload keeps the place, and Back
// closes it. Only the ROOT drawer is addressed; a blade drilled over it keeps its
// own stack and is not in the URL. The param composes with a page's filter, sort
// and edit params (it is one key among them, and writes leave the rest alone).
//
// LINKING TO A DRAWER FROM ANOTHER SURFACE: use `drawerHref(kind, address)`.
//
//   drawerHref("event_rule", "av-offline")  ->  "/event-rules?open=av-offline"
//   drawerHref("user", uuid, { edit: "1" }) ->  "/users?open=<uuid>&edit=1"
//
// `kind` is a key of DRAWER_PAGES (the list page that hosts the entity's drawer),
// `address` is the row's API address, and `extra` carries any further search
// params (not `open`). Never build an `?open=` string by hand.

// The list page that hosts each addressable entity's drawer. The keys are the
// singular entity names; the values are the console routes (pinned against the
// route manifest by drawer_address.test.ts, so a renamed route fails loudly).
export const DRAWER_PAGES = {
  // admin
  user: "/users",
  role: "/roles",
  group: "/user-groups",
  secret: "/secrets",
  variable: "/variables",
  outpost: "/outposts",
  file: "/files",
  alarm: "/alarms",
  // catalog
  metric: "/metrics",
  property: "/properties",
  event_type: "/event-types",
  command_type: "/command-types",
  event_rule: "/event-rules",
  calc_rule: "/calc-rules",
  severity_level: "/severity-levels",
  vendor: "/vendors",
  product: "/products",
  driver: "/drivers",
  component_type: "/component-types",
  standard: "/standards",
  system_type: "/system-types",
  location_type: "/location-types",
  secret_type: "/secret-types",
  tag: "/tags",
  template: "/templates",
  notification_type: "/notifications",
  action_rule: "/action-rules",
  notification_target: "/notification-targets",
} as const;

export type DrawerKind = keyof typeof DRAWER_PAGES;

export const DRAWER_PARAM = "open";

// drawerHref is the link to a list page with one row's drawer open. Pure.
export function drawerHref(kind: DrawerKind, address: string, extra: Record<string, string> = {}): string {
  const q = new URLSearchParams({ [DRAWER_PARAM]: address });
  for (const [k, v] of Object.entries(extra)) if (k !== DRAWER_PARAM) q.set(k, v);
  return `${DRAWER_PAGES[kind]}?${q.toString()}`;
}

// firstParam reads a search param as one string (the router hands an array for a
// repeated key); empty is absent.
export function firstParam(v: string | string[] | undefined): string | undefined {
  const s = Array.isArray(v) ? v[0] : v;
  return s ? s : undefined;
}

type Params = Record<string, string | string[] | undefined>;
type SetParams = (patch: Record<string, string | undefined>, opts?: { replace?: boolean }) => void;

// The URL's search params, or, for a list rendered outside any Router (a unit
// test, an embed), the same shape held in memory: the drawer still opens and
// closes, it just has no address to share.
function useParamsOrLocal(): [Params, SetParams] {
  try {
    const [params, setParams] = useSearchParams();
    return [params, (patch, opts) => setParams(patch, opts)];
  } catch {
    const [local, setLocal] = createSignal<Params>({});
    const proxy = new Proxy({} as Params, { get: (_, k) => local()[k as string], has: (_, k) => (k as string) in local() });
    return [proxy, (patch) => setLocal((cur) => ({ ...cur, ...patch }))];
  }
}

export type DrawerAddress = {
  // The address in the URL, if any.
  address: () => string | undefined;
  // Push the address (a new history entry: Back closes the drawer).
  open: (id: string) => void;
  // Clear it (history replace: the entry the operator is on stops naming a drawer).
  close: () => void;
  // The address when it names no row the caller can see, once the list has
  // loaded; undefined otherwise. Same answer for "missing" and "out of scope".
  miss: () => string | undefined;
};

// useDrawerAddress binds one list's open drawer to the URL.
//
// `exists(id)` answers whether the id names a visible row: true, false, or
// undefined while the list is still loading (so a fresh load never flashes the
// not-found notice). `blades`, when given, is the list's blade stack: the
// address and the stack's root are kept in step in both directions (the address
// opens the root blade, closing the last blade clears the address); nested
// blades above the root are the stack's own business. `legacy` names an older
// bespoke id param (`u`, `g`, `n`) still honored on load and swept on the next
// write, so links minted before #2429 keep working.
export function useDrawerAddress(opts: {
  exists: (id: string) => boolean | undefined;
  blades?: BladeController;
  rootRef?: (id: string) => { kind: string; id: string };
  legacy?: string;
}): DrawerAddress {
  const [params, setParams] = useParamsOrLocal();
  const address = createMemo(() => firstParam(params[DRAWER_PARAM]) ?? (opts.legacy ? firstParam(params[opts.legacy]) : undefined));
  const sweep = (): Record<string, string | undefined> => (opts.legacy ? { [opts.legacy]: undefined } : {});

  const open = (id: string) => setParams({ [DRAWER_PARAM]: id, ...sweep() });
  const close = () => setParams({ [DRAWER_PARAM]: undefined, ...sweep() }, { replace: true });

  const found = createMemo(() => {
    const a = address();
    return a ? opts.exists(a) : undefined;
  });

  // A drawer that was showing and whose row then vanished (deleted under it)
  // closes quietly; only an address that never resolved reads as "not found".
  const [seen, setSeen] = createSignal<string | undefined>();
  createEffect(() => {
    const a = address();
    const f = found();
    if (a && f === true) setSeen(a);
    else if (a && f === false && untrack(seen) === a) {
      setSeen(undefined);
      close();
    }
  });
  const miss = createMemo(() => (found() === false && address() !== seen() ? address() : undefined));

  const b = opts.blades;
  if (b) {
    const ref = (id: string) => opts.rootRef?.(id) ?? { kind: "", id };
    // Address -> stack: the root blade follows the address; nested blades stay.
    createEffect(() => {
      const a = address();
      const f = found();
      untrack(() => {
        if (!a || f === false) {
          if (!a && b.stack().length > 0) b.close();
          return;
        }
        if (f !== true) return;
        const r = ref(a);
        const root = b.stack()[0];
        if (root && root.kind === r.kind && root.id === r.id) return;
        b.close();
        b.push(r);
      });
    });
    // Stack -> address: closing the last blade clears the address. `on`'s prev
    // argument is undefined on a deferred first run, so the last length is kept by hand.
    let lastLen = 0;
    createEffect(
      on(
        () => b.stack().length,
        (n) => {
          const was = lastLen;
          lastLen = n;
          if (n === 0 && was > 0 && address()) close();
        },
      ),
    );
    // A root blade swapped by the stack itself (not the address) re-points it.
    createEffect(
      on(
        () => b.stack()[0],
        (root) => {
          if (root && root.id !== untrack(address)) open(root.id);
        },
        { defer: true },
      ),
    );
  }

  return { address, open, close, miss };
}
