import type { Driver, DriverSpecInput } from "./drivers";
import type { Product } from "./products";
import type { ComponentType } from "./component_types";
import type { Secret } from "./secrets";
import type { EffectiveSlot } from "./system_slots";
import type { PollFailure } from "./poll_test";
import type { PickerOption } from "../components/ui/Picker";
import { entityLabel } from "./entities";

// The add-a-component flow (#2460): one path from a product to an endpoint the
// operator has seen answer. The pure half lives here (what each step needs,
// which driver a product suggests, which secrets fit an input, where a failed
// test sends the operator back to, and the create/undo sequence); the
// component (components/AddComponentFlow.tsx) only draws it.
//
// Nothing is created until the operator commits at the credentials step: the
// poll test needs a real endpoint to run, so "Create and test" creates the
// component and its endpoint, and every way out short of Done (Back from the
// test, Cancel, leaving the page) takes both back out. A commit refused half
// way takes back what it made, so a refusal also leaves nothing.

// addComponentHref: the flow's address, starting where the operator stands.
export function addComponentHref(at: { system?: string; under?: string; product?: string; simulated?: boolean }): string {
  const q = new URLSearchParams();
  if (at.under) q.set("under", at.under);
  if (at.system) q.set("system", at.system);
  if (at.product) q.set("product", at.product);
  if (at.simulated) q.set("simulated", "1");
  const s = q.toString();
  return `/components/add${s ? `?${s}` : ""}`;
}

export type StepKey = "product" | "where" | "endpoint" | "credentials" | "test" | "done";

export const STEPS: { key: StepKey; title: string }[] = [
  { key: "product", title: "Product" },
  { key: "where", title: "Where" },
  { key: "endpoint", title: "Endpoint" },
  { key: "credentials", title: "Credentials" },
  { key: "test", title: "Test" },
  { key: "done", title: "Done" },
];

export type FlowDraft = {
  product: string;
  // The name the operator typed, or "" for the platform to mint one.
  name: string;
  label: string;
  // The minted name the form previewed: the create's precondition (#702).
  expectedName?: string;
  system: string;
  slot: string;
  // The place, a location uuid. Defaulted from the system's place.
  location: string;
  outpost: string;
  driver: string;
  // Every driver input by name, the secret ones holding a secret's NAME.
  inputs: Record<string, string>;
};

export const emptyDraft = (): FlowDraft => ({
  product: "",
  name: "",
  label: "",
  system: "",
  slot: "",
  location: "",
  outpost: "",
  driver: "",
  inputs: {},
});

// productOption: a product as the picker shows it, told apart from its
// namesakes by its type and vendor.
export function productOption(p: Product, typesByName: Map<string, ComponentType>): PickerOption {
  const t = typesByName.get(p.component_type);
  return { value: p.name, label: entityLabel(p), detail: [t ? entityLabel(t) : p.component_type, p.vendor].filter(Boolean).join(", ") };
}

export type DriverChoice = { driver: Driver; why: "product" | "kin" | "other" };

// driverChoices: the attachable drivers (a stub has no spec to attach) in the
// order a product suggests them: its own driver, then the drivers the other
// products of its type are on, then, only when asked for all or when the
// product suggests nothing, every other one.
export function driverChoices(drivers: Driver[], products: Product[], productName: string, all: boolean): DriverChoice[] {
  const attachable = drivers.filter((d) => !!d.spec);
  const product = products.find((p) => p.name === productName);
  const own = product?.driver;
  const kin = new Set(
    products.filter((p) => p.name !== productName && !!product && p.component_type === product.component_type && p.driver).map((p) => p.driver as string),
  );
  const out: DriverChoice[] = [];
  const mine = attachable.find((d) => d.name === own);
  if (mine) out.push({ driver: mine, why: "product" });
  for (const d of attachable) if (d.name !== own && kin.has(d.name)) out.push({ driver: d, why: "kin" });
  if (all || out.length === 0) for (const d of attachable) if (!out.some((c) => c.driver.name === d.name)) out.push({ driver: d, why: "other" });
  return out;
}

// splitInputs: a driver's inputs, the parameters on the endpoint step and the
// credentials (its secret inputs) on their own step.
export function splitInputs(driver: Driver | undefined): { params: DriverSpecInput[]; secrets: DriverSpecInput[] } {
  const inputs = driver?.spec?.inputs ?? [];
  return { params: inputs.filter((i) => i.kind !== "secret"), secrets: inputs.filter((i) => i.kind === "secret") };
}

// secretOptions: the secrets an input of this type may name, valued on the
// name (what a driver input carries: a reference the cascade resolves, never
// the value), told apart by who owns them.
export function secretOptions(secrets: Pick<Secret, "name" | "label" | "secret_type" | "owner_kind" | "owner_name">[], secretType: string | undefined): PickerOption[] {
  return secrets
    .filter((s) => !secretType || s.secret_type === secretType)
    .map((s) => ({ value: s.name, label: entityLabel(s), detail: `${s.name}, ${[s.owner_kind, s.owner_name].filter(Boolean).join(" ")}` }));
}

// typeLine: a component type and each type above it, nearest first.
function typeLine(name: string, types: ComponentType[]): string[] {
  const byName = new Map(types.map((t) => [t.name, t]));
  const out: string[] = [];
  for (let t = byName.get(name), seen = 0; t && seen < 64; t = t.parent ? byName.get(t.parent) : undefined, seen++) out.push(t.name);
  return out.length ? out : [name];
}

// slotsFor: the system's slots a component of this product may fill, as the
// slot's typed guard decides (#626): an accepted type at or above the
// product's, and the product among the pins when the slot pins any.
export function slotsFor(slots: EffectiveSlot[], product: Product | undefined, types: ComponentType[]): EffectiveSlot[] {
  if (!product) return [];
  const line = new Set(typeLine(product.component_type, types));
  return slots.filter(
    (s) =>
      (!s.accepted_types?.length || s.accepted_types.some((t) => line.has(t))) &&
      (!s.pinned_products?.length || s.pinned_products.includes(product.name)),
  );
}

// inheritedLocation: the place a component in this system takes, the
// system's own. The server does not inherit it yet (#878), so the flow sets it.
export function inheritedLocation(systems: { id: string; location_id?: string | null }[], system: string): string {
  return systems.find((s) => s.id === system)?.location_id ?? "";
}

export type SimPlan = {
  outpost: string;
  product: string;
  address: string;
  endpoints: { driver?: string; inputs?: Record<string, string>; transport?: string; target?: string }[] | null;
};
export type SimChoice = { outpost: string; address: string; driver: string; inputs: Record<string, string> };

// simChoice: what "use a simulated device" fills in, read off the server's plan
// for a new simulated device of the product: the outpost beside the simulator,
// the address it would answer on, and the driver attach with its inputs (the
// secret ones naming the platform secret the simulator answers to). A plan the
// simulator can only probe offers nothing: this flow attaches a driver.
export function simChoice(plan: SimPlan): SimChoice | null {
  const ep = (plan.endpoints ?? []).find((e) => !!e.driver);
  if (!ep) return null;
  return { outpost: plan.outpost, address: plan.address, driver: ep.driver as string, inputs: { ...(ep.inputs ?? {}) } };
}

const filled = (v: string | undefined) => (v ?? "").trim() !== "";

// stepReady: whether the operator may go on from a step.
export function stepReady(step: StepKey, d: FlowDraft, driver: Driver | undefined): boolean {
  const { params, secrets } = splitInputs(driver);
  switch (step) {
    case "product":
      return !!d.product;
    case "where":
      return !d.slot || !!d.system;
    case "endpoint":
      return !!d.driver && !!driver && !!d.outpost && params.every((i) => !i.required || filled(d.inputs[i.name]));
    case "credentials":
      return secrets.every((i) => !i.required || filled(d.inputs[i.name]));
    default:
      return true;
  }
}

// failureStep: the step a failed test is fixed on. A refused login (the
// gate's reason starts with its class, "auth: ...") is the credential; any
// other failure (no answer, the wrong port, an outpost not connected) is the
// endpoint's address or placement.
export function failureStep(f: PollFailure): "endpoint" | "credentials" {
  return /^auth\b|unauthori[sz]ed|authori[sz]ation|credential|community|login|\b401\b|\b403\b/i.test(f.reason) ? "credentials" : "endpoint";
}

export type Created = { component: string; endpoint?: string };

export type FlowIO = {
  createComponent: (b: { product: string; name?: string; expected_name?: string; label?: string; system?: string; location?: string }) => Promise<{ id: string }>;
  assignSlot: (system: string, slot: string, component: string) => Promise<void>;
  createEndpoint: (b: { driver: string; inputs: Record<string, string>; component: string; outpost: string }) => Promise<{ id: string }>;
  deleteEndpoint: (id: string) => Promise<void>;
  deleteComponent: (id: string) => Promise<void>;
};

// commitFlow creates what the draft describes: the component (in its system
// and place), its slot, then the driver attach with every input filled. A
// refusal part way takes back what was made before it rethrows.
export async function commitFlow(d: FlowDraft, io: FlowIO): Promise<Required<Created>> {
  const nm = d.name.trim();
  const c = await io.createComponent({
    product: d.product,
    name: nm || undefined,
    expected_name: nm ? undefined : d.expectedName,
    label: d.label.trim() || undefined,
    system: d.system || undefined,
    location: d.location || undefined,
  });
  try {
    if (d.system && d.slot) await io.assignSlot(d.system, d.slot, c.id);
    const inputs = Object.fromEntries(
      Object.entries(d.inputs)
        .map(([k, v]) => [k, v.trim()])
        .filter(([, v]) => v !== ""),
    );
    const ep = await io.createEndpoint({ driver: d.driver, inputs, component: c.id, outpost: d.outpost });
    return { component: c.id, endpoint: ep.id };
  } catch (e) {
    await undoCreated({ component: c.id }, io);
    throw e;
  }
}

// undoCreated takes a commit back out: the endpoint first, since a component
// that owns endpoints refuses its delete (#138).
export async function undoCreated(made: Created, io: Pick<FlowIO, "deleteEndpoint" | "deleteComponent">): Promise<void> {
  if (made.endpoint) await io.deleteEndpoint(made.endpoint);
  await io.deleteComponent(made.component);
}
