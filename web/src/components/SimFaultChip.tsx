import { Show, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import { Plug, X } from "./icons";
import { can, useMe } from "../lib/auth";
import { describeError } from "../lib/format";
import { pushToast } from "../lib/toast";
import { applyFaultSteps, activeFaults, age, restoreSteps, type HeldFault } from "../lib/simfaults";
import { componentSimKey, getComponentSim, componentSimPollMs, toImpairment, type SimComponent } from "../lib/simulation";

// SimFaultChip: what an operator has broken on a simulated device, as one chip
// with how long it has been so and a one-click clear (#2462). It reads the
// simulator's own account (the impairment and the state paths it reports held
// off their defaults), so a reload shows it; it renders nothing for a healthy
// device. Clear is Restore: it repairs the impairment and sets every held path
// back, and is offered only to a caller who may update the simulation (the
// route's own permission), so a viewer sees the fault and cannot clear it.
//
// Plain props, so it mounts anywhere a simulated device is shown: the
// Simulation row, the drawer, and the component's page beside the alarm it
// causes (SimFaultChipFor).
export default function SimFaultChip(props: {
  componentId: string;
  sim: Pick<SimComponent, "impairment" | "impairment_since" | "faults"> | undefined;
  // The device's name, for the clear button's accessible name.
  name?: string;
  size?: "xs" | "sm";
}): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const [busy, setBusy] = createSignal(false);
  const faults = () => (props.sim ? activeFaults(props.sim) : []);
  const canClear = () => can(me.data, "simulation", "update");

  async function clear() {
    if (!props.sim) return;
    setBusy(true);
    try {
      // The surface says which route puts each held path back; a row has none
      // of it, so the clear reads it (the drawer and page already hold it).
      const surfaces = (await qc.fetchQuery({ queryKey: componentSimKey(props.componentId), queryFn: () => getComponentSim(props.componentId), staleTime: 30_000 })).surface ?? [];
      const held: HeldFault[] = (props.sim.faults ?? []).map((f) => ({ path: f.path, value: f.value, default: f.default, since: f.since }));
      await applyFaultSteps(qc, props.componentId, restoreSteps(surfaces, { impairment: toImpairment(props.sim.impairment), faults: held }));
    } catch (e) {
      pushToast({ tone: "error", title: `Could not clear the fault on ${props.name ?? "the device"}`, body: () => <>{describeError(e)}</> });
    } finally {
      setBusy(false);
    }
  }

  return (
    <Show when={faults().length > 0}>
      <span
        class="badge badge-soft badge-warning max-w-full min-w-0 gap-1 whitespace-nowrap pr-0.5"
        classList={{ "badge-sm": (props.size ?? "sm") === "sm", "badge-xs": props.size === "xs", "pr-2": !canClear() }}
        title={faults().map((f) => (f.since ? `${f.label}, ${age(f.since)}` : f.label)).join("; ")}
        data-testid="sim-fault-chip"
      >
        <Plug size={12} />
        <span class="truncate" data-testid="sim-fault-label">
          {faults()[0].label}
          <Show when={faults().length > 1}> +{faults().length - 1}</Show>
        </span>
        <Show when={faults()[0].since}>
          {(t) => <span class="tnum text-base-content/60" data-testid="sim-fault-age">{age(t())}</span>}
        </Show>
        <Show when={canClear()}>
          <span data-testid="sim-fault-clear" class="inline-flex">
            <Button
              square
              size="xs"
              icon={X}
              loading={busy()}
              class="h-4 min-h-0 w-4"
              label={`Clear the fault on ${props.name ?? "the device"}`}
              title="Clear: repair the impairment and set every held value back to its default"
              onClick={(e) => {
                e.stopPropagation();
                void clear();
              }}
            />
          </span>
        </Show>
      </span>
    </Show>
  );
}

// SimFaultChipFor: the chip for a component that may or may not be simulated,
// on the component's own page (#2462), where the alarm the fault causes is.
// It reads the component's simulation (polled, as the page's Simulation card
// is), and renders nothing for a component that is not simulated, one the
// caller cannot read simulation of, or one with nothing broken.
export function SimFaultChipFor(props: { componentId: string; name?: string }): JSX.Element {
  const me = useMe();
  const sim = useQuery(() => ({
    queryKey: componentSimKey(props.componentId),
    queryFn: () => getComponentSim(props.componentId),
    enabled: can(me.data, "simulation", "read"),
    refetchInterval: (q) => componentSimPollMs(q.state.data),
    retry: false,
  }));
  return (
    <Show when={sim.data?.simulated ? sim.data.activity : undefined}>
      {(a) => <SimFaultChip componentId={props.componentId} sim={a()} name={props.name} />}
    </Show>
  );
}
