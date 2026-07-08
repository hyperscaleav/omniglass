import { Show, createMemo, createSignal } from "solid-js";
import { Dialog } from "@kobalte/core/dialog";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import { Check, Copy, Server } from "../components/icons";
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
import { useMe, can } from "../lib/auth";
import { describeError, rel } from "../lib/format";

// Nodes: the collection-daemon inventory, a config over the shared FlatList (the
// flat sibling of the inventory TreeList). A row per node with its liveness pill
// (derived client-side from last_heartbeat_at against the server's down window),
// a row opening the side Drawer detail (facts + an Enroll / Re-enroll action), and
// "New node" opening the create Drawer. Day-one enrollment mints a secret token
// shown ONCE in a modal; it is never cached or logged. Every gate is a UI hint;
// the server is the authority.
// Per-status pill class. up/down carry a soft hue; "never" (no heartbeat yet) is a
// neutral state, so it uses badge-ghost, which reads in both themes (badge-neutral
// renders near-black against the dark surface).
const STATUS: Record<NodeStatus, { label: string; badge: string }> = {
  up: { label: "up", badge: "badge-soft badge-success" },
  down: { label: "down", badge: "badge-soft badge-error" },
  never: { label: "never", badge: "badge-ghost" },
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
  // lifetime: enrollNode hands it here, the modal reveals it, and onClose clears
  // it. It is never written to the query cache, localStorage, or a log.
  const [enrollResult, setEnrollResult] = createSignal<EnrollOutput | null>(null);

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
          detail: (n) => ({
            title: <span class="font-data">{n.name}</span>,
            body: <NodeDetail name={n.name} canEnroll={can(me.data, "node", "enroll")} onEnrolled={setEnrollResult} />,
          }),
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

function Fact(props: { label: string; value: unknown }) {
  return (
    <div>
      <div class="eyebrow">{props.label}</div>
      <div>{props.value as never}</div>
    </div>
  );
}

// NodeDetail is the row's side-Drawer body. It re-derives the node from the live
// query by name (not the row snapshot the Drawer opened with), so a re-enroll
// (which flips enrolled / stamps enrolled_at) reflects after the invalidate. The
// Enroll / Re-enroll action is gated by node:enroll; a re-enroll invalidates the
// previous token, so the copy is the operator's only chance to keep it.
function NodeDetail(props: { name: string; canEnroll: boolean; onEnrolled: (out: EnrollOutput) => void }) {
  const qc = useQueryClient();
  const nodes = useQuery(() => ({ queryKey: NODES_KEY, queryFn: () => listNodes() }));
  const n = createMemo(() => nodes.data?.find((x) => x.name === props.name) ?? null);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  async function doEnroll(node: Node) {
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

  return (
    <Show when={n()} fallback={<p class="py-8 text-center text-sm text-base-content/40">This node is no longer available.</p>}>
      {(node) => (
        <div class="flex flex-col gap-3">
          <div class="flex items-center gap-3">
            <span class="text-base-content/40"><Server size={22} /></span>
            <StatusPill node={node()} />
          </div>

          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>

          <div class="grid grid-cols-2 gap-3 text-sm">
            <Fact label="Name" value={<span class="font-data">{node().name}</span>} />
            <Fact label="Status" value={STATUS[nodeStatus(node())].label} />
            <Fact label="Last heartbeat" value={node().last_heartbeat_at ? rel(node().last_heartbeat_at!) : <span class="text-base-content/40">never</span>} />
            <Fact label="Enrolled" value={node().enrolled ? (node().enrolled_at ? rel(node().enrolled_at!) : "yes") : <span class="text-base-content/40">not yet</span>} />
            <Show when={node().description}>
              <Fact label="Description" value={node().description} />
            </Show>
          </div>

          <Show when={props.canEnroll}>
            <div class="flex items-center gap-2 border-t border-base-300 pt-3">
              <span class="text-xs text-base-content/50">
                {node().enrolled ? "Re-mint the enrollment token (the old one stops working)." : "Mint the enrollment token to connect this node."}
              </span>
              <span class="flex-1" />
              <button class="btn btn-action btn-sm" onClick={() => doEnroll(node())} disabled={busy()}>
                <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
                {node().enrolled ? "Re-enroll" : "Enroll"}
              </button>
            </div>
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
        <button type="button" class="btn btn-quiet btn-sm" onClick={props.close} disabled={busy()}>Cancel</button>
        <button type="submit" class="btn btn-action btn-sm" disabled={busy() || !name().trim()}>
          <Show when={busy()}><span class="loading loading-spinner loading-xs" /></Show>
          Create node
        </button>
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
              <button class="btn btn-action btn-sm gap-1.5" onClick={() => props.result && copy(props.result.token)}>
                <Show when={copied()} fallback={<><Copy size={14} /> Copy</>}><Check size={14} /> Copied</Show>
              </button>
            </div>
            <p class="text-xs text-base-content/50">Hand it to the node deployment; the node presents it to claim its NATS credential. The server stores only a hash and never logs it.</p>
            <div class="flex justify-end">
              <button class="btn btn-quiet btn-sm" onClick={props.onClose}>Done</button>
            </div>
          </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog>
  );
}
