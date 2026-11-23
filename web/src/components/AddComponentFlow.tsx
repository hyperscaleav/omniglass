import { For, Index, Match, Show, Switch, createEffect, createMemo, createSignal, on, onCleanup, type JSX } from "solid-js";
import { createStore } from "solid-js/store";
import { A, useNavigate, useSearchParams } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import FieldRow from "./FieldRow";
import Eyebrow from "./Eyebrow";
import InfoTip from "./InfoTip";
import TreeSelect from "./TreeSelect";
import LocationSelect from "./LocationSelect";
import CreateIdentity from "./CreateIdentity";
import DriverInputFields from "./DriverInputFields";
import SecretPicker from "./SecretPicker";
import PollTest, { type PollOutcome } from "./PollTest";
import FormActionBar from "./FormActionBar";
import Picker from "./ui/Picker";
import Collapsible from "./ui/Collapsible";
import { OutpostSelect } from "./endpointBlades";
import { ArrowRight, ChevronLeft, Check, FlaskConical, Zap } from "./icons";
import {
  STEPS,
  commitFlow,
  driverChoices,
  emptyDraft,
  failureStep,
  inheritedLocation,
  productOption,
  simChoice,
  slotsFor,
  splitInputs,
  stepReady,
  undoCreated,
  type Created,
  type FlowDraft,
  type FlowIO,
  type StepKey,
} from "../lib/add_component_flow";
import { PRODUCTS_KEY, listProducts } from "../lib/products";
import { COMPONENT_TYPES_KEY, listComponentTypes } from "../lib/component_types";
import { DRIVERS_KEY, listDrivers } from "../lib/drivers";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { LOCATION_TYPES_KEY, listLocationTypes } from "../lib/location_types";
import { OUTPOSTS_KEY, listOutposts } from "../lib/outposts";
import { COMPONENTS_KEY, createComponent, deleteComponent } from "../lib/components";
import { ENDPOINTS_KEY, createEndpoint, deleteEndpoint } from "../lib/endpoints";
import { assignSlot, systemSlots, systemSlotsKey } from "../lib/system_slots";
import { FLEET_VIEW_KEY } from "../lib/fleet";
import { SIMULATION_KEY, getSimulation, planSimComponent, simComponentPlanKey } from "../lib/simulation";
import { bindableSystemNodes } from "../lib/members";
import { locationTreeNodes } from "../lib/location_tree";
import { bucketPhrase, createPen, nameBucket, penIncomplete } from "../lib/namegen";
import { nameRefused, useLabelDraft } from "../lib/labeldraft";
import { pathTo } from "../lib/treeselect";
import { createFormActions } from "../lib/formactions";
import { can, useMe } from "../lib/auth";
import { describeError } from "../lib/format";
import { entityLabel } from "../lib/entities";

// AddComponentFlow (#2460): adding a device is one path that ends with data.
// Six steps on one page: the product and the name, where it sits (system,
// slot, place), the endpoint (driver, outpost, address, or a simulated device
// in a lab), the credentials (picked, never typed), a test that runs the
// driver's polls through the outpost, and the component's own page. Each
// step is the console's existing piece (the create form's identity section,
// the placement pickers, the driver inputs, the secret form, the poll test),
// not a copy. Nothing is created until "Create and test"; every way out
// short of the end takes what was created back out (lib/add_component_flow).
//
// Entry: /components/add, with ?under= (a place), ?system=, ?product= and
// ?simulated=1 to start where the operator stood. The plain create form stays
// at /components/create, linked here as the advanced way.

const IO: FlowIO = {
  createComponent: (b) => createComponent(b),
  assignSlot,
  createEndpoint: (b) => createEndpoint(b),
  deleteEndpoint,
  deleteComponent,
};

export default function AddComponentFlow(): JSX.Element {
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();
  const [params] = useSearchParams();
  const param = (k: string) => {
    const v = params[k];
    return (Array.isArray(v) ? v[0] : v) || "";
  };

  const products = useQuery(() => ({ queryKey: PRODUCTS_KEY, queryFn: listProducts }));
  const types = useQuery(() => ({ queryKey: COMPONENT_TYPES_KEY, queryFn: listComponentTypes }));
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const locationTypes = useQuery(() => ({ queryKey: LOCATION_TYPES_KEY, queryFn: listLocationTypes }));
  const outposts = useQuery(() => ({ queryKey: OUTPOSTS_KEY, queryFn: () => listOutposts() }));
  const mayReadSim = () => can(me.data, "simulation", "read");
  const sim = useQuery(() => ({ queryKey: SIMULATION_KEY, queryFn: getSimulation, enabled: mayReadSim() }));

  const [d, setD] = createStore<FlowDraft>({ ...emptyDraft(), product: param("product"), system: param("system"), location: param("under") });
  const namePen = createPen();
  const displayPen = createPen();
  const [step, setStep] = createSignal<StepKey>("product");
  const [allDrivers, setAllDrivers] = createSignal(false);
  const [created, setCreated] = createSignal<Created | null>(null);
  const [finished, setFinished] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  const [outcomes, setOutcomes] = createStore<Record<string, PollOutcome | undefined>>({});

  const typesByName = createMemo(() => new Map((types.data ?? []).map((t) => [t.name, t])));
  const product = createMemo(() => products.data?.find((p) => p.name === d.product));
  const productOptions = createMemo(() => (products.data ?? []).map((p) => productOption(p, typesByName())));
  const choices = createMemo(() => driverChoices(drivers.data ?? [], products.data ?? [], d.product, allDrivers()));
  const driver = createMemo(() => drivers.data?.find((x) => x.name === d.driver));
  const split = createMemo(() => splitInputs(driver()));
  const stepIndex = () => STEPS.findIndex((s) => s.key === step());

  // The name the platform would mint, rendered by the server from the same
  // body this flow posts (ADR-0098), exactly as the plain create form asks.
  const labelDraft = useLabelDraft(() =>
    d.product ? { kind: "component" as const, body: { product: d.product, name: namePen.value().trim() || undefined, location: d.location || undefined, system: d.system || undefined } } : null,
  );
  const locationItems = createMemo(() => locationTreeNodes(locations.data ?? [], locationTypes.data ?? []));
  const bucketText = createMemo(() => {
    const b = nameBucket("", d.location);
    return bucketPhrase("component", b, b.under === "location" ? pathTo(locationItems(), b.id) : []);
  });
  createEffect(() => setD({ name: namePen.value(), label: displayPen.value(), expectedName: labelDraft.data?.name }));

  // A product suggests its driver: choosing one (or arriving with one) takes
  // its first suggestion, and changing the product starts the endpoint over.
  createEffect(
    on(
      () => [d.product, choices().length] as const,
      () => {
        if (!d.product || d.driver && choices().some((c) => c.driver.name === d.driver)) return;
        const first = choices()[0];
        if (first) setD({ driver: first.driver.name, inputs: {} });
      },
    ),
  );
  // One outpost is the outpost.
  createEffect(() => {
    if (!d.outpost && outposts.data?.length === 1) setD("outpost", outposts.data[0].name);
  });

  // Where: the system's slots this product may fill, and the system's place.
  const bindable = createMemo(() => bindableSystemNodes(systems.data ?? []));
  const mayBind = () => can(me.data, "system", "update") && bindable().length > 0;
  const systemRow = () => systems.data?.find((s) => s.id === d.system);
  const slots = useQuery(() => ({ queryKey: systemSlotsKey(d.system), queryFn: () => systemSlots(d.system), enabled: !!d.system }));
  const fitting = createMemo(() => slotsFor(slots.data ?? [], product(), types.data ?? []));
  const inherited = () => inheritedLocation(systems.data ?? [], d.system);
  function chooseSystem(id: string) {
    setD({ system: id, slot: "" });
    const at = inheritedLocation(systems.data ?? [], id);
    if (at) setD("location", at);
  }
  // A system named on arrival places the component as choosing it would.
  createEffect(on(() => systems.data, () => d.system && !d.location && chooseSystem(d.system)));

  // In a lab: a new simulated device of this product, planned by the server
  // (its free address, and the driver attach the simulator answers to).
  const simOutpost = () => (sim.data?.outposts ?? []).find((o) => o.connected)?.outpost ?? "";
  const simulatedKin = createMemo(() => (sim.data?.outposts ?? []).flatMap((o) => (o.components ?? []).filter((c) => c.product === d.product)));
  const plan = useQuery(() => ({
    queryKey: simComponentPlanKey(simOutpost(), d.product),
    queryFn: () => planSimComponent(simOutpost(), d.product),
    enabled: mayReadSim() && !!simOutpost() && !!d.product && step() === "endpoint",
    retry: false,
  }));
  const simulated = createMemo(() => (plan.data ? simChoice(plan.data) : null));
  const usingSim = () => !!simulated() && d.inputs.host === simulated()!.address && d.outpost === simulated()!.outpost;
  function useSimulated() {
    const s = simulated();
    if (s) setD({ outpost: s.outpost, driver: s.driver, inputs: { ...s.inputs } });
  }
  createEffect(on(simulated, (s) => s && param("simulated") === "1" && !d.inputs.host && useSimulated()));

  async function undo() {
    const made = created();
    if (!made) return;
    setCreated(null);
    setOutcomes({});
    for (const k of Object.keys(outcomes)) setOutcomes(k, undefined);
    try {
      await undoCreated(made, IO);
    } finally {
      await Promise.all([qc.invalidateQueries({ queryKey: [...COMPONENTS_KEY] }), qc.invalidateQueries({ queryKey: [...ENDPOINTS_KEY] }), qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] })]);
    }
  }
  // Leaving the page before the end takes the half-made component with it.
  onCleanup(() => {
    const made = created();
    if (made && !finished()) void undoCreated(made, IO).catch(() => undefined);
  });

  async function commit() {
    setBusy(true);
    setErr(null);
    try {
      const made = await commitFlow({ ...d }, IO);
      await qc.invalidateQueries({ queryKey: [...ENDPOINTS_KEY] });
      setCreated(made);
      setStep("test");
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  async function back(to?: StepKey) {
    setErr(null);
    const target = to ?? STEPS[Math.max(0, stepIndex() - 1)].key;
    if (created()) {
      setBusy(true);
      try {
        await undo();
      } catch (e) {
        setErr(describeError(e));
        return;
      } finally {
        setBusy(false);
      }
    }
    setStep(target);
  }

  async function cancel() {
    setBusy(true);
    try {
      await undo();
    } catch {
      // The rows stay, and the component's own page can delete them.
    } finally {
      setBusy(false);
      navigate("/explore");
    }
  }

  async function finish() {
    const made = created();
    if (!made) return;
    setFinished(true);
    await Promise.all([qc.invalidateQueries({ queryKey: [...COMPONENTS_KEY] }), qc.invalidateQueries({ queryKey: [...FLEET_VIEW_KEY] })]);
    navigate(`/components/${encodeURIComponent(made.component)}`);
  }

  const results = () => Object.values(outcomes).filter((o): o is PollOutcome => !!o);
  const anyRead = () => results().some((o) => o.ok);
  const firstFailure = () => results().find((o): o is Extract<PollOutcome, { ok: false }> => !o.ok)?.failure;

  const ready = () => stepReady(step(), d, driver()) && (step() !== "product" || !penIncomplete(!!labelDraft.data?.name, namePen));
  const actions = createFormActions();
  createEffect(() => {
    const s = step();
    actions.bind({
      submitLabel: s === "credentials" ? "Create and test" : s === "test" ? (anyRead() ? "Open the component" : "Keep it untested") : "Next",
      submitIcon: s === "credentials" ? Zap : s === "test" ? Check : ArrowRight,
      submit: () => {
        if (s === "credentials") return void commit();
        if (s === "test") return void finish();
        setStep(STEPS[stepIndex() + 1].key);
      },
      busy,
      disabled: () => busy() || !ready(),
      cancel: () => void cancel(),
      secondary: () => (s === "product" ? undefined : { label: "Back", icon: ChevronLeft, onClick: () => void back() }),
    });
  });

  const mayAdd = () => can(me.data, "component", "create") && can(me.data, "endpoint", "create");

  return (
    <section class="fade-in flex max-w-3xl flex-col gap-4" data-testid="add-component-flow">
      <div class="flex items-center justify-between gap-3">
        <Button class="flex-none" onClick={() => void cancel()}>{"←"} Explore</Button>
        <A href={`/components/create${param("under") ? `?under=${encodeURIComponent(param("under"))}` : ""}`} class="link link-hover text-xs text-base-content/60">
          Use the plain form (advanced)
        </A>
      </div>
      <div class="card border border-base-300 bg-base-200 og-pad flex flex-col gap-5">
        <div class="flex flex-col gap-4">
          <h2 class="text-lg font-semibold tracking-tight">Add a component</h2>
          <ol class="steps steps-horizontal w-full text-xs" aria-label="Steps">
            <Index each={STEPS}>
              {(s, i) => (
                <li
                  class="step"
                  classList={{ "step-primary": i <= stepIndex() }}
                  aria-current={s().key === step() ? "step" : undefined}
                  data-content={i < stepIndex() ? "✓" : String(i + 1)}
                >
                  <Show
                    when={i < stepIndex() && !created() && !busy()}
                    fallback={<span classList={{ "font-semibold": s().key === step() }}>{s().title}</span>}
                  >
                    <button type="button" class="link link-hover" onClick={() => setStep(s().key)}>{s().title}</button>
                  </Show>
                </li>
              )}
            </Index>
          </ol>
        </div>

        <Show when={!mayAdd()}>
          <div role="alert" class="alert alert-warning alert-soft text-sm">
            <span>Adding a component and its endpoint needs <code>component:create</code> and <code>endpoint:create</code>.</span>
          </div>
        </Show>
        <Show when={err()}>
          <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
        </Show>

        <Switch>
          <Match when={step() === "product"}>
            <div class="flex flex-col gap-5" data-step="product">
              <FieldRow label="Product" hint="What it is. The product decides its type, the driver it suggests and the name the platform gives it.">
                <Picker
                  options={productOptions()}
                  value={d.product || null}
                  onChange={(v) => setD({ product: v ?? "", driver: "", inputs: {}, slot: "" })}
                  placeholder="Type a product, its type or its vendor"
                />
              </FieldRow>
              <CreateIdentity
                kind="component"
                draft={() => labelDraft.data}
                pending={() => labelDraft.isFetching}
                nameRefused={() => nameRefused(labelDraft.error)}
                bucket={bucketText}
                namePen={namePen}
                displayPen={displayPen}
                namePlaceholder="display-2 (optional)"
                displayPlaceholder="Lobby display"
              />
            </div>
          </Match>

          <Match when={step() === "where"}>
            <div class="flex flex-col gap-4" data-step="where">
              <Show
                when={mayBind()}
                fallback={<p class="text-xs text-base-content/60">Putting a component in a system writes that system's membership, which needs <code>system:update</code> on a system you can see. Add it without one; it can join a system later.</p>}
              >
                <FieldRow label="System" hint="The system it serves. Its place comes with it.">
                  <TreeSelect items={bindable()} value={d.system} onChange={chooseSystem} rootLabel="None" />
                </FieldRow>
                <Show when={d.system}>
                  <FieldRow
                    label="Slot"
                    info="The role it fills in the system, such as its display or its codec. Only the slots this product's type may fill are offered."
                    hint={slots.isLoading ? "Reading the system's slots" : fitting().length === 0 ? "No slot of this system takes this product; it joins the system without one." : undefined}
                  >
                    <select class="select select-bordered w-full" value={d.slot} onChange={(e) => setD("slot", e.currentTarget.value)} disabled={fitting().length === 0}>
                      <option value="">No slot</option>
                      <For each={fitting()}>{(s) => <option value={s.name}>{entityLabel(s)}{s.assigned_to?.length ? ` (${s.assigned_to.length} in it)` : ""}</option>}</For>
                    </select>
                  </FieldRow>
                </Show>
              </Show>
              <FieldRow
                label="Place"
                hint={
                  d.system && inherited() && d.location === inherited()
                    ? `Taken from ${systemRow() ? entityLabel(systemRow()!) : "the system"}. The flow sets it, since the platform does not inherit a place from a system yet (#878).`
                    : "Where it physically sits."
                }
              >
                <LocationSelect value={d.location} onChange={(v) => setD("location", v)} rootLabel="None" />
              </FieldRow>
            </div>
          </Match>

          <Match when={step() === "endpoint"}>
            <div class="flex flex-col gap-4" data-step="endpoint">
              <Show when={simulated()}>
                {(s) => (
                  <div class="flex flex-col gap-2 rounded-box border border-base-300 bg-base-100 p-3" data-testid="simulated-device">
                    <div class="flex flex-wrap items-center gap-2">
                      <FlaskConical size={15} />
                      <Eyebrow label="Simulated device" hint="This is a lab: a simulator beside the outpost answers as any product it has a profile for, at an address of its own. Using one fills in the outpost, the address and the driver's inputs, so nothing has to be read off the Simulation page." />
                      <span class="text-sm">
                        A new {product() ? entityLabel(product()!) : d.product} answers at <span class="font-data">{s().address}</span> beside <span class="font-data">{s().outpost}</span>.
                      </span>
                      <div class="ml-auto">
                        <Show when={!usingSim()} fallback={<span class="badge badge-healthy badge-sm">in use</span>}>
                          <Button size="sm" intent="quiet" icon={FlaskConical} onClick={useSimulated}>Use a simulated device</Button>
                        </Show>
                      </div>
                    </div>
                    <Show when={simulatedKin().length}>
                      <Collapsible title={<span class="text-xs text-base-content/60">{simulatedKin().length} simulated {d.product} already answer, each a component of its own</span>}>
                        <ul class="grid grid-cols-[minmax(0,auto)_1fr] gap-x-4 gap-y-0.5 pt-1 text-xs">
                          <For each={simulatedKin().slice(0, 12)}>
                            {(c) => (
                              <>
                                <span class="font-data">{c.address}</span>
                                <A class="link link-hover truncate" href={`/components/${c.component_id}`}>{c.component}</A>
                              </>
                            )}
                          </For>
                        </ul>
                      </Collapsible>
                    </Show>
                  </div>
                )}
              </Show>
              <FieldRow label="Driver" info="How the outpost talks to it and what it reads. The product's own driver comes first, then the drivers other products of its type use.">
                <Picker
                  options={choices().map((c) => ({
                    value: c.driver.name,
                    label: entityLabel(c.driver),
                    detail: c.why === "product" ? "this product's driver" : c.why === "kin" ? `used by other ${typesByName().get(product()?.component_type ?? "")?.label ?? ""} products`.replace("  ", " ") : `${c.driver.spec?.transport ?? ""} driver`,
                  }))}
                  value={d.driver || null}
                  onChange={(v) => setD({ driver: v ?? "", inputs: usingSim() ? d.inputs : {} })}
                  placeholder="Choose a driver"
                />
              </FieldRow>
              <label class="flex items-center gap-2 text-xs text-base-content/60">
                <input type="checkbox" class="checkbox checkbox-xs" checked={allDrivers()} onChange={(e) => setAllDrivers(e.currentTarget.checked)} />
                Show every driver
              </label>
              <FieldRow label="Outpost" hint={outposts.data?.length === 1 ? "The only outpost, so it collects this." : "The outpost on the device's network collects it."}>
                <OutpostSelect value={d.outpost} onChange={(v) => setD("outpost", v)} allowNone={false} />
              </FieldRow>
              <DriverInputFields
                inputs={split().params}
                values={d.inputs}
                onChange={(name, value) => setD("inputs", name, value)}
                idPrefix="flow-input-"
              />
            </div>
          </Match>

          <Match when={step() === "credentials"}>
            <div class="flex flex-col gap-4" data-step="credentials">
              <Show when={split().secrets.length} fallback={<p class="text-sm text-base-content/60">This driver needs no credential. Create it and test it.</p>}>
                <p class="flex items-center gap-1 text-xs text-base-content/60">
                  Pick the secret the device accepts. The endpoint keeps its name; the value stays sealed on the server.
                  <InfoTip label="Credentials" text="A secret is named here, never copied: the outpost resolves it when it polls, so rotating the secret needs no edit to the endpoint." />
                </p>
                <DriverInputFields
                  inputs={split().secrets}
                  values={d.inputs}
                  onChange={(name, value) => setD("inputs", name, value)}
                  idPrefix="flow-input-"
                  secretControl={(input, id) => <SecretPicker id={id} secretType={input.secret_type} value={d.inputs[input.name] ?? ""} onChange={(v) => setD("inputs", input.name, v)} />}
                />
              </Show>
            </div>
          </Match>

          <Match when={step() === "test" && created()}>
            {(made) => (
              <div class="flex flex-col gap-4" data-step="test">
                <p class="text-sm text-base-content/70">
                  Created, with its endpoint on <span class="font-data">{d.outpost}</span>. The outpost runs each of the driver's polls now and shows what it read.
                </p>
                <PollTest endpoint={made().endpoint!} autoRun onResult={(poll, o) => setOutcomes(poll, o)} />
                <Show when={!anyRead() && firstFailure()}>
                  {(f) => (
                    <div role="alert" class="alert alert-warning alert-soft flex flex-wrap items-center gap-2 text-sm" data-testid="flow-fix">
                      <span class="flex-1">
                        {failureStep(f()) === "credentials" ? "The device refused the credential." : "The device did not answer as set up."} Going back takes this component out, so fixing it starts clean.
                      </span>
                      <Button size="sm" intent="quiet" icon={ChevronLeft} onClick={() => void back(failureStep(f()))} disabled={busy()}>
                        {failureStep(f()) === "credentials" ? "Fix the credentials" : "Fix the endpoint"}
                      </Button>
                    </div>
                  )}
                </Show>
              </div>
            )}
          </Match>
        </Switch>

        <FormActionBar actions={actions} inline />
      </div>
    </section>
  );
}
