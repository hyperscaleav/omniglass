import { api } from "../api/client";
import type { ComponentDependencyBody, ListComponentDependenciesOutputBody, ReachGateReasonBody } from "../api/api.gen";
import { entityLabel } from "./entities";
import type { Component } from "./components";
import type { Reachability } from "./reachability";

// The component dependencies data layer (#2428, #1648): thin typed wrappers
// over the generated client plus the pure derivations the panel renders.
//
// A dependency edge says a component needs another (its UPSTREAM) by power
// (fed from one of the upstream's outlets), host (hosted by it) or path
// (reached through it). It is a fact about the dependent, so a write costs
// component:update on it. The list answers both ways in one read: the
// upstreams of the component and its dependents (the downstream).

export type Dependency = ComponentDependencyBody;
export type Dependencies = ListComponentDependenciesOutputBody;
export type DependencyKind = Dependency["kind"];

export const DEPENDENCY_KINDS: { kind: DependencyKind; label: string; hint: string }[] = [
  { kind: "power", label: "Power", hint: "Fed from one of the upstream's outlets, so cycling that outlet holds this component." },
  { kind: "host", label: "Host", hint: "Hosted by the upstream, so a reboot of the upstream holds this component's endpoints too." },
  { kind: "path", label: "Path", hint: "Reached through the upstream, so while the upstream's gate is closed this component's in-band endpoints are held." },
];

export const kindLabel = (k: string) => DEPENDENCY_KINDS.find((d) => d.kind === k)?.label ?? k;

export const dependenciesKey = (component: string) => ["component-dependencies", component] as const;

export async function listDependencies(component: string): Promise<Dependencies> {
  const { data, error } = await api.GET("/components/{name}/dependencies", { params: { path: { name: component } } });
  if (error) throw error;
  return data as Dependencies;
}

export async function addDependency(component: string, upstream: string, kind: DependencyKind, outlet?: string): Promise<Dependency> {
  const { data, error } = await api.POST("/components/{name}/dependencies", {
    params: { path: { name: component } },
    body: { upstream, kind, ...(kind === "power" && outlet ? { outlet } : {}) },
  });
  if (error) throw error;
  return data as Dependency;
}

export async function removeDependency(component: string, kind: DependencyKind, upstream: string): Promise<void> {
  const { error } = await api.DELETE("/components/{name}/dependencies/{kind}/{upstream}", {
    params: { path: { name: component, kind, upstream } },
  });
  if (error) throw error;
}

// refusalWords is a refused write in the words the panel shows in place: a
// 422 carries the server's own reason (a cycle, a self-reference, a missing
// outlet), a 409 an edge that is already recorded, a 404 an upstream gone or
// outside what the caller reads, a 403 a component outside its update scope.
export function refusalWords(e: unknown): string {
  const o = e as { status?: number; detail?: string; title?: string } | null | undefined;
  const why = o?.detail ?? o?.title ?? "The operation failed.";
  switch (o?.status) {
    case 403:
      return `Not allowed: ${why}`;
    case 404:
      return "Not found: that component is gone, or outside what you can see.";
    case 409:
      return why;
    case 422:
      return `Refused: ${why}`;
    default:
      return why;
  }
}

// dependencyChoices is what the add picker offers: the components matching the
// filter (label, name or product, case-insensitive), never the component
// itself (a self-reference is refused), each sorted by what it reads as. The
// server stays the authority on a cycle; the picker does not try to predict
// one.
export function dependencyChoices(components: Component[], selfId: string, query: string): Component[] {
  const q = query.trim().toLowerCase();
  return components
    .filter((c) => c.id !== selfId && c.name !== selfId)
    .filter((c) => !q || `${entityLabel(c)} ${c.name} ${c.product ?? ""}`.toLowerCase().includes(q))
    .sort((a, b) => entityLabel(a).localeCompare(entityLabel(b)));
}

// upstreamHolds reads which components currently hold this one, from the
// reachability read of the component itself: each endpoint's gate carries an
// upstream reason whose source is the uuid of the component holding it (the
// root of the chain). Keyed by that uuid; the value is the holder's name when
// the caller may read it.
export function upstreamHolds(reach: Reachability | undefined): Map<string, string> {
  const out = new Map<string, string>();
  for (const ep of reach?.endpoints ?? []) {
    const reasons = (ep as { gate_reasons?: ReachGateReasonBody[] | null }).gate_reasons ?? [];
    for (const r of reasons) {
      if (r.kind === "upstream" && r.source) out.set(r.source, r.component ?? "");
    }
  }
  return out;
}

// edgeKey is a row's stable handle across polls.
export const edgeKey = (d: Pick<Dependency, "component_id" | "upstream_id" | "kind">) => `${d.kind}:${d.component_id}:${d.upstream_id}`;

// A downstream component, once, with every kind of edge it holds on this one:
// a panel both hosted by and reached through a bar is one dependent with two
// kinds, not two rows. First-seen order is kept (the server orders by kind,
// then name).
export type Dependent = { id: string; name: string; kinds: DependencyKind[]; edges: Dependency[] };

export function groupDependents(edges: Dependency[]): Dependent[] {
  const out = new Map<string, Dependent>();
  for (const e of edges) {
    const hit = out.get(e.component_id);
    if (hit) {
      hit.kinds.push(e.kind);
      hit.edges.push(e);
    } else {
      out.set(e.component_id, { id: e.component_id, name: e.component, kinds: [e.kind], edges: [e] });
    }
  }
  return [...out.values()].sort((a, b) => a.name.localeCompare(b.name));
}
