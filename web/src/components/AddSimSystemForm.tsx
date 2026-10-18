import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient, type UseQueryResult } from "@tanstack/solid-query";
import FieldRow from "./FieldRow";
import Eyebrow from "./Eyebrow";
import Button from "./Button";
import { Plus, RotateCcw } from "./icons";
import { useFormActions } from "../lib/formactions";
import { can, useMe } from "../lib/auth";
import { describeError } from "../lib/format";
import { byLabel, entityLabel } from "../lib/entities";
import { COMPONENTS_KEY } from "../lib/components";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { STANDARDS_KEY, listStandards } from "../lib/standards";
import { SYSTEM_TYPES_KEY, listSystemTypes } from "../lib/system_types";
import {
  SIMULATION_KEY,
  addSimSystem,
  addSystemBody,
  planShortfall,
  planSimSystem,
  simPlanKey,
  validateAddSystem,
  type AddSimSystemResult,
  type AddSystemDraft,
  type SimRoleFill,
  type SimSystemPlan,
} from "../lib/simulation";

// AddSimSystemForm (#913): the add-simulated-system form the Drawer hosts. The
// operator picks where it is served, the standard it is built to, its type, and
// optionally where it sits and its name; the server staffs each of the
// standard's roles with a simulated product and creates the system and every
// component in one transaction. Choosing a standard reads the plan first, so
// the operator sees what the system will hold, and which roles it will be
// short, before anything is created.
export default function AddSimSystemForm(props: { nodes: string[]; close: () => void; onAdded: (r: AddSimSystemResult) => void }) {
  const qc = useQueryClient();
  const me = useMe();
  const canPlace = () => can(me.data, "location", "read");
  const standards = useQuery(() => ({ queryKey: STANDARDS_KEY, queryFn: listStandards }));
  const types = useQuery(() => ({ queryKey: SYSTEM_TYPES_KEY, queryFn: listSystemTypes }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations, enabled: canPlace() }));
  const [draft, setDraft] = createSignal<AddSystemDraft>({
    node: props.nodes.length === 1 ? props.nodes[0] : "",
    standard: "",
    system_type: "",
    name: "",
    location: "",
  });
  const plan = useQuery(() => ({
    queryKey: simPlanKey(draft().standard),
    queryFn: () => planSimSystem(draft().standard),
    enabled: !!draft().standard,
    retry: false,
  }));
  const [touched, setTouched] = createSignal<Partial<Record<keyof AddSystemDraft, boolean>>>({});
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const errors = createMemo(() => validateAddSystem(draft()));
  const shown = (k: keyof AddSystemDraft) => (touched()[k] ? errors()[k] : undefined);
  const set = (k: keyof AddSystemDraft, v: string) => {
    setDraft({ ...draft(), [k]: v });
    setTouched({ ...touched(), [k]: true });
  };
  // Nothing to create until the plan has answered with at least one component.
  const planReady = () => !!draft().standard && !!plan.data && plan.data.standard === draft().standard && plan.data.components > 0;
  const sortedStandards = createMemo(() => [...(standards.data ?? [])].sort(byLabel));
  const sortedTypes = createMemo(() => [...(types.data ?? [])].sort(byLabel));

  useFormActions().bind({
    submitLabel: "Add system",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => Object.keys(errors()).length > 0 || !planReady(),
    cancel: props.close,
  });

  async function submit() {
    if (Object.keys(errors()).length) {
      setTouched({ node: true, standard: true, system_type: true, name: true, location: true });
      return;
    }
    if (!planReady()) return;
    setBusy(true);
    setErr(null);
    const d = draft();
    try {
      const r = await addSimSystem(d.node, addSystemBody(d));
      await Promise.all([
        qc.invalidateQueries({ queryKey: SIMULATION_KEY, exact: true }),
        qc.invalidateQueries({ queryKey: COMPONENTS_KEY }),
      ]);
      props.onAdded(r);
      props.close();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-4" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm" data-testid="add-system-error"><span>{err()}</span></div>
      </Show>
      <FieldRow label="Node" info="The node whose simulator serves the system's components. Their endpoints are placed on this node, so this node's collection polls them." hint={shown("node")}>
        <select class="select select-bordered w-full font-data" aria-required="true" value={draft().node} onChange={(e) => set("node", e.currentTarget.value)} disabled={busy()}>
          <option value="" disabled>Choose a node...</option>
          <For each={props.nodes}>{(n) => <option value={n}>{n}</option>}</For>
        </select>
      </FieldRow>
      <LoadError what="the standards" query={standards} testid="standards-error" />
      <FieldRow
        label="Standard"
        info="The blueprint the system is built to. Its roles say which components a system of this standard holds and how many; each role is staffed with the first simulated product that fits it, and where the standard offers a choice, one alternate is taken."
        hint={shown("standard") ?? (standards.data?.length === 0 ? "No standards yet: define one under Catalog, Standards." : undefined)}
      >
        <select class="select select-bordered w-full" aria-required="true" value={draft().standard} onChange={(e) => set("standard", e.currentTarget.value)} disabled={busy() || standards.isPending}>
          <option value="" disabled>{standards.isPending ? "Loading standards..." : "Choose a standard..."}</option>
          <For each={sortedStandards()}>{(s) => <option value={s.name}>{entityLabel(s)}</option>}</For>
        </select>
      </FieldRow>
      <Show when={draft().standard}>
        <PlanPreview
          plan={plan.data?.standard === draft().standard ? plan.data : undefined}
          pending={plan.isPending}
          error={plan.error}
          retrying={plan.isFetching}
          onRetry={() => void plan.refetch()}
        />
      </Show>
      <LoadError what="the system types" query={types} testid="system-types-error" />
      <FieldRow
        label="System type"
        info="What kind of system this is. The type also mints the system's name when none is given."
        hint={shown("system_type") ?? (types.data?.length === 0 ? "No system types yet: define one under Catalog, System types." : undefined)}
      >
        <select class="select select-bordered w-full" aria-required="true" value={draft().system_type} onChange={(e) => set("system_type", e.currentTarget.value)} disabled={busy() || types.isPending}>
          <option value="" disabled>{types.isPending ? "Loading types..." : "Choose a type..."}</option>
          <For each={sortedTypes()}>{(t) => <option value={t.name}>{entityLabel(t)}</option>}</For>
        </select>
      </FieldRow>
      <Show when={canPlace()}>
        <LoadError what="the places" query={locations} testid="locations-error" />
        <FieldRow label="Location" info="Where the system and its components sit. Optional: an unplaced system is still served and collected.">
          <select class="select select-bordered w-full" value={draft().location} onChange={(e) => set("location", e.currentTarget.value)} disabled={busy()}>
            <option value="">(unplaced)</option>
            <For each={locations.data ?? []}>{(l) => <option value={l.path || l.name}>{entityLabel(l)}{l.path ? ` (${l.path})` : ""}</option>}</For>
          </select>
        </FieldRow>
      </Show>
      <FieldRow bind="name" info="Optional. Lowercase letters, digits and hyphens; left empty, a name is minted from the system type. Component names are minted the same way, and each gets the lowest free address in the simulated block." hint={shown("name")}>
        <input
          class="input input-bordered w-full font-data"
          classList={{ "input-error": !!shown("name") }}
          autocomplete="off"
          placeholder="minted from the type"
          value={draft().name}
          onInput={(e) => set("name", e.currentTarget.value)}
          disabled={busy()}
        />
      </FieldRow>
    </form>
  );
}

const PILL_WARN = "badge badge-sm badge-soft badge-warning";

// LoadError: a list a select depends on could not be read; says so, with a
// retry, instead of leaving the select silently empty.
function LoadError(props: { what: string; query: UseQueryResult<unknown>; testid: string }): JSX.Element {
  return (
    <Show when={props.query.error && !props.query.data}>
      <div role="alert" class="alert alert-error alert-soft text-sm" data-testid={props.testid}>
        <span class="flex-1">Could not read {props.what}: {describeError(props.query.error)}</span>
        <Button size="xs" icon={RotateCcw} loading={props.query.isFetching} onClick={() => void props.query.refetch()}>Retry</Button>
      </div>
    </Show>
  );
}

// PlanPreview: what the chosen standard's system will hold, role by role, read
// from the plan before anything is created.
function PlanPreview(props: { plan?: SimSystemPlan; pending: boolean; error: unknown; retrying: boolean; onRetry: () => void }): JSX.Element {
  const short = () => planShortfall(props.plan);
  return (
    <section class="rounded-box border border-base-300 bg-base-200" data-testid="sim-plan" aria-live="polite">
      <div class="flex items-center gap-3 border-b border-base-300 px-3 py-2">
        <Eyebrow
          label="Plan"
          hint="What the system will hold: each of the standard's roles, the simulated product it gets and how many. A short role is one no simulated product fits; the system is created without it and reads as short of quorum, the same gap a real system missing a component shows."
        />
        <Show when={props.plan}>
          {(p) => (
            <span class="ml-auto flex items-center gap-2 text-xs">
              <Show when={short().roles.length > 0}>
                <span class={PILL_WARN} data-testid="plan-short-count">{short().roles.length} short</span>
              </Show>
              <span class="font-data tnum font-semibold" data-testid="plan-total">{p().components}</span>
              <span class="text-base-content/60">{p().components === 1 ? "component" : "components"}</span>
            </span>
          )}
        </Show>
      </div>
      <Show when={props.error && !props.plan}>
        <div role="alert" class="alert alert-error alert-soft m-3 text-sm">
          <span class="flex-1">Could not plan the system: {describeError(props.error)}</span>
          <Button size="xs" icon={RotateCcw} loading={props.retrying} onClick={() => props.onRetry()}>Retry</Button>
        </div>
      </Show>
      <Show when={!props.plan && props.pending && !props.error}>
        <div class="flex flex-col gap-2 p-3" aria-busy="true">
          <For each={[0, 1, 2]}>{() => <div class="skeleton h-6 w-full" />}</For>
        </div>
      </Show>
      <Show when={props.plan}>
        {(p) => (
          <>
            <Show when={p().components === 0}>
              <div role="alert" class="alert alert-warning alert-soft m-3 text-sm" data-testid="plan-empty">
                <span>Nothing in this standard can be simulated: no role has a simulated product that fits it.</span>
              </div>
            </Show>
            <table class="table table-fixed table-sm" data-testid="plan-roles" aria-label="Plan: each role, its simulated product and count">
              <colgroup>
                <col style={{ width: "38%" }} />
                <col />
                <col style={{ width: "68px" }} />
              </colgroup>
              <thead>
                <tr>
                  <th>Role</th>
                  <th>Product</th>
                  <th class="text-right">Count</th>
                </tr>
              </thead>
              <tbody>
                <For each={p().roles ?? []}>{(r) => <PlanRow role={r} />}</For>
              </tbody>
            </table>
          </>
        )}
      </Show>
    </section>
  );
}

function PlanRow(props: { role: SimRoleFill }) {
  const r = () => props.role;
  const isShort = () => !r().product || (r().missing ?? 0) > 0;
  return (
    <tr data-testid="plan-role" data-role={r().role} data-short={isShort() ? "true" : undefined}>
      <td>
        <span class="flex min-w-0 flex-col">
          <span class="truncate font-data text-xs" title={r().role}>{r().role}</span>
          <Show when={r().alternate}>
            <span class="truncate font-data text-[11px] text-base-content/50" title={`Alternate taken: ${r().alternate}`}>{r().alternate}</span>
          </Show>
        </span>
      </td>
      <td>
        <Show
          when={!isShort()}
          fallback={
            <span class="flex min-w-0 flex-col items-start gap-0.5" data-testid="plan-short">
              <span class={PILL_WARN}>short {r().missing ?? 0}</span>
              <span class="text-[11px] leading-snug text-base-content/60">{r().reason ?? "nothing simulated fits"}</span>
            </span>
          }
        >
          <span class="block truncate font-data text-xs text-base-content/70" title={r().product}>{r().product}</span>
        </Show>
      </td>
      <td class="text-right font-data tnum text-sm" classList={{ "text-base-content/40": isShort() }}>{r().count}</td>
    </tr>
  );
}
