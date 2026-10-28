import { For, Match, Show, Switch, type JSX } from "solid-js";
import Eyebrow from "./Eyebrow";
import KVStacked from "./KVStacked";
import { Box, MapPin } from "./icons";
import { entityLabel } from "../lib/entities";
import {
  type Template,
  type TemplateRole,
  placeTreeRows,
  quorumText,
  systemRoleSlots,
  valueText,
} from "../lib/templates";

// TemplatePreview (#1329): what a template creates, rendered from the template
// body itself, never a description of it. Each kind reads its own body: a
// standard its roles (quorum, accepted types, pins) and choices, a system the
// fill of each role, a place its subtree as an indented tree with the systems
// that land in it, a component its product, preset inputs and declared values.
// `all` is the catalog, so a system's fills read as the component templates'
// labels and its role counts come from its standard template.

const CHIP = "badge badge-ghost badge-sm whitespace-nowrap font-data";

// heldRoles: the roles of the fleet's own standard a system template builds to,
// when it holds one; the counts come from those, else from the standard template.
type HeldRole = { name: string; label?: string; quorum: number };

export default function TemplatePreview(props: { template: Template; all: Template[]; heldRoles?: HeldRole[] }): JSX.Element {
  return (
    <Switch>
      <Match when={props.template.standard}>{(s) => <StandardPreview standard={s()} />}</Match>
      <Match when={props.template.system && props.template}>{(t) => <SystemPreview template={t()} all={props.all} heldRoles={props.heldRoles} />}</Match>
      <Match when={props.template.place}>{(p) => <PlacePreview template={props.template} all={props.all} place={p()} />}</Match>
      <Match when={props.template.component}>{(c) => <ComponentPreview component={c()} />}</Match>
    </Switch>
  );
}

function Chips(props: { items?: string[] | null; none: string }): JSX.Element {
  return (
    <Show when={(props.items ?? []).length > 0} fallback={<span class="text-base-content/50">{props.none}</span>}>
      <span class="flex flex-wrap gap-1">
        <For each={props.items ?? []}>{(x) => <span class={CHIP}>{x}</span>}</For>
      </span>
    </Show>
  );
}

function StandardPreview(props: { standard: NonNullable<Template["standard"]> }): JSX.Element {
  const roles = () => props.standard.roles ?? [];
  const choiceOf = (r: TemplateRole) => {
    if (!r.choice) return "";
    const c = (props.standard.choices ?? []).find((x) => x.name === r.choice);
    const a = c?.alternates?.find((x) => x.name === r.alternate);
    return `${c ? entityLabel(c) : r.choice}: ${a ? entityLabel(a) : (r.alternate ?? "")}`;
  };
  return (
    <div class="flex flex-col gap-4" data-testid="preview-standard">
      <section class="flex flex-col gap-1.5">
        <Eyebrow label="Roles" hint="What a conforming system needs filled. Quorum is how many components the role takes; accepts is the component types that may fill it (any, when empty), and a pinned role is held to the products it names." />
        <div class="overflow-x-auto rounded-box border border-base-300">
          <table class="table table-sm">
            <thead>
              <tr><th>Role</th><th class="whitespace-nowrap">Quorum</th><th>Accepts</th></tr>
            </thead>
            <tbody>
              <For each={roles()}>
                {(r) => (
                  <tr data-role={r.name}>
                    <td>
                      <div class="flex flex-col">
                        <span>{entityLabel(r)}</span>
                        <Show when={r.label && r.label !== r.name}><span class="whitespace-nowrap font-data text-xs text-base-content/50">{r.name}</span></Show>
                        <Show when={choiceOf(r)}><span class="text-xs text-base-content/60">{choiceOf(r)}</span></Show>
                      </div>
                    </td>
                    <td class="whitespace-nowrap font-data tabular-nums">{quorumText(r)}</td>
                    <td>
                      <div class="flex flex-col gap-1">
                        <Chips items={r.accepted_types} none="any" />
                        <Show when={(r.pinned_products ?? []).length > 0}>
                          <span class="flex flex-wrap items-center gap-1 text-xs text-base-content/60">
                            pinned to <Chips items={r.pinned_products} none="" />
                          </span>
                        </Show>
                      </div>
                    </td>
                  </tr>
                )}
              </For>
            </tbody>
          </table>
        </div>
      </section>
      <Show when={(props.standard.choices ?? []).length > 0}>
        <section class="flex flex-col gap-1.5">
          <Eyebrow label="Choices" hint="A choice is answered one way by each conforming system: the roles of the alternate it takes are needed, the others are not. The first alternate is the template's default." />
          <ul class="flex flex-col gap-1.5 text-sm">
            <For each={props.standard.choices ?? []}>
              {(c) => (
                <li data-choice={c.name} class="flex flex-wrap items-center gap-1.5">
                  <span class="font-medium">{entityLabel(c)}</span>
                  <For each={c.alternates ?? []}>
                    {(a, i) => (
                      <span class="badge badge-sm" classList={{ "badge-ghost": i() > 0, "badge-soft badge-info": i() === 0 }}>
                        {entityLabel(a)}{i() === 0 ? " (default)" : ""}
                      </span>
                    )}
                  </For>
                </li>
              )}
            </For>
          </ul>
        </section>
      </Show>
      <Contract label="Declared properties" lines={props.standard.properties} />
      <Contract label="Declared metrics" lines={props.standard.metrics} />
    </div>
  );
}

function Contract(props: { label: string; lines?: NonNullable<Template["standard"]>["properties"] }): JSX.Element {
  return (
    <Show when={(props.lines ?? []).length > 0}>
      <section class="flex flex-col gap-1.5">
        <Eyebrow label={props.label} hint="The contract lines the standard declares: what every conforming system carries, with its default where the line sets one." />
        <ul class="flex flex-wrap gap-1.5">
          <For each={props.lines ?? []}>
            {(l) => (
              <li class={CHIP}>
                {l.name}
                <Show when={l.default !== undefined}>{` = ${valueText(l.default)}`}</Show>
                <Show when={l.required}>{" (required)"}</Show>
              </li>
            )}
          </For>
        </ul>
      </section>
    </Show>
  );
}

function SystemPreview(props: { template: Template; all: Template[]; heldRoles?: HeldRole[] }): JSX.Element {
  const sys = () => props.template.system!;
  const slots = () => systemRoleSlots(props.template, props.all, props.heldRoles);
  return (
    <div class="flex flex-col gap-4" data-testid="preview-system">
      <div class="grid grid-cols-2 gap-3">
        <KVStacked label="Standard" mono hint="The standard the system is built to: yours of that name when you hold one, else created from its template in the same write." value={<span data-testid="preview-standard-source">{sys().standard} <span class="font-sans text-xs text-base-content/60">{props.heldRoles ? "(yours)" : "(created from its template)"}</span></span>} />
        <KVStacked label="System type" mono value={sys().system_type || "none"} />
      </div>
      <section class="flex flex-col gap-1.5">
        <Eyebrow label="Role fills" hint="A component for each role, as many as the quorum of the standard it is built to (yours of that name when you hold one), each assigned to its role. They land unattached unless you give an address." />
        <ul class="flex flex-col divide-y divide-base-300 rounded-box border border-base-300 text-sm">
          <For each={slots()}>
            {(s) => (
              <li data-role={s.role} class="flex items-center gap-3 px-3 py-2">
                <span class="flex-1">{s.label}</span>
                <span class="text-base-content/70">{s.fill}</span>
                <span class="font-data tabular-nums text-xs text-base-content/50">x{s.count}</span>
              </li>
            )}
          </For>
        </ul>
      </section>
      <Values label="Declared values" values={sys().properties} />
    </div>
  );
}

function PlacePreview(props: { template: Template; all: Template[]; place: NonNullable<Template["place"]> }): JSX.Element {
  const rows = () => placeTreeRows(props.place);
  const systemLabel = (name: string) => {
    const t = props.all.find((x) => x.kind === "system_template" && x.name === name);
    return t ? entityLabel(t) : name;
  };
  return (
    <section class="flex flex-col gap-1.5" data-testid="preview-place">
      <Eyebrow label="Places" hint="The subtree it creates, each place a location type this fleet holds, under a parent you choose, with the systems each template places in it. Each place's type has to be allowed under its parent's." />
      <ul class="flex flex-col rounded-box border border-base-300 py-1 text-sm" role="tree" aria-label="Places it creates">
        <For each={rows()}>
          {(r) => (
            <li role="treeitem" class="flex items-center gap-2 py-1 pr-3" style={{ "padding-left": `${12 + r.depth * 20}px` }} data-depth={r.depth}>
              <Show
                when={r.kind === "place" && r}
                fallback={
                  <>
                    <span class="flex-none text-base-content/50"><Box size={14} /></span>
                    <span>{entityLabel({ name: systemLabel((r as { template: string }).template), label: (r as { label?: string }).label })}</span>
                    <span class="badge badge-ghost badge-sm">system</span>
                  </>
                }
              >
                {(p) => (
                  <>
                    <span class="flex-none text-base-content/50"><MapPin size={14} /></span>
                    <span>{entityLabel({ name: p().name ?? "", label: p().label }) || "(named by its type)"}</span>
                    <Show when={p().name}><span class="font-data text-xs text-base-content/50">{p().name}</span></Show>
                    <span class="ml-auto font-data text-xs text-base-content/60">{p().type}</span>
                  </>
                )}
              </Show>
            </li>
          )}
        </For>
      </ul>
    </section>
  );
}

function ComponentPreview(props: { component: NonNullable<Template["component"]> }): JSX.Element {
  const inputs = () => Object.entries(props.component.inputs ?? {}).sort(([a], [b]) => a.localeCompare(b));
  return (
    <div class="flex flex-col gap-4" data-testid="preview-component">
      <div class="grid grid-cols-2 gap-3">
        <KVStacked label="Product" mono value={props.component.product} />
        <KVStacked label="Driver" mono={!!props.component.driver} hint="The driver the preset is for; the product's own when the template names none." value={props.component.driver || "the product's own"} />
      </div>
      <section class="flex flex-col gap-1.5">
        <Eyebrow label="Preset inputs" hint="Every driver input but the address, which is a fact about one install and so is yours to give. A secret input names a secret; the template carries none." />
        <Show when={inputs().length > 0} fallback={<span class="text-sm text-base-content/50">none</span>}>
          <ul class="flex flex-wrap gap-1.5">
            <For each={inputs()}>{([k, v]) => <li class={CHIP}>{k} = {v}</li>}</For>
          </ul>
        </Show>
      </section>
      <Values label="Declared values" values={props.component.properties} />
    </div>
  );
}

function Values(props: { label: string; values?: Record<string, unknown> }): JSX.Element {
  const entries = () => Object.entries(props.values ?? {}).sort(([a], [b]) => a.localeCompare(b));
  return (
    <Show when={entries().length > 0}>
      <section class="flex flex-col gap-1.5">
        <Eyebrow label={props.label} hint="Property values written on the created row, yours to change afterwards like any other." />
        <ul class="flex flex-wrap gap-1.5">
          <For each={entries()}>{([k, v]) => <li class={CHIP}>{k} = {valueText(v)}</li>}</For>
        </ul>
      </section>
    </Show>
  );
}
