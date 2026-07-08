import { For, Show, createSignal } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Drawer from "./Drawer";
import { Plus } from "./icons";
import { useMe, can } from "../lib/auth";
import { describeError } from "../lib/format";
import { createInterface, INTERFACES_KEY, type Interface } from "../lib/interfaces";
import { createTask, TASKS_KEY } from "../lib/tasks";
import { NODES_KEY, listNodes } from "../lib/nodes";
import { REACHABILITY_KEY } from "../lib/reachability";

// AddReachabilityCheck is the component-scoped "Add check" affordance on the
// Reachability panel. It authors a valid reachability check the way the node runs
// one: an interface (type = the chosen protocol, owned by THIS component, on a node,
// with params.target) and then a poll task over it (mode = poll, enabled). Creating
// the check is two writes, so it handles the seam honestly: if the task create fails
// after the interface already exists, it says so (the interface is created; retry
// only re-attempts the task) instead of swallowing the error. Gated on BOTH
// interface:create and task:create, the two permissions the writes need; the server
// is still the authority.
const PROTOCOLS = ["tcp", "icmp"] as const;

export default function AddReachabilityCheck(props: { component: string }) {
  const me = useMe();
  const allowed = () => can(me.data, "interface", "create") && can(me.data, "task", "create");
  const [open, setOpen] = createSignal(false);

  return (
    <Show when={allowed()}>
      <button class="btn btn-action btn-xs gap-1" onClick={() => setOpen(true)}>
        <Plus size={13} /> Add check
      </button>
      <Show when={open()}>
        <Drawer open={true} onClose={() => setOpen(false)} title={<>Add reachability check</>}>
          <AddCheckForm component={props.component} close={() => setOpen(false)} />
        </Drawer>
      </Show>
    </Show>
  );
}

function AddCheckForm(props: { component: string; close: () => void }) {
  const qc = useQueryClient();
  const nodes = useQuery(() => ({ queryKey: NODES_KEY, queryFn: () => listNodes() }));
  const [protocol, setProtocol] = createSignal<string>(PROTOCOLS[0]);
  const [name, setName] = createSignal<string>(PROTOCOLS[0]);
  // Whether the operator has typed a name; until then the name field tracks the
  // protocol default, so switching protocol re-defaults the (untouched) name.
  const [nameTouched, setNameTouched] = createSignal(false);
  const [target, setTarget] = createSignal("");
  const [node, setNode] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);
  // The interface, once created, so a retry after a failed task create skips
  // straight to the task (re-creating the interface would be a duplicate-name 409).
  const [createdIface, setCreatedIface] = createSignal<Interface | null>(null);

  function onProtocol(p: string) {
    setProtocol(p);
    if (!nameTouched()) setName(p); // keep the default name in step with the protocol
  }

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      let iface = createdIface();
      if (!iface) {
        // Step 1: the interface. type = protocol, owner = THIS component, friendly
        // name (defaulted to the protocol, unique on this component), target in params.
        iface = await createInterface({
          name: (name().trim() || protocol()),
          type: protocol(),
          component: props.component,
          node: node() || undefined,
          params: { target: target().trim() },
        });
        setCreatedIface(iface);
        // Surface the new interface right away, even if the task step then fails.
        await qc.invalidateQueries({ queryKey: INTERFACES_KEY });
      }
      // Step 2: the poll task over the interface (by its surrogate id), on the worklist.
      await createTask({ interface_id: iface.id, mode: "poll", enabled: true });
      await Promise.all([
        qc.invalidateQueries({ queryKey: REACHABILITY_KEY(props.component) }),
        qc.invalidateQueries({ queryKey: INTERFACES_KEY }),
        qc.invalidateQueries({ queryKey: TASKS_KEY }),
      ]);
      props.close();
    } catch (er) {
      // Two-step honesty: distinguish a failure after the interface already exists
      // from a clean first-step failure. Do not swallow the partial state.
      const created = createdIface();
      if (created) {
        setErr(`The interface "${created.name}" was created, but the task could not be scheduled: ${describeError(er)} Retry to add the task.`);
      } else {
        setErr(describeError(er));
      }
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={submit}>
      <p class="text-xs text-base-content/50">
        Adds a reachability check on <span class="font-data">{props.component}</span>: an interface plus a poll task the node runs.
      </p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="add-check-protocol">Protocol</label>
        <select id="add-check-protocol" class="select select-bordered w-full" value={protocol()} onChange={(e) => onProtocol(e.currentTarget.value)} disabled={busy() || !!createdIface()}>
          <For each={PROTOCOLS}>{(p) => <option value={p}>{p}</option>}</For>
        </select>
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="add-check-name">Name</label>
        <input id="add-check-name" autocomplete="off" class="input input-bordered w-full font-data" value={name()} onInput={(e) => { setNameTouched(true); setName(e.currentTarget.value); }} disabled={busy() || !!createdIface()} required />
        <p class="mt-1 text-[11px] text-base-content/40">A friendly name, unique on this component (defaults to the protocol).</p>
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="add-check-target">Target</label>
        <input id="add-check-target" autocomplete="off" class="input input-bordered w-full font-data" value={target()} placeholder={protocol() === "tcp" ? "10.0.0.1:5000" : "10.0.0.1"} onInput={(e) => setTarget(e.currentTarget.value)} disabled={busy() || !!createdIface()} required />
        <p class="mt-1 text-[11px] text-base-content/40">host:port for tcp, host for icmp.</p>
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="add-check-node">Node</label>
        <select id="add-check-node" class="select select-bordered w-full" value={node()} onChange={(e) => setNode(e.currentTarget.value)} disabled={busy() || !!createdIface()}>
          <option value="">Unassigned</option>
          <For each={nodes.data}>{(n) => <option value={n.name}>{n.name}</option>}</For>
        </select>
      </div>
      <div class="mt-1 flex justify-end gap-2">
        <button type="button" class="btn btn-quiet btn-sm" onClick={props.close} disabled={busy()}>Cancel</button>
        <button type="submit" class="btn btn-action btn-sm" disabled={busy() || !target().trim()}>
          <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
          {createdIface() ? "Retry task" : "Add check"}
        </button>
      </div>
    </form>
  );
}
