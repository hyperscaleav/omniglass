import { Show, For, createMemo, createSignal, type JSX } from "solid-js";
import { Dialog } from "@kobalte/core/dialog";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Button from "../components/Button";
import { Check, Copy, Server } from "../components/icons";
import { Fact } from "../components/DetailShell";
import {
  type Node,
  type EnrollOutput,
  type NodeStatus,
  NODES_KEY,
  listNodes,
  createNode,
  enrollNode,
  nodeStatus,
  nodeFilterKeys,
} from "../lib/nodes";
import { TASKS_KEY, listTasks } from "../lib/tasks";
import { INTERFACES_KEY, listInterfaces } from "../lib/interfaces";
import { useMe, can } from "../lib/auth";
import { describeError, rel } from "../lib/format";
import { type BladeDef, useBladeEdit } from "../lib/blades";

// Nodes: the collection-daemon inventory, a config over the shared FlatList (the
// flat sibling of the inventory TreeList). A row per node with its liveness pill
// (derived client-side from last_heartbeat_at against the server's down window),
// a row opening the blade detail (facts + the derived Tasks panel + an Enroll /
// Re-enroll action), and "New node" opening the create Drawer. Day-one enrollment
// mints a secret token shown ONCE in a modal; it is never cached or logged. The
// tasks a node runs are DERIVED read-only plumbing, so they read as a panel on the
// node, not a separate nav entry. Every gate is a UI hint; the server is the
// authority.
// Per-status pill class. up/down carry a soft hue; "never" (no heartbeat yet) is a
// neutral state given a soft grey fill (a tint of the text color, so it reads as a
// visible pill in both themes). The daisyUI neutral badge renders near-black against
// the dark surface and badge-ghost renders transparent, so neither works here.
const NEUTRAL_PILL = "bg-base-content/10 text-base-content/70 border-transparent";
const STATUS: Record<NodeStatus, { label: string; badge: string }> = {
  up: { label: "up", badge: "badge-soft badge-success" },
  down: { label: "down", badge: "badge-soft badge-error" },
  never: { label: "never", badge: NEUTRAL_PILL },
};

function StatusPill(props: { node: Node }) {
  const s = () => nodeStatus(props.node);
  return <span class={`badge badge-sm ${STATUS[s()].badge}`}>{STATUS[s()].label}</span>;
}

// The node columns: Name carries a server glyph + the node name (its address) and
// an optional description; Status the derived liveness pill; Last heartbeat the
// relative time (or a muted dash for a node that has never checked in).
const columns: FlatColumn<Node>[] = [
  {
    key: "name", label: "Name", sortVal: (n) => n.name.toLowerCase(),
    cell: (n) => (
      <div class="flex items-center gap-2.5">
        <span class="text-base-content/40"><Server size={16} /></span>
        <div class="min-w-0 leading-tight">
          <div class="truncate font-data text-sm font-medium">{n.name}</div>
          <Show when={n.description}><div class="truncate text-[11px] text-base-content/40">{n.description}</div></Show>
        </div>
      </div>
    ),
  },
  {
    key: "status", label: "Status", width: "120px", sortVal: (n) => nodeStatus(n),
    cell: (n) => <StatusPill node={n} />,
  },
  {
    key: "heartbeat", label: "Last heartbeat", width: "160px", sortVal: (n) => n.last_heartbeat_at ?? "",
    cell: (n) => (n.last_heartbeat_at ? <span class="tnum text-base-content/60">{rel(n.last_heartbeat_at)}</span> : <span class="text-base-content/30">-</span>),
  },
];

export default function Nodes() {
  const me = useMe();
  const nodes = useQuery(() => ({ queryKey: NODES_KEY, queryFn: () => listNodes() }));
  // The once-shown enrollment token lives ONLY in this signal for the modal's
  // lifetime: enroll hands it here, the modal reveals it, and onClose clears it. It
  // is never written to the query cache, localStorage, or a log.
  const [enrollResult, setEnrollResult] = createSignal<EnrollOutput | null>(null);

  // The node blade closes over setEnrollResult so its Enroll action can hand the
  // minted token to the page-level show-once modal (the blade body only receives an
  // id, so the callback rides in on the registry entry).
  const nodeBlade: BladeDef = {
    Title: (p) => <NodeBladeTitle name={p.id} />,
    Body: (p) => <NodeBladeBody name={p.id} onEnrolled={setEnrollResult} />,
  };

  return (
    <>
      <FlatList<Node>
        config={{
          entity: { name: "node", plural: "nodes" },
          rows: () => nodes.data ?? [],
          loading: () => nodes.isLoading,
          error: () => nodes.error,
          filterKeys: nodeFilterKeys,
          filterPlaceholder: "filter by name or status",
          columns,
          empty: "No nodes yet.",
          rowId: (n) => n.name,
          blades: { registry: { node: nodeBlade }, rootKind: "node" },
          create: {
            label: "New node",
            can: () => can(me.data, "node", "create") && can(me.data, "node", "enroll"),
            body: (ctx) => <CreateNodeForm close={ctx.close} onEnrolled={(out) => { setEnrollResult(out); ctx.close(); }} />,
          },
        }}
      />
      <EnrollTokenModal result={enrollResult()} onClose={() => setEnrollResult(null)} />
    </>
  );
}

function NodeBladeTitle(props: { name: string }): JSX.Element {
  const nodes = useQuery(() => ({ queryKey: NODES_KEY, queryFn: () => listNodes() }));
  const n = () => (nodes.data ?? []).find((x) => x.name === props.name);
  return <span class="font-data">{n()?.name ?? props.name}</span>;
}

// NodeBladeBody is the node's detail on the shared blade stack (same chrome and
// footer action rail as the identity blades). It re-derives the node from the live
// query by name (not a row snapshot), so a re-enroll (which flips enrolled / stamps
// enrolled_at) reflects after the invalidate. A node has no editable fields, so no
// pencil; the footer carries Enroll / Re-enroll as its one prominent action. The
// Tasks panel lists the node's derived tasks (read-only): a task's node placement
// projects from its interface, so it is read here, never authored.
function NodeBladeBody(props: { name: string; onEnrolled: (out: EnrollOutput) => void }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const edit = useBladeEdit();
  const nodes = useQuery(() => ({ queryKey: NODES_KEY, queryFn: () => listNodes() }));
  const tasks = useQuery(() => ({ queryKey: TASKS_KEY, queryFn: () => listTasks() }));
  const interfaces = useQuery(() => ({ queryKey: INTERFACES_KEY, queryFn: () => listInterfaces() }));
  const n = createMemo(() => nodes.data?.find((x) => x.name === props.name) ?? null);
  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  const canEnroll = () => can(me.data, "node", "enroll");

  // The node's derived tasks. A task has no name: it is a binding, a function running
  // over an interface, so it reads as its interface (the anchor, resolved from the
  // surrogate interface_id) plus the function it runs, never a redundant label. The
  // function name arrives with device drivers, so today it reads as the built-in check
  // with a provisional marker. Read-only: a task is derived, never authored here.
  const ifaceName = (id: string) => interfaces.data?.find((i) => i.id === id)?.name ?? id;
  const nodeTasks = createMemo(() =>
    (tasks.data ?? [])
      .filter((t) => t.node === props.name)
      .map((t) => ({ id: t.id, iface: ifaceName(t.interface_id), enabled: t.enabled })),
  );

  async function doEnroll() {
    const node = n();
    if (!node || busy()) return;
    setBusy(true);
    setErr(null);
    try {
      const out = await enrollNode(node.name);
      await qc.invalidateQueries({ queryKey: NODES_KEY });
      props.onEnrolled(out);
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  // A node has no operator-editable fields (name / description are set at creation),
  // so the body is never editable; its one prominent footer action is Enroll /
  // Re-enroll, gated on node:enroll.
  edit.bind({
    editable: () => false,
    primary: () => (canEnroll() && n() ? { label: n()!.enrolled ? "Re-enroll" : "Enroll", onClick: () => void doEnroll() } : undefined),
  });

  return (
    <Show when={n()} fallback={<p class="text-sm text-base-content/50">This node is no longer available.</p>}>
      {(node) => (
        <div class="flex flex-col gap-4">
          <div class="flex items-center gap-3">
            <span class="text-base-content/40"><Server size={22} /></span>
            <StatusPill node={node()} />
          </div>

          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>

          <div class="grid grid-cols-2 gap-4">
            <Fact label="Name" value={<span class="font-data">{node().name}</span>} />
            <Fact label="Status" value={STATUS[nodeStatus(node())].label} />
            <Fact label="Last heartbeat" value={node().last_heartbeat_at ? rel(node().last_heartbeat_at!) : <span class="text-base-content/40">never</span>} />
            <Fact label="Enrolled" value={node().enrolled ? (node().enrolled_at ? rel(node().enrolled_at!) : "yes") : <span class="text-base-content/40">not yet</span>} />
            <Show when={node().description}>
              <Fact label="Description" value={node().description} />
            </Show>
          </div>

          <div class="flex flex-col gap-1.5">
            <div class="flex items-baseline gap-1.5">
              <span class="eyebrow">Tasks</span>
              <Show when={nodeTasks().length}><span class="text-xs text-base-content/40">{nodeTasks().length}</span></Show>
            </div>
            <Show
              when={nodeTasks().length}
              fallback={<p class="text-xs text-base-content/40">No tasks. A task derives when an interface placed on this node is created.</p>}
            >
              <div class="overflow-hidden rounded-box border border-base-300">
                <For each={nodeTasks()}>
                  {(t, i) => (
                    <div class="flex items-center gap-2 px-3 py-1.5 text-sm" classList={{ "border-t border-base-300": i() > 0 }}>
                      <span class="font-data text-base-content/80">{t.iface}</span>
                      <span class="text-base-content/25">/</span>
                      <span class="flex items-center gap-1.5 text-base-content/50">
                        reachability
                        <span class="rounded bg-base-content/5 px-1 py-px text-[9px] font-medium uppercase tracking-wide text-base-content/40" title="Named collection functions arrive with device drivers">driver fn soon</span>
                      </span>
                      <span class="flex-1" />
                      <span class={`badge badge-xs ${t.enabled ? "badge-soft badge-success" : "bg-base-content/10 text-base-content/70 border-transparent"}`}>{t.enabled ? "enabled" : "disabled"}</span>
                    </div>
                  )}
                </For>
              </div>
            </Show>
          </div>

          <Show when={canEnroll()}>
            <p class="text-xs text-base-content/50">
              {node().enrolled ? "Re-mint the enrollment token (the old one stops working)." : "Mint the enrollment token to connect this node."}
            </p>
          </Show>
        </div>
      )}
    </Show>
  );
}

// CreateNodeForm is the new-node form the create Drawer hosts: name (the node's
// address, required) and an optional description. Day one, a node is created then
// enrolled immediately, so on success it invalidates the list and hands the minted
// token to onEnrolled, which reveals it in the show-once modal (closing this
// Drawer). The token is never held here.
function CreateNodeForm(props: { close: () => void; onEnrolled: (out: EnrollOutput) => void }) {
  const qc = useQueryClient();
  const [name, setName] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function submit(e: SubmitEvent) {
    e.preventDefault();
    setBusy(true);
    setErr(null);
    try {
      const created = await createNode({ name: name().trim(), description: description().trim() || undefined });
      await qc.invalidateQueries({ queryKey: NODES_KEY });
      // Day-one enrollment: mint the token now so the operator can hand it to the
      // node deployment. Shown once (next), never cached.
      const out = await enrollNode(created.name);
      props.onEnrolled(out);
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={submit}>
      <p class="text-xs text-base-content/50">Registers an edge node and mints its enrollment token. The name is the node's address (no dots or whitespace); the token is shown once.</p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-node-name">Name</label>
        <input id="new-node-name" autocomplete="off" class="input input-bordered w-full font-data" value={name()} placeholder="edge-hq-1" onInput={(e) => setName(e.currentTarget.value)} disabled={busy()} required />
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-node-desc">Description</label>
        <input id="new-node-desc" autocomplete="off" class="input input-bordered w-full" value={description()} placeholder="HQ network closet" onInput={(e) => setDescription(e.currentTarget.value)} disabled={busy()} />
      </div>
      <div class="mt-1 flex justify-end gap-2">
        <Button type="button" intent="quiet" onClick={props.close} disabled={busy()}>Cancel</Button>
        <Button type="submit" intent="action" disabled={busy() || !name().trim()}>
          <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
          Create node
        </Button>
      </div>
    </form>
  );
}

// EnrollTokenModal reveals the enrollment token exactly once (a centered Kobalte
// Dialog, portaled so it escapes any overflow). The token is a secret: a clear
// once-only warning, a monospace readonly field, and a copy-to-clipboard button
// with a copied confirmation. It holds no token of its own; it reads props.result,
// which the page clears on close, so the secret does not outlive the modal.
function EnrollTokenModal(props: { result: EnrollOutput | null; onClose: () => void }) {
  const [copied, setCopied] = createSignal(false);

  async function copy(token: string) {
    try {
      await navigator.clipboard.writeText(token);
      setCopied(true);
      setTimeout(() => setCopied(false), 2000);
    } catch {
      // Clipboard blocked (permissions, insecure context): the field stays
      // selectable so the operator can copy it by hand. The token is not logged.
    }
  }

  return (
    <Dialog open={!!props.result} onOpenChange={(o) => !o && props.onClose()}>
      <Dialog.Portal>
        <Dialog.Overlay class="fixed inset-0 z-[70] bg-black/50" />
        <div class="fixed inset-0 z-[70] flex items-center justify-center p-4">
          <Dialog.Content class="flex w-full max-w-lg flex-col gap-4 rounded-box border border-base-300 bg-base-100 p-6 shadow-2xl">
            <Dialog.Title class="text-base font-semibold">Enrollment token for <span class="font-data">{props.result?.name}</span></Dialog.Title>
            <div role="alert" class="alert alert-warning alert-soft text-sm">
              <span>This token is shown once. Copy it now; it cannot be retrieved again.</span>
            </div>
            <div class="flex items-stretch gap-2">
              <input readonly value={props.result?.token ?? ""} aria-label="Enrollment token" class="input input-bordered w-full font-data text-xs" onFocus={(e) => e.currentTarget.select()} />
              <Button intent="action" onClick={() => props.result && copy(props.result.token)}>
                <Show when={copied()} fallback={<><Copy size={14} /> Copy</>}><Check size={14} /> Copied</Show>
              </Button>
            </div>
            <p class="text-xs text-base-content/50">Hand it to the node deployment; the node presents it to claim its NATS credential. The server stores only a hash and never logs it.</p>
            <div class="flex justify-end">
              <Button intent="quiet" onClick={props.onClose}>Done</Button>
            </div>
          </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog>
  );
}
