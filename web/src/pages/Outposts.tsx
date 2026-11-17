import { createIdentity, entityLabel } from "../lib/entities";
import { Show, For, createEffect, createMemo, createSignal, on, type JSX } from "solid-js";
import { Dialog } from "@kobalte/core/dialog";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useSearchParams } from "@solidjs/router";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import BladeField from "../components/BladeField";
import { identityColumn } from "../components/IdentityCell";
import Button from "../components/Button";
import { useFormActions } from "../lib/formactions";
import { bindSelectValue } from "../lib/selectvalue";
import { Check, Copy, Plus, Server } from "../components/icons";
import KVStacked from "../components/KVStacked";
import LogsPanel from "../components/LogsPanel";
import { OUTPOST_LOGS_KEY, getOutpostLogs } from "../lib/logs";
import TagAdder from "../components/TagAdder";
import TagPills from "../components/TagPills";
import { tagFilterKeys } from "../lib/predicate";
import {
  type Outpost,
  type EnrollOutput,
  type OutpostStatus,
  OUTPOSTS_KEY,
  listOutposts,
  createOutpost,
  updateOutpost,
  deleteOutpost,
  enrollOutpost,
  outpostStatus,
  outpostLabel,
  outpostFilterKeys,
} from "../lib/outposts";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { TASKS_KEY, listTasks } from "../lib/tasks";
import { ENDPOINTS_KEY, listEndpoints } from "../lib/endpoints";
import { useMe, can } from "../lib/auth";
import { describeError, rel } from "../lib/format";
import { pushToast } from "../lib/toast";
import { type BladeDef, useBlades, useBladeEdit } from "../lib/blades";

// Outposts: the collection-daemon inventory, a config over the shared FlatList (the
// flat sibling of the inventory TreeList). A row per outpost with its liveness pill
// (derived client-side from last_heartbeat_at against the server's down window),
// a row opening the blade detail (facts + the derived Tasks panel + an Enroll /
// Re-enroll action), and "New outpost" opening the create Drawer. Creating an outpost does
// not enroll it: Enroll is a separate act on the blade (primary until the outpost is
// enrolled) that mints a secret token shown ONCE in a modal; it is never cached or logged. The
// tasks an outpost runs are DERIVED read-only plumbing, so they read as a panel on the
// outpost, not a separate nav entry. Every gate is a UI hint; the server is the
// authority.
// Per-status pill class. up/down carry a soft hue; "never" (no heartbeat yet) is a
// neutral state given a soft grey fill (a tint of the text color, so it reads as a
// visible pill in both themes). The daisyUI neutral badge renders near-black against
// the dark surface and badge-ghost renders transparent, so neither works here.
const NEUTRAL_PILL = "bg-base-content/10 text-base-content/70 border-transparent";
const STATUS: Record<OutpostStatus, { label: string; badge: string }> = {
  up: { label: "up", badge: "badge-soft badge-success" },
  down: { label: "down", badge: "badge-soft badge-error" },
  never: { label: "never", badge: NEUTRAL_PILL },
};

function StatusPill(props: { outpost: Outpost }) {
  const s = () => outpostStatus(props.outpost);
  return <span class={`badge badge-sm ${STATUS[s()].badge}`}>{STATUS[s()].label}</span>;
}

// The outpost columns: Name is the shared identity cell (label over address, the
// address suppressed when it IS the label), so an outpost reads the way every other
// entity in the console reads; Status the derived liveness pill; Last heartbeat the
// relative time (or a muted dash for an outpost that has never checked in).
const columns: FlatColumn<Outpost>[] = [
  identityColumn<Outpost>(),
  {
    key: "status", label: "Status", width: "120px", sortVal: (n) => outpostStatus(n),
    cell: (n) => <StatusPill outpost={n} />,
  },
  {
    key: "heartbeat", label: "Last heartbeat", width: "160px", sortVal: (n) => n.last_heartbeat_at ?? "",
    cell: (n) => (n.last_heartbeat_at ? <span class="tnum text-base-content/60">{rel(n.last_heartbeat_at)}</span> : <span class="text-base-content/30">-</span>),
  },
  {
    key: "tags", label: "Tags", width: "300px",
    sortVal: (n) => Object.keys(n.tags).sort().join(","),
    cell: (n) => <TagPills tags={n.tags} />,
  },
];

export default function Outposts() {
  const me = useMe();
  const outposts = useQuery(() => ({ queryKey: OUTPOSTS_KEY, queryFn: () => listOutposts() }));
  // ?n=<name> deep-links to an outpost's blade (Platform health's row link, #1644).
  const [params] = useSearchParams();
  const openId = () => (Array.isArray(params.n) ? params.n[0] : params.n) || undefined;

  // One filter facet per tag key present across the outposts, derived from their
  // effective tags, so the bar can filter by any tag like any other field
  // (mirrors the Components list).
  const tagFacets = createMemo(() => {
    const keys = new Set<string>();
    for (const n of outposts.data ?? []) for (const k of Object.keys(n.tags)) keys.add(k);
    return tagFilterKeys<Outpost>([...keys].sort(), new Set(["name", "status", "location"]));
  });
  // The once-shown enrollment token lives ONLY in this signal for the modal's
  // lifetime: enroll hands it here, the modal reveals it, and onClose clears it. It
  // is never written to the query cache, localStorage, or a log.
  const [enrollResult, setEnrollResult] = createSignal<EnrollOutput | null>(null);

  // The outpost blade closes over setEnrollResult so its Enroll action can hand the
  // minted token to the page-level show-once modal (the blade body only receives an
  // id, so the callback rides in on the registry entry).
  const outpostBlade: BladeDef = {
    Title: (p) => <OutpostBladeTitle name={p.id} />,
    Body: (p) => <OutpostBladeBody name={p.id} onEnrolled={setEnrollResult} />,
  };

  return (
    <>
      <FlatList<Outpost>
        config={{
          entity: { name: "outpost", plural: "outposts" },
          rows: () => outposts.data ?? [],
          loading: () => outposts.isLoading,
          error: () => outposts.error,
          filterKeys: () => [...outpostFilterKeys, ...tagFacets()],
          filterPlaceholder: "filter by name, status, or tag",
          columns,
          empty: "No outposts yet.",
          rowId: (n) => n.name,
          openId,
          blades: { registry: { outpost: outpostBlade }, rootKind: "outpost" },
          create: {
            label: "New outpost",
            can: () => can(me.data, "outpost", "create"),
            body: (ctx) => <CreateOutpostForm close={ctx.close} onCreated={ctx.select} />,
          },
        }}
      />
      <EnrollTokenModal result={enrollResult()} onClose={() => setEnrollResult(null)} />
    </>
  );
}

function OutpostBladeTitle(props: { name: string }): JSX.Element {
  const outposts = useQuery(() => ({ queryKey: OUTPOSTS_KEY, queryFn: () => listOutposts() }));
  const n = () => (outposts.data ?? []).find((x) => x.name === props.name);
  return <BladeTitle row={n} fallback={props.name} />;
}

// OutpostBladeBody is the outpost's detail on the shared blade stack (same chrome and
// footer action rail as the identity blades). It re-derives the outpost from the live
// query by name (not a row snapshot), so a re-enroll (which flips enrolled / stamps
// enrolled_at) reflects after the invalidate. An outpost has no editable fields, so no
// pencil; the footer carries Enroll / Re-enroll as its one prominent action. The
// Tasks panel lists the outpost's derived tasks (read-only): a task's outpost placement
// projects from its interface, so it is read here, never authored.
function OutpostBladeBody(props: { name: string; onEnrolled: (out: EnrollOutput) => void }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const edit = useBladeEdit();
  const blades = useBlades();
  const outposts = useQuery(() => ({ queryKey: OUTPOSTS_KEY, queryFn: () => listOutposts() }));
  const tasks = useQuery(() => ({ queryKey: TASKS_KEY, queryFn: () => listTasks() }));
  const interfaces = useQuery(() => ({ queryKey: ENDPOINTS_KEY, queryFn: () => listEndpoints() }));
  const n = createMemo(() => outposts.data?.find((x) => x.name === props.name) ?? null);
  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);
  const canEnroll = () => can(me.data, "outpost", "enroll");
  const canDelete = () => can(me.data, "outpost", "delete");

  // Decommission: a hard delete that cascades the outpost's interfaces, tasks, tags,
  // and enrollment credential (the server does the cascade). Confirmed, then the
  // blade closes and the list refreshes.
  async function removeOutpost() {
    const outpost = n();
    if (!outpost) return;
    if (!confirm(`Delete outpost "${outpostLabel(outpost)}"? Its endpoints, tasks, and enrollment are removed. This cannot be undone.`)) return;
    setErr(null);
    try {
      await deleteOutpost(outpost.name);
      blades.close();
      await qc.invalidateQueries({ queryKey: OUTPOSTS_KEY });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  // The editable identity fields (label, description, location), the same
  // read-edit-save shape the component/location blades use. The location options
  // are the fleet's locations (a descriptive placement, not scope). The signals
  // re-sync from the live outpost whenever it changes or edit mode toggles, so Cancel
  // reverts and a Save reflects.
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: () => listLocations() }));
  const [label, setLabel] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [location, setLocation] = createSignal("");
  createEffect(on([n, () => edit.editing()] as const, ([outpost]) => {
    setLabel(outpost?.label ?? "");
    setDescription(outpost?.description ?? "");
    setLocation(outpost?.location ?? "");
    setErr(null);
  }));

  async function save() {
    const outpost = n();
    if (!outpost) return;
    setErr(null);
    try {
      await updateOutpost(outpost.name, { label: label().trim(), description: description().trim(), location: location() });
      await qc.invalidateQueries({ queryKey: OUTPOSTS_KEY });
    } catch (e) {
      setErr(describeError(e));
      throw e; // keep the blade in edit mode on failure
    }
  }

  // The outpost's derived tasks. A task has no name: it is a binding, a function running
  // over an interface, so it reads as its interface (the anchor, resolved from the
  // surrogate endpoint_id) plus the function it runs, never a redundant label. The
  // function name arrives with device drivers, so today it reads as the built-in check
  // with a provisional marker. Read-only: a task is derived, never authored here.
  const ifaceName = (id: string) => interfaces.data?.find((i) => i.id === id)?.name ?? id;
  const outpostTasks = createMemo(() =>
    (tasks.data ?? [])
      .filter((t) => t.outpost === props.name)
      .map((t) => ({ id: t.id, iface: ifaceName(t.endpoint_id), enabled: t.enabled })),
  );

  async function doEnroll() {
    const outpost = n();
    if (!outpost || busy()) return;
    setBusy(true);
    setErr(null);
    try {
      const out = await enrollOutpost(outpost.name);
      await qc.invalidateQueries({ queryKey: OUTPOSTS_KEY });
      props.onEnrolled(out);
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  // Edit is gated on outpost:update (the name is the immutable key, so it stays
  // read-only). Enroll is the next step for an outpost that has never been enrolled, so it
  // takes the footer's primary slot; once enrolled, Re-enroll is a rarer act and
  // folds into the kebab.
  edit.bind({
    editable: () => !!n() && can(me.data, "outpost", "update"),
    save,
    destructive: () => (canDelete() && n() ? { label: "Delete", tone: "danger", onClick: () => void removeOutpost() } : undefined),
    primary: () =>
      canEnroll() && n() && !n()!.enrolled
        ? { label: "Enroll", onClick: () => void doEnroll(), busy }
        : undefined,
    secondary: () =>
      canEnroll() && n()?.enrolled ? [{ label: "Re-enroll", onClick: () => void doEnroll() }] : [],
  });

  return (
    <Show when={n()} fallback={<p class="text-sm text-base-content/50">This outpost is no longer available.</p>}>
      {(outpost) => (
        <div class="flex flex-col gap-4">
          <div class="flex items-center gap-3">
            <span class="text-base-content/40"><Server size={22} /></span>
            <StatusPill outpost={outpost()} />
          </div>

          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>

          <div class="grid grid-cols-2 gap-4">
            <KVStacked bind="name" value={<span class="font-data">{outpost().name}</span>} />
            <KVStacked label="Status" value={STATUS[outpostStatus(outpost())].label} />
            <KVStacked label="Last heartbeat" value={outpost().last_heartbeat_at ? rel(outpost().last_heartbeat_at!) : <span class="text-base-content/40">never</span>} />
            <KVStacked label="Enrolled" value={outpost().enrolled ? (outpost().enrolled_at ? rel(outpost().enrolled_at!) : "yes") : <span class="text-base-content/40">not yet</span>} />
          </div>

          <BladeField
            bind="label"
            placeholder={outpost().name}
            value={() => outpost().label ?? ""}
            draft={label}
            onInput={setLabel}
            read={
              <Show when={outpost().label} fallback={<span class="text-base-content/40">{outpost().name}</span>}>
                {outpost().label}
              </Show>
            }
          />

          <BladeField
            label="Location"
            read={
              <Show when={outpost().location} fallback={<span class="text-base-content/40">unplaced</span>}>
                {outpost().location}
              </Show>
            }
          >
            {/* The directory can answer after the blade is on screen, and a
                <select> keeps no value it has no option for, so the control takes
                its value through the shared binder (lib/selectvalue.ts, #772). */}
            <select ref={bindSelectValue(location, () => locations.data)} class="select select-bordered w-full" onChange={(e) => setLocation(e.currentTarget.value)}>
              <option value="">(unplaced)</option>
              <For each={locations.data ?? []}>{(l) => <option value={l.name}>{entityLabel(l)}</option>}</For>
            </select>
          </BladeField>

          <BladeField
            label="Description"
            multiline
            placeholder="HQ network closet"
            value={() => outpost().description ?? ""}
            draft={description}
            onInput={setDescription}
          />

          <TagAdder
            kind="outpost"
            name={outpost().name}
            canUpdate={edit.editing() && can(me.data, "outpost", "update")}
            canCreateKey={can(me.data, "tag", "create")}
          />

          <div class="flex flex-col gap-1.5">
            <div class="flex items-baseline gap-1.5">
              <span class="eyebrow">Tasks</span>
              <Show when={outpostTasks().length}><span class="text-xs text-base-content/40">{outpostTasks().length}</span></Show>
            </div>
            <Show
              when={outpostTasks().length}
              fallback={<p class="text-xs text-base-content/40">No tasks. A task derives when an interface placed on this outpost is created.</p>}
            >
              <div class="overflow-hidden rounded-box border border-base-300">
                <For each={outpostTasks()}>
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

          <LogsPanel
            name={props.name}
            title="Self-logs"
            intro="Operational log lines the outpost emitted about itself (connected to the bus, worklist pulled, a task skipped), shipped back over the telemetry lane. The outpost's own story, not a component's. Newest first, last 24 hours."
            queryKey={OUTPOST_LOGS_KEY(props.name)}
            queryFn={() => getOutpostLogs(props.name)}
          />
        </div>
      )}
    </Show>
  );
}

// CreateOutpostForm is the new-outpost form the create Drawer hosts: the label
// leads, the name (the outpost's address, required) follows it, plus an optional
// location and description. Creating an outpost does not enroll it: on success it
// invalidates the list and hands the new row to onCreated, which opens its blade,
// where Enroll is the primary action once the device is ready to claim a token.
function CreateOutpostForm(props: { close: () => void; onCreated: (row: Outpost) => void }) {
  const qc = useQueryClient();
  const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: () => listLocations() }));
  // Label leads and the address follows it, stopping the moment the
  // operator edits the address by hand (lib/entities).
  const { display, setDisplay, name, setName, nameDerived } = createIdentity();
  const [location, setLocation] = createSignal("");
  const [description, setDescription] = createSignal("");
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  useFormActions().bind({
    submitLabel: "Create outpost",
    submitIcon: Plus,
    submit: () => void submit(),
    busy,
    disabled: () => !name().trim(),
    cancel: props.close,
  });

  async function submit() {
    setBusy(true);
    setErr(null);
    try {
      const created = await createOutpost({
        name: name().trim(),
        label: display().trim() || undefined,
        description: description().trim() || undefined,
        location: location() || undefined,
      });
      await qc.invalidateQueries({ queryKey: OUTPOSTS_KEY });
      pushToast({ tone: "success", title: "Outpost created. Enroll it when the device is ready." });
      props.onCreated(created);
    } catch (er) {
      setErr(describeError(er));
    } finally {
      setBusy(false);
    }
  }

  return (
    <form class="flex flex-col gap-3" onSubmit={(e) => { e.preventDefault(); void submit(); }}>
      <p class="text-xs text-base-content/50">Registers an edge outpost. Enroll it from its detail when the device is ready to claim a token.</p>
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-outpost-display">Label</label>
        <input id="new-outpost-display" autocomplete="off" class="input input-bordered w-full" value={display()} placeholder="HQ Closet Outpost" onInput={(e) => setDisplay(e.currentTarget.value)} disabled={busy()} />
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-outpost-name">Name</label>
        <input id="new-outpost-name" autocomplete="off" class="input input-bordered w-full font-data" value={name()} placeholder="edge-hq-1" onInput={(e) => setName(e.currentTarget.value)} disabled={busy()} aria-required="true" />
        <p class="mt-1 text-xs text-base-content/50">{nameDerived() ? "Derived from the label. Edit to set your own." : "The outpost's address, used by the API and CLI."}</p>
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-outpost-location">Location</label>
        <select id="new-outpost-location" class="select select-bordered w-full" value={location()} onChange={(e) => setLocation(e.currentTarget.value)} disabled={busy()}>
          <option value="">(unplaced)</option>
          <For each={locations.data ?? []}>{(l) => <option value={l.name}>{entityLabel(l)}</option>}</For>
        </select>
      </div>
      <div>
        <label class="eyebrow mb-1.5 block" for="new-outpost-desc">Description</label>
        <input id="new-outpost-desc" autocomplete="off" class="input input-bordered w-full" value={description()} placeholder="HQ network closet" onInput={(e) => setDescription(e.currentTarget.value)} disabled={busy()} />
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
              <span>This token is shown once. Copy it now; it cannot be retrieved again. Any previously issued token for this outpost stops working.</span>
            </div>
            <div class="flex items-stretch gap-2">
              <input readonly value={props.result?.token ?? ""} aria-label="Enrollment token" class="input input-bordered w-full font-data text-xs" onFocus={(e) => e.currentTarget.select()} />
              <Button intent="action" onClick={() => props.result && copy(props.result.token)}>
                <Show when={copied()} fallback={<><Copy size={14} /> Copy</>}><Check size={14} /> Copied</Show>
              </Button>
            </div>
            <p class="text-xs text-base-content/50">Hand it to the outpost deployment; the outpost presents it to claim its NATS credential. The server stores only a hash and never logs it.</p>
            <div class="flex justify-end">
              <Button intent="quiet" onClick={props.onClose}>Done</Button>
            </div>
          </Dialog.Content>
        </div>
      </Dialog.Portal>
    </Dialog>
  );
}
