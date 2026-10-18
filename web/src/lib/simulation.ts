import { api } from "../api/client";
import type { components } from "../api/schema.gen";

// The simulation data layer (#890): thin typed wrappers over the generated client
// plus the pure rules the Simulation page and the simulated component drawer share. Shapes are
// the generated ones (api/simfleet.go, api/impairment.go). A simulated component
// is addressed by its component's uuid everywhere here, never its name: the seeded
// fleet reuses names across places (thirty rooms each hold a `display-1`), so a
// name is ambiguous and the API answers it with a 409.

type S = components["schemas"];
export type SimFleet = S["SimFleetOutputBody"];
export type SimNode = S["SimNodeBody"];
// The generated schema keeps its old name for the row type; the console calls it
// what it is, a simulated component.
export type SimComponent = S["SimDeviceBody"];
export type SimImpairmentStatus = S["SimImpairmentStatus"];
export type SimProfile = S["SimProfileBody"];
export type ComponentSim = S["ComponentSimOutputBody"];
export type SimSurface = S["SimSurfaceBody"];
export type SimRoute = S["SimRouteBody"];
export type SimCommand = S["SimCommandBody"];
export type SimOID = S["SimOIDBody"];
export type SimResult = S["SimOperationResult"];
export type FireInput = Omit<S["FireSimInputBody"], "$schema">;
export type AddSimComponent = Omit<S["AddSimComponentInputBody"], "$schema">;
export type AddSimComponentResult = S["AddSimComponentOutputBody"];

// The impairment as the console holds it: every field present, since a PUT
// replaces the whole impairment and a field left out reads as zero.
export type Impairment = {
  unplugged: boolean;
  latency_ms: number;
  jitter_ms: number;
  loss_pct: number;
  rate_kbit: number;
};

export const HEALTHY: Impairment = { unplugged: false, latency_ms: 0, jitter_ms: 0, loss_pct: 0, rate_kbit: 0 };

export const SIMULATION_KEY = ["simulation"] as const;
export const SIM_PROFILES_KEY = ["simulation", "profiles"] as const;
export const componentSimKey = (id: string) => ["simulation", "component", id] as const;
export const impairmentKey = (id: string) => ["simulation", "impairment", id] as const;

// How often each surface reads the fleet: the sidebar only needs to know a
// simulator is there, the page wants hits as they land.
export const SIDEBAR_POLL_MS = 10_000;
// While no simulator is connected the sidebar asks far less often: most
// installs never run one, and the door only needs to appear eventually.
export const SIDEBAR_IDLE_POLL_MS = 60_000;
export const PAGE_POLL_MS = 1_000;
// How long a row's flash lasts; shorter than the page poll so a component hit
// on every poll flashes once per poll instead of glowing steadily.
export const BLINK_MS = 700;

// sidebarPollMs: the sidebar's interval for the fleet it last read.
export function sidebarPollMs(f: SimFleet | undefined): number {
  return f?.connected ? SIDEBAR_POLL_MS : SIDEBAR_IDLE_POLL_MS;
}

export async function getSimulation(): Promise<SimFleet> {
  const { data, error } = await api.GET("/simulation");
  if (error) throw error;
  return data as SimFleet;
}

export async function listSimProfiles(): Promise<SimProfile[]> {
  const { data, error } = await api.GET("/simulation/profiles");
  if (error) throw error;
  return data?.profiles ?? [];
}

export async function getComponentSim(id: string): Promise<ComponentSim> {
  const { data, error } = await api.GET("/components/{name}/simulation", { params: { path: { name: id } } });
  if (error) throw error;
  return data as ComponentSim;
}

export async function fireSim(id: string, body: FireInput): Promise<SimResult> {
  const { data, error } = await api.POST("/components/{name}/simulation:fire", { params: { path: { name: id } }, body });
  if (error) throw error;
  return data as SimResult;
}

export async function addSimComponent(node: string, body: AddSimComponent): Promise<AddSimComponentResult> {
  const { data, error } = await api.POST("/nodes/{name}/simulation/components", { params: { path: { name: node } }, body });
  if (error) throw error;
  return data as AddSimComponentResult;
}

export async function getImpairment(id: string): Promise<Impairment> {
  const { data, error } = await api.GET("/components/{name}/impairment", { params: { path: { name: id } } });
  if (error) throw error;
  return toImpairment(data);
}

export async function setImpairment(id: string, body: Impairment): Promise<Impairment> {
  const { data, error } = await api.PUT("/components/{name}/impairment", { params: { path: { name: id } }, body });
  if (error) throw error;
  return toImpairment(data);
}

// toImpairment fills the fields an answer left out with zero, which is what the
// server means by an absent field.
export function toImpairment(f: Partial<Impairment> | null | undefined): Impairment {
  return {
    unplugged: !!f?.unplugged,
    latency_ms: f?.latency_ms ?? 0,
    jitter_ms: f?.jitter_ms ?? 0,
    loss_pct: f?.loss_pct ?? 0,
    rate_kbit: f?.rate_kbit ?? 0,
  };
}

// isImpaired: anything broken at all. A healthy component reads all zero.
export function isImpaired(i: Partial<Impairment> | null | undefined): boolean {
  const f = toImpairment(i);
  return f.unplugged || f.latency_ms > 0 || f.jitter_ms > 0 || f.loss_pct > 0 || f.rate_kbit > 0;
}

// impairmentSummary says what is broken in a few words, for a row's impairment
// pill and the component page's fact.
export function impairmentSummary(i: Partial<Impairment> | null | undefined): string {
  const f = toImpairment(i);
  if (f.unplugged) return "unplugged";
  const parts: string[] = [];
  if (f.latency_ms > 0) parts.push(f.jitter_ms > 0 ? `${f.latency_ms} ms ±${f.jitter_ms}` : `${f.latency_ms} ms`);
  else if (f.jitter_ms > 0) parts.push(`±${f.jitter_ms} ms`);
  if (f.loss_pct > 0) parts.push(`${f.loss_pct}% loss`);
  if (f.rate_kbit > 0) parts.push(`${f.rate_kbit} kbit/s`);
  return parts.length ? parts.join(", ") : "healthy";
}

// The impairment's ranges, as the API states them. A draft field is the text
// the operator typed; validation parses it so the inputs never carry a native
// constraint attribute (ADR-0113).
export const IMPAIRMENT_LIMITS = { latency_ms: 10_000, loss_pct: 100, rate_kbit: 10_000_000 } as const;

export type ImpairmentDraft = {
  unplugged: boolean;
  latency_ms: string;
  jitter_ms: string;
  loss_pct: string;
  rate_kbit: string;
};
export type ImpairmentErrors = Partial<Record<"latency_ms" | "jitter_ms" | "loss_pct" | "rate_kbit", string>>;

export function draftOf(i: Impairment): ImpairmentDraft {
  return {
    unplugged: i.unplugged,
    latency_ms: String(i.latency_ms),
    jitter_ms: String(i.jitter_ms),
    loss_pct: String(i.loss_pct),
    rate_kbit: String(i.rate_kbit),
  };
}

const INT_RE = /^\d+$/;
const DECIMAL_RE = /^\d+(\.\d+)?$/;

// parseImpairment validates a draft and, when it holds, returns the full body a
// PUT sends. Empty reads as zero, so clearing a field is how to stop impairing it.
export function parseImpairment(d: ImpairmentDraft): { value: Impairment | null; errors: ImpairmentErrors } {
  const errors: ImpairmentErrors = {};
  const int = (k: "latency_ms" | "jitter_ms" | "rate_kbit", max: number, what: string): number => {
    const raw = d[k].trim();
    if (raw === "") return 0;
    if (!INT_RE.test(raw)) {
      errors[k] = "A whole number.";
      return 0;
    }
    const n = Number(raw);
    if (n > max) errors[k] = `At most ${max.toLocaleString("en-US")} ${what}.`;
    return n;
  };
  const latency = int("latency_ms", IMPAIRMENT_LIMITS.latency_ms, "ms");
  const rate = int("rate_kbit", IMPAIRMENT_LIMITS.rate_kbit, "kbit/s");
  const jitter = int("jitter_ms", IMPAIRMENT_LIMITS.latency_ms, "ms");
  if (!errors.jitter_ms && !errors.latency_ms && jitter > latency) errors.jitter_ms = "At most the latency.";
  let loss = 0;
  const lossRaw = d.loss_pct.trim();
  if (lossRaw !== "") {
    if (!DECIMAL_RE.test(lossRaw)) errors.loss_pct = "A percentage, like 2.5.";
    else {
      loss = Math.round(Number(lossRaw) * 100) / 100;
      if (loss > IMPAIRMENT_LIMITS.loss_pct) errors.loss_pct = "At most 100%.";
    }
  }
  if (Object.keys(errors).length) return { value: null, errors };
  return { value: { unplugged: d.unplugged, latency_ms: latency, jitter_ms: jitter, loss_pct: loss, rate_kbit: rate }, errors };
}

export function sameImpairment(a: Impairment, b: Impairment): boolean {
  return a.unplugged === b.unplugged && a.latency_ms === b.latency_ms && a.jitter_ms === b.jitter_ms && a.loss_pct === b.loss_pct && a.rate_kbit === b.rate_kbit;
}

// risenHits compares a fresh read against the hit counts last seen and returns
// the components whose count rose, then remembers the fresh counts. The first
// read of a component seeds it without flashing: arriving on the page is not a hit.
export function risenHits(seen: Map<string, number>, rows: readonly SimComponent[]): string[] {
  const risen: string[] = [];
  for (const d of rows) {
    const before = seen.get(d.component_id);
    if (before !== undefined && d.hits > before) risen.push(d.component_id);
    seen.set(d.component_id, d.hits);
  }
  return risen;
}

export function totalHits(n: SimNode): number {
  return (n.components ?? []).reduce((sum, d) => sum + d.hits, 0);
}

// The connected simulator nodes: where a simulated component can be added.
export function connectedNodes(f: SimFleet | undefined): string[] {
  return (f?.nodes ?? []).filter((n) => n.connected).map((n) => n.node);
}

// hitsByProtocol orders a component's per-protocol counts, busiest first.
export function hitsByProtocol(d: SimComponent): { protocol: string; hits: number }[] {
  return Object.entries(d.hits_by_protocol ?? {})
    .map(([protocol, hits]) => ({ protocol, hits }))
    .sort((a, b) => b.hits - a.hits || a.protocol.localeCompare(b.protocol));
}

// The add-component form's rule. The name mirrors storage's entity name rule
// (lowercase letters, digits and hyphens, starting with a letter or digit, at
// most 100 characters, never a dot); node and product must be chosen.
export const ENTITY_NAME_RE = /^[a-z0-9][a-z0-9-]*$/;
export type AddComponentDraft = { node: string; product: string; name: string; location: string };
export type AddComponentErrors = Partial<Record<keyof AddComponentDraft, string>>;

export function entityNameError(name: string): string | null {
  if (!name) return null;
  if (name.length > 100) return "At most 100 characters.";
  if (!ENTITY_NAME_RE.test(name)) return "Lowercase letters, digits and hyphens, starting with a letter or digit.";
  return null;
}

export function validateAddComponent(d: AddComponentDraft): AddComponentErrors {
  const e: AddComponentErrors = {};
  if (!d.node) e.node = "Choose the node whose simulator serves it.";
  if (!d.product) e.product = "Choose a simulated product.";
  const nameErr = d.name.trim() ? entityNameError(d.name.trim()) : "Name the new component.";
  if (nameErr) e.name = nameErr;
  return e;
}

// surfaceKind says which fire form a surface takes: an HTTP call, a command
// line, or an SNMP get. Anything else (ICMP) cannot be fired from the console.
export type SurfaceKind = "http" | "command" | "snmp" | "none";
export function surfaceKind(s: SimSurface): SurfaceKind {
  if (!s.fireable) return "none";
  const p = s.protocol.toLowerCase();
  if (p === "http" || p === "https") return "http";
  if (p === "snmp") return "snmp";
  return "command";
}

// The fire request a chosen operation pre-fills; the operator may edit any of it.
export type FireDraft = { surface: string; kind: SurfaceKind; method: string; path: string; body: string; command: string; oid: string };

export function fireDraftFor(s: SimSurface, pick?: { route?: SimRoute; command?: SimCommand; oid?: SimOID }): FireDraft {
  return {
    surface: s.name,
    kind: surfaceKind(s),
    method: pick?.route?.method ?? "GET",
    path: pick?.route?.path ?? "/",
    body: pick?.route?.example?.trim() ?? "",
    command: (pick?.command?.example ?? pick?.command?.match ?? "").trim(),
    oid: pick?.oid?.oid ?? "",
  };
}

// fireBody turns a draft into the request, sending only what its kind reads.
export function fireBody(d: FireDraft): FireInput {
  if (d.kind === "http") return { surface: d.surface, method: d.method || "GET", path: d.path || "/", ...(d.body.trim() ? { body: d.body } : {}) };
  if (d.kind === "snmp") return { surface: d.surface, oid: d.oid.trim() };
  return { surface: d.surface, command: d.command };
}

export function fireReady(d: FireDraft): boolean {
  if (d.kind === "http") return !!d.path.trim();
  if (d.kind === "snmp") return !!d.oid.trim();
  if (d.kind === "command") return !!d.command.trim();
  return false;
}

// groupProducts lays the profile catalog out for a grouped picker: each
// profile's products under its manufacturer and model.
export function productGroups(profiles: readonly SimProfile[]): { label: string; products: string[] }[] {
  return profiles
    .map((p) => ({ label: `${p.manufacturer} ${p.model} (${p.device_type})`, products: p.products ?? [] }))
    .filter((g) => g.products.length > 0)
    .sort((a, b) => a.label.localeCompare(b.label));
}
