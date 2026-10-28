import { For, Index, Match, Show, Switch, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FieldRow from "./FieldRow";
import Eyebrow from "./Eyebrow";
import TreeSelect from "./TreeSelect";
import { Plus } from "./icons";
import { useFormActions } from "../lib/formactions";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { byLabel, entityLabel } from "../lib/entities";
import { nameStemError } from "../lib/validate";
import { type TreeNode } from "../lib/treeselect";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { NODES_KEY, listNodes } from "../lib/nodes";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { SYSTEMS_KEY } from "../lib/systems";
import { COMPONENTS_KEY } from "../lib/components";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { DRIVERS_KEY, listDrivers } from "../lib/drivers";
import { SECRETS_KEY, listSecrets } from "../lib/secrets";
import { FLEET_VIEW_KEY } from "../lib/fleet";
import {
  type ComponentDraft,
  type ForkDraft,
  type PlacesDraft,
  type SystemDraft,
  type Template,
  componentBody,
  forkBody,
  forkStandard,
  instantiateComponent,
  instantiateLocations,
  instantiateSystem,
  landingHref,
  missingNeeds,
  needsNode,
  permText,
  placeRef,
  placeValue,
  placesBody,
  startNeeds,
  systemBody,
  systemRoleSlots,
} from "../lib/templates";
import { standardRoles, standardRolesKey } from "../lib/system_roles";

// StartFromTemplateForm (#1329): the "Start from this template" form a Drawer
// hosts, one per kind, each posting to its own route: a standard template
// forks (POST /standards:fork), the others instantiate (POST
// /components:instantiate, /systems:instantiate, /locations:instantiate). What
// lands is ordinary rows the operator owns; a refusal shows the API's own
// detail, and nothing lands. A success hands back the created row's address
// with the template's name riding along as ?from=, so the row's page can say it
// is now the operator's. `place` is the default target a create flow carried in.

export type StartFormProps = { template: Template; all: Template[]; place?: string; onDone: (href: string) => void };

export default function StartFromTemplateForm(props: StartFormProps): JSX.Element {
  return (
    <Switch>
      <Match when={props.template.kind === "standard_template"}><ForkForm {...props} /></Match>
      <Match when={props.template.kind === "component_template"}><ComponentForm {...props} /></Match>
      <Match when={props.template.kind === "system_template"}><SystemForm {...props} /></Match>
      <Match when={props.template.kind === "place_template"}><PlacesForm {...props} /></Match>
    </Switch>
  );
}

function ErrorAlert(props: { err: string | null }): JSX.Element {
  return (
    <Show when={props.err}>
      <div role="alert" data-testid="start-error" class="alert alert-error alert-soft text-sm"><span>{props.err}</span></div>
    </Show>
  );
}

// The submit lifecycle every form shares: busy, the refusal, and the hand-off.
function useSubmit(run: () => Promise<string>, onDone: (href: string) => void) {
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const submit = async () => {
    setBusy(true);
    setErr(null);
    try {
      onDone(await run());
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  };
  return { busy, err, submit };
}

// The place picker's state: offered by dotted path (lib/templates placeValue),
// defaulting to the place a create flow carried until the operator picks.
function usePlace(carried?: string) {
  const me = useMe();
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations, enabled: can(me.data, "location", "read") }));
  const items = createMemo<TreeNode[]>(() => (locations.data ?? []).map((l) => ({ id: l.id, value: placeValue(l), label: entityLabel(l), parentId: l.parent_id })));
  const [chosen, setChosen] = createSignal<string | null>(null);
  const value = () => chosen() ?? placeRef(locations.data ?? [], carried);
  return { items, value, set: (v: string) => setChosen(v) };
}

// The standards the fleet holds, by name: what decides whether a start forks
// one (and so needs standard:create) and whose roles a system is staffed to.
export function useHeldStandards() {
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));
  const byName = (name: string) => (standards.data ?? []).find((s) => s.name === name);
  return { standards, byName, held: (name: string) => !!byName(name) };
}

// The roles of the fleet's own standard of that name, when it holds one: a
// system is staffed to those, not to the template's copy. Undefined while it
// is not held (the plan forks the template's) or not yet read.
export function useHeldRoles(byName: (name: string) => { id: string } | undefined, name: () => string) {
  const held = () => byName(name());
  const roles = useQuery(() => ({
    queryKey: standardRolesKey(held()?.id ?? ""),
    queryFn: () => standardRoles(held()!.id),
    enabled: !!held(),
  }));
  return () => (held() ? roles.data : undefined);
}

// What the form still lacks to start, given its own choices; an alert names
// it and the submit is refused until it is empty.
function useMissing(props: StartFormProps, held: (n: string) => boolean, choice: () => { standard?: string; address?: boolean }) {
  const me = useMe();
  return createMemo(() => missingNeeds(startNeeds(props.template, { all: props.all, held, ...choice() }), (r, a) => can(me.data, r, a)));
}

function MissingAlert(props: { missing: readonly (readonly [string, string])[] }): JSX.Element {
  return (
    <Show when={props.missing.length > 0}>
      <div role="alert" data-testid="start-missing" class="alert alert-warning alert-soft text-sm">
        <span>Starting this as filled in needs {permText([...props.missing])}.</span>
      </div>
    </Show>
  );
}

// The hand-off extras the notice reads: the standards a start forked and how
// many components still wait to be attached.
function extras(made: { standard_forked?: string[] | null; to_attach?: string[] | null }): Record<string, string> {
  const out: Record<string, string> = {};
  if ((made.standard_forked ?? []).length) out.forked = made.standard_forked!.join(",");
  if ((made.to_attach ?? []).length) out.unattached = String(made.to_attach!.length);
  return out;
}

function NodeSelect(props: { value: string; onChange: (v: string) => void; needed: boolean }): JSX.Element {
  const me = useMe();
  const nodes = useQuery(() => ({ queryKey: NODES_KEY, queryFn: listNodes, enabled: can(me.data, "node", "read") }));
  const sorted = createMemo(() => [...(nodes.data ?? [])].sort(byLabel));
  return (
    <FieldRow
      label="Node"
      info="The node an attached endpoint is placed on, so its collection reaches the component. Needed only when you give an address."
      hint={props.needed ? "An address needs a node to place its endpoint on." : undefined}
    >
      <select class="select select-bordered w-full" classList={{ "select-error": props.needed }} value={props.value} onChange={(e) => props.onChange(e.currentTarget.value)}>
        <option value="">None</option>
        <For each={sorted()}>{(n) => <option value={n.name}>{entityLabel(n)}</option>}</For>
      </select>
    </FieldRow>
  );
}

function NameField(props: { value: string; onInput: (v: string) => void; placeholder: string; info: string }): JSX.Element {
  const err = () => nameStemError(props.value.trim());
  return (
    <FieldRow bind="name" info={props.info} hint={err() ?? undefined}>
      <input class="input input-bordered w-full font-data" classList={{ "input-error": !!err() }} autocomplete="off" placeholder={props.placeholder} value={props.value} onInput={(e) => props.onInput(e.currentTarget.value)} />
    </FieldRow>
  );
}

function LabelField(props: { value: string; onInput: (v: string) => void; placeholder: string }): JSX.Element {
  return (
    <FieldRow bind="label" info="What an operator reads. Optional.">
      <input class="input input-bordered w-full" autocomplete="off" placeholder={props.placeholder} value={props.value} onInput={(e) => props.onInput(e.currentTarget.value)} />
    </FieldRow>
  );
}

// --- standard: fork --------------------------------------------------------------

function ForkForm(props: StartFormProps): JSX.Element {
  const qc = useQueryClient();
  const { standards, held } = useHeldStandards();
  const missing = useMissing(props, held, () => ({}));
  const parentOf = () => props.template.standard?.parent ?? "";
  const [d, setD] = createSignal<ForkDraft>({ name: "", label: "", parent: "" });
  const set = (k: keyof ForkDraft, v: string) => setD({ ...d(), [k]: v });
  const options = createMemo(() => [...(standards.data ?? [])].sort(byLabel));
  const { busy, err, submit } = useSubmit(async () => {
    const made = await forkStandard(forkBody(props.template.name, d()));
    await qc.invalidateQueries({ queryKey: STANDARDS_KEY });
    return landingHref("/standards", props.template.name, { open: made.name });
  }, props.onDone);
  useFormActions().bind({
    submitLabel: "Fork standard",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => missing().length > 0 || !!nameStemError(d().name.trim()),
  });
  return (
    <form class="flex flex-col gap-4" data-testid="start-form-standard" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <ErrorAlert err={err()} />
      <MissingAlert missing={missing()} />
      <NameField value={d().name} onInput={(v) => set("name", v)} placeholder={props.template.name} info="The standard's name. Empty takes the template's; a second fork of the same template needs a name of its own." />
      <LabelField value={d().label} onInput={(v) => set("label", v)} placeholder={props.template.label} />
      <Show when={parentOf()}>
        <FieldRow label="Variant of" info="This template is a variant: it hangs off a standard you hold. The default is yours named for the template's parent.">
          <select class="select select-bordered w-full" aria-label="Variant of" value={d().parent} onChange={(e) => set("parent", e.currentTarget.value)}>
            <option value="">Yours named {parentOf()}</option>
            <For each={options()}>{(s) => <option value={s.name}>{entityLabel(s)}</option>}</For>
          </select>
        </FieldRow>
      </Show>
    </form>
  );
}

// --- component -------------------------------------------------------------------

function ComponentForm(props: StartFormProps): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const place = usePlace(props.place);
  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts, enabled: can(me.data, "product", "read") }));
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers, enabled: can(me.data, "driver", "read") }));
  const secrets = useQuery(() => ({ queryKey: SECRETS_KEY, queryFn: listSecrets, enabled: can(me.data, "secret", "read") }));
  const [d, setD] = createSignal<Omit<ComponentDraft, "place">>({ name: "", label: "", node: "", address: "", secrets: {} });
  const set = <K extends keyof Omit<ComponentDraft, "place">>(k: K, v: ComponentDraft[K]) => setD({ ...d(), [k]: v });
  // The secret inputs the template's driver declares, for the secret names you give.
  const secretInputs = createMemo(() => {
    const c = props.template.component;
    if (!c) return [];
    const driverName = c.driver || (products.data ?? []).find((p) => p.name === c.product)?.driver;
    const spec = (drivers.data ?? []).find((x) => x.name === driverName)?.spec;
    return (spec?.inputs ?? []).filter((i) => i.kind === "secret" && !(c.inputs ?? {})[i.name]);
  });
  const { held } = useHeldStandards();
  // Attaching writes an endpoint; without endpoint:create the component lands unattached.
  const mayAttach = () => can(me.data, "endpoint", "create");
  const missing = useMissing(props, held, () => ({ address: !!d().address.trim() }));
  const noNode = () => needsNode(d());
  const { busy, err, submit } = useSubmit(async () => {
    const made = await instantiateComponent(componentBody(props.template.name, { ...d(), place: place.value() }));
    await Promise.all([qc.invalidateQueries({ queryKey: COMPONENTS_KEY }), qc.invalidateQueries({ queryKey: FLEET_VIEW_KEY })]);
    return landingHref(`/components/${encodeURIComponent(made.component_id)}`, props.template.name, made.attached ? {} : { unattached: "1" });
  }, props.onDone);
  useFormActions().bind({
    submitLabel: "Start component",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => missing().length > 0 || noNode() || !!nameStemError(d().name.trim()),
  });
  return (
    <form class="flex flex-col gap-4" data-testid="start-form-component" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <ErrorAlert err={err()} />
      <MissingAlert missing={missing()} />
      <FieldRow label="Place" info="Where the component sits. Optional.">
        <TreeSelect items={place.items()} value={place.value()} onChange={place.set} rootLabel="None" />
      </FieldRow>
      <NameField value={d().name} onInput={(v) => set("name", v)} placeholder="minted from its type" info="Optional. Empty mints one from its type, as any component's name is." />
      <LabelField value={d().label} onInput={(v) => set("label", v)} placeholder={props.template.label} />
      <Show when={mayAttach()} fallback={<NoAttach />}>
      <NodeSelect value={d().node} onChange={(v) => set("node", v)} needed={noNode()} />
      <FieldRow label="Address" info="The host its driver reaches it at. Given, the endpoint attaches with the template's preset under it; empty, the component lands unattached and you attach it later.">
        <input class="input input-bordered w-full font-data" autocomplete="off" placeholder="10.0.0.20" value={d().address} onInput={(e) => set("address", e.currentTarget.value)} />
      </FieldRow>
      <For each={secretInputs()}>
        {(i) => (
          <FieldRow label={`Secret: ${i.name}`} info={`A ${i.secret_type ?? ""} secret of yours the driver reads as ${i.name}. Optional.`}>
            <select class="select select-bordered w-full" value={d().secrets[i.name] ?? ""} onChange={(e) => set("secrets", { ...d().secrets, [i.name]: e.currentTarget.value })}>
              <option value="">None</option>
              <For each={(secrets.data ?? []).filter((s) => !i.secret_type || s.secret_type === i.secret_type)}>
                {(s) => <option value={s.name}>{entityLabel(s)}</option>}
              </For>
            </select>
          </FieldRow>
        )}
      </For>
      </Show>
    </form>
  );
}

// Without endpoint:create nothing attaches: the address fields are not offered,
// and the field that would hold them says why.
function NoAttach(): JSX.Element {
  return (
    <FieldRow label="Address" info="Attaching writes an endpoint, which needs endpoint:create. Without it everything lands unattached, for someone who holds it to attach later.">
      <input class="input input-bordered w-full font-data" disabled placeholder="lands unattached (attaching needs endpoint:create)" />
    </FieldRow>
  );
}

// --- system ----------------------------------------------------------------------

function SystemForm(props: StartFormProps): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const place = usePlace(props.place);
  const { standards, held, byName } = useHeldStandards();
  const [d, setD] = createSignal<Omit<SystemDraft, "place">>({ name: "", label: "", standard: "", node: "", addresses: {} });
  // The standard the plan builds to: the form's choice, else the template's. When
  // the fleet holds it the slots are its roles, else the template's it forks.
  const planStandard = () => d().standard || props.template.system?.standard || "";
  const heldRoles = useHeldRoles(byName, planStandard);
  const slots = createMemo(() => systemRoleSlots(props.template, props.all, heldRoles()));
  const mayAttach = () => can(me.data, "endpoint", "create");
  const anyAddress = () => Object.values(d().addresses).some((l) => l.some((a) => !!a.trim()));
  const missing = useMissing(props, held, () => ({ standard: d().standard, address: anyAddress() }));
  const set = <K extends keyof Omit<SystemDraft, "place">>(k: K, v: SystemDraft[K]) => setD({ ...d(), [k]: v });
  const setAddress = (role: string, i: number, count: number, v: string) => {
    const list = [...(d().addresses[role] ?? Array.from({ length: count }, () => ""))];
    list[i] = v;
    set("addresses", { ...d().addresses, [role]: list });
  };
  const options = createMemo(() => [...(standards.data ?? [])].sort(byLabel));
  const noNode = () => needsNode(d());
  const { busy, err, submit } = useSubmit(async () => {
    const made = await instantiateSystem(systemBody(props.template.name, { ...d(), place: place.value() }));
    await Promise.all([
      qc.invalidateQueries({ queryKey: SYSTEMS_KEY }),
      qc.invalidateQueries({ queryKey: COMPONENTS_KEY }),
      qc.invalidateQueries({ queryKey: STANDARDS_KEY }),
      qc.invalidateQueries({ queryKey: FLEET_VIEW_KEY }),
    ]);
    return landingHref(`/systems/${encodeURIComponent(made.system_id)}`, props.template.name, extras(made));
  }, props.onDone);
  useFormActions().bind({
    submitLabel: "Start system",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => missing().length > 0 || noNode() || !!nameStemError(d().name.trim()),
  });
  return (
    <form class="flex flex-col gap-4" data-testid="start-form-system" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <ErrorAlert err={err()} />
      <MissingAlert missing={missing()} />
      <FieldRow label="Place" info="Where the system and its components sit. Optional.">
        <TreeSelect items={place.items()} value={place.value()} onChange={place.set} rootLabel="None" />
      </FieldRow>
      <NameField value={d().name} onInput={(v) => set("name", v)} placeholder="minted from its type" info="Optional. Empty mints one from its system type." />
      <LabelField value={d().label} onInput={(v) => set("label", v)} placeholder={props.template.label} />
      <FieldRow label="Standard" info="What it is built to. The default is the template's: yours of that name when you hold one, else forked from its template in the same write. Or name one of your own.">
        <select class="select select-bordered w-full" aria-label="Standard" value={d().standard} onChange={(e) => set("standard", e.currentTarget.value)}>
          <option value="">The template's ({props.template.system?.standard})</option>
          <For each={options()}>{(s) => <option value={s.name}>{entityLabel(s)}</option>}</For>
        </select>
      </FieldRow>
      <Show when={mayAttach()} fallback={<NoAttach />}>
      <NodeSelect value={d().node} onChange={(v) => set("node", v)} needed={noNode()} />
      <section class="flex flex-col gap-2">
        <Eyebrow label="Addresses" hint="One per component, by role, as many as the standard it is built to asks for (yours of that name when you hold one). A component given an address lands attached through its driver with the template's preset; the rest land unattached and are yours to attach later." />
        <For each={slots()}>
          {(s) => (
            <Index each={Array.from({ length: s.count })}>
              {(_, i) => (
                <FieldRow label={s.count > 1 ? `${s.label} ${i + 1}` : s.label} hint={i === 0 ? s.fill : undefined}>
                  <input
                    class="input input-bordered input-sm w-full font-data"
                    autocomplete="off"
                    placeholder="unattached"
                    value={(d().addresses[s.role] ?? [])[i] ?? ""}
                    onInput={(e) => setAddress(s.role, i, s.count, e.currentTarget.value)}
                  />
                </FieldRow>
              )}
            </Index>
          )}
        </For>
      </section>
      </Show>
    </form>
  );
}

// --- places ----------------------------------------------------------------------

function PlacesForm(props: StartFormProps): JSX.Element {
  const qc = useQueryClient();
  const parent = usePlace(props.place);
  const { held } = useHeldStandards();
  const missing = useMissing(props, held, () => ({}));
  const [d, setD] = createSignal<Omit<PlacesDraft, "parent">>({ name: "", label: "" });
  const set = (k: keyof Omit<PlacesDraft, "parent">, v: string) => setD({ ...d(), [k]: v });
  const { busy, err, submit } = useSubmit(async () => {
    const made = await instantiateLocations(placesBody(props.template.name, { ...d(), parent: parent.value() }));
    await Promise.all([
      qc.invalidateQueries({ queryKey: LOCATIONS_KEY }),
      qc.invalidateQueries({ queryKey: SYSTEMS_KEY }),
      qc.invalidateQueries({ queryKey: COMPONENTS_KEY }),
      qc.invalidateQueries({ queryKey: STANDARDS_KEY }),
      qc.invalidateQueries({ queryKey: FLEET_VIEW_KEY }),
    ]);
    const root = (made.places ?? [])[0];
    return landingHref(`/locations/${encodeURIComponent(root?.location_id ?? "")}`, props.template.name, extras(made));
  }, props.onDone);
  useFormActions().bind({
    submitLabel: "Start places",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => missing().length > 0 || !!nameStemError(d().name.trim()),
  });
  return (
    <form class="flex flex-col gap-4" data-testid="start-form-place" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <ErrorAlert err={err()} />
      <MissingAlert missing={missing()} />
      <FieldRow label="Parent place" info="The place the subtree sits under. Each place's type has to be allowed under its parent's; None makes the root a new top.">
        <TreeSelect items={parent.items()} value={parent.value()} onChange={parent.set} rootLabel="None (a new top)" />
      </FieldRow>
      <NameField value={d().name} onInput={(v) => set("name", v)} placeholder={props.template.place?.name || "minted from its type"} info="The root place's name. Empty takes the template's, else its type's name rule mints one." />
      <LabelField value={d().label} onInput={(v) => set("label", v)} placeholder={entityLabel({ name: entityLabel(props.template), label: props.template.place?.label })} />
    </form>
  );
}
