import { Show, createSignal, type JSX } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Eyebrow from "./Eyebrow";
import Button from "./Button";
import SimDeviceDrawer from "./SimDeviceDrawer";
import { FlaskConical } from "./icons";
import { can, useMe } from "../lib/auth";
import { rel } from "../lib/format";
import { componentSimKey, getComponentSim, impairmentSummary, isImpaired } from "../lib/simulation";

// SimulationSection: a component's own page says when the component is a
// simulated device (#890), with where it is served, what has reached it and
// what is broken, and opens the same drawer the Simulation page does. A
// component that is not simulated, or a reader who cannot read simulation,
// renders nothing at all.
export default function SimulationSection(props: { componentId: string }): JSX.Element {
  const me = useMe();
  const sim = useQuery(() => ({
    queryKey: componentSimKey(props.componentId),
    queryFn: () => getComponentSim(props.componentId),
    enabled: can(me.data, "simulation", "read"),
    refetchInterval: 5_000,
    retry: false,
  }));
  const [open, setOpen] = createSignal(false);
  const navigate = useNavigate();
  return (
    <Show when={sim.data?.simulated ? sim.data : undefined}>
      {(s) => (
        <section data-testid="leaf-simulation" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
          <Eyebrow
            label="Simulation"
            hint="A simulator beside the node answers for this component: a loopback address speaking the device's protocols, so collection reads it as real hardware. Impair it to watch monitoring notice."
            right={<Button size="xs" icon={FlaskConical} onClick={() => setOpen(true)}>Open device</Button>}
          />
          <dl class="mt-2 grid grid-cols-[6rem_1fr] gap-x-3 gap-y-1">
            <dt class="text-base-content/50">Node</dt>
            <dd class="font-data text-xs">{s().node}</dd>
            <dt class="text-base-content/50">Address</dt>
            <dd class="font-data text-xs">{s().address}</dd>
            <dt class="text-base-content/50">Serving</dt>
            <dd>
              <span class={s().device?.serving ? "badge badge-sm badge-ghost" : "badge badge-sm badge-soft badge-warning"}>
                {s().device?.serving ? "serving" : "not serving"}
              </span>
            </dd>
            <dt class="text-base-content/50">Hits</dt>
            <dd class="font-data text-xs tnum">
              {(s().device?.hits ?? 0).toLocaleString()}
              <Show when={s().device?.last_hit_at}>
                <span class="ml-2 text-base-content/50">last {rel(s().device!.last_hit_at!)}{s().device?.last_protocol ? ` via ${s().device!.last_protocol}` : ""}</span>
              </Show>
            </dd>
            <dt class="text-base-content/50">Impairment</dt>
            <dd>
              <span class={isImpaired(s().device?.impairment) ? "badge badge-sm badge-soft badge-warning" : "badge badge-sm badge-healthy"}>
                {impairmentSummary(s().device?.impairment)}
              </span>
            </dd>
          </dl>
          <SimDeviceDrawer id={open() ? props.componentId : null} onClose={() => setOpen(false)} onRemoved={() => navigate("/simulation")} showOpen={false} />
        </section>
      )}
    </Show>
  );
}
