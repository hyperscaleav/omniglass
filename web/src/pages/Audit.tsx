import { For, Show, createSignal, onMount, type JSX } from "solid-js";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Button from "../components/Button";
import KVStacked from "../components/KVStacked";
import { type AuditEvent, AUDIT_PAGE, auditFilterKeys, listAuditLog, actorLabel, accountableLabel } from "../lib/audit";
import { diffFields, type DiffRow } from "../lib/auditdiff";

// Audit: the read-only audit trail, now a config over the shared FlatList (the flat
// sibling of the inventory TreeList, both wearing ListShell's chrome). Every
// privileged mutation and auth event, newest first, with the accountable actor
// and, for an impersonated action, the identity assumed. Gated by audit:read
// (admin and owner), so a viewer never sees it. Filtering is client-side over the
// loaded rows; "Load older" pages backward through the server `before` cursor.

// Colour a verb so the trail scans at a glance: destructive red, additive green,
// auth blue, the rest neutral.
const verbClass = (verb: string): string => {
  if (verb === "delete" || verb === "login_failed") return "badge-soft badge-error";
  if (verb === "create") return "badge-soft badge-success";
  if (verb === "update" || verb === "change_password" || verb === "login_denied") return "badge-soft badge-warning";
  if (verb === "reveal" || verb === "copy") return "badge-soft badge-warning";
  if (verb === "login" || verb === "logout") return "badge-soft badge-info";
  if (verb === "impersonate") return "badge-soft badge-primary";
  return "badge-ghost";
};

const when = (ts: string): string => {
  const d = new Date(ts);
  return isNaN(d.getTime()) ? ts : d.toLocaleString();
};

// The trail columns. The accountable actor is the human who acted (the impersonator
// when impersonating), tagged with the identity they assumed ("admin as bob").
const columns: FlatColumn<AuditEvent>[] = [
  { key: "when", label: "When", width: "190px", cell: (e) => <span class="whitespace-nowrap tnum text-xs text-base-content/60">{when(e.ts)}</span> },
  {
    key: "who", label: "Who", cell: (e) => (
      <span class="text-sm">
        <span class="font-data">{accountableLabel(e)}</span>
        <Show when={e.real_actor}>
          <span class="ml-1.5 badge badge-soft badge-warning badge-xs" title="Acted while impersonating this principal">as {actorLabel(e)}</span>
        </Show>
      </span>
    ),
  },
  { key: "action", label: "Action", width: "150px", cell: (e) => <span class={`badge badge-sm ${verbClass(e.verb)} font-data`}>{e.verb}</span> },
  { key: "resource", label: "Resource", width: "170px", cell: (e) => <span class="font-data text-xs text-base-content/70">{e.resource}</span> },
  { key: "id", label: "Id", cell: (e) => <span class="font-data text-[11px] text-base-content/40">{e.resource_id || ""}</span> },
];

// The state-to-style vocabulary for a diff row: an added field tints the After
// cell, a removed one the Before cell, a changed one both; unchanged rows dim.
const diffCellClass = (row: DiffRow, side: "before" | "after"): string => {
  if (row.state === "same") return "text-base-content/45";
  if (row.state === "changed") return side === "before" ? "bg-error/10 text-error" : "bg-success/10 text-success";
  if (row.state === "added") return side === "after" ? "bg-success/10 text-success" : "text-base-content/30";
  return side === "before" ? "bg-error/10 text-error" : "text-base-content/30";
};

// AuditDetail is the drawer body: who did what and when, then the field-level
// before/after diff over the row images the write recorded. An auth event (no
// images) says so instead of rendering an empty table; a create renders only
// After values and a delete only Before, so the one-sided registry writes
// (#228) still read honestly.
function AuditDetail(p: { e: AuditEvent }): JSX.Element {
  const e = p.e;
  const rows = diffFields(e.old, e.new);
  return (
    <div class="flex flex-col gap-4">
      <div class="grid grid-cols-2 gap-3 text-sm">
        <KVStacked label="When" value={<span class="tnum text-xs">{when(e.ts)}</span>} />
        <KVStacked
          label="Who"
          value={
            <span class="text-sm">
              <span class="font-data">{accountableLabel(e)}</span>
              <Show when={e.real_actor}>
                <span class="ml-1.5 badge badge-soft badge-warning badge-xs">as {actorLabel(e)}</span>
              </Show>
            </span>
          }
        />
        <KVStacked label="Action" value={<span class={`badge badge-sm ${verbClass(e.verb)} font-data`}>{e.verb}</span>} />
        <KVStacked label="Resource" value={<span class="font-data text-xs">{e.resource}{e.resource_id ? ` ${e.resource_id}` : ""}</span>} />
      </div>
      <div class="flex flex-col gap-1.5">
        <span class="eyebrow">Change</span>
        <Show
          when={rows.length}
          fallback={<span class="text-sm text-base-content/50">This event recorded no row images (auth events and credential changes carry none).</span>}
        >
          <div class="overflow-x-auto rounded-box border border-base-300">
            <table class="table table-xs">
              <thead>
                <tr>
                  <th>Field</th>
                  <th>Before</th>
                  <th>After</th>
                </tr>
              </thead>
              <tbody>
                <For each={rows}>
                  {(r) => (
                    <tr>
                      <td class="font-data text-xs">{r.key}</td>
                      <td class={`font-data text-xs ${diffCellClass(r, "before")}`}>{r.before ?? "(none)"}</td>
                      <td class={`font-data text-xs ${diffCellClass(r, "after")}`}>{r.after ?? "(none)"}</td>
                    </tr>
                  )}
                </For>
              </tbody>
            </table>
          </div>
        </Show>
      </div>
    </div>
  );
}

export default function Audit() {
  const [rows, setRows] = createSignal<AuditEvent[]>([]);
  const [error, setError] = createSignal<unknown>(null);
  const [loading, setLoading] = createSignal(false);
  const [loadingOlder, setLoadingOlder] = createSignal(false);
  const [done, setDone] = createSignal(false);

  // load fetches one page, newest first; with a `before` cursor it pages backward
  // and appends. A short page means there is nothing older left.
  async function load(before?: string) {
    const older = before !== undefined;
    older ? setLoadingOlder(true) : setLoading(true);
    setError(null);
    try {
      const page = await listAuditLog({ before, limit: AUDIT_PAGE });
      setRows((prev) => (older ? [...prev, ...page] : page));
      if (page.length < AUDIT_PAGE) setDone(true);
    } catch (e) {
      setError(e);
    } finally {
      older ? setLoadingOlder(false) : setLoading(false);
    }
  }
  onMount(() => load());
  const oldest = () => {
    const r = rows();
    return r.length ? r[r.length - 1].ts : undefined;
  };

  return (
    <FlatList<AuditEvent>
      config={{
        entity: { name: "audit", plural: "events" },
        rows,
        loading,
        error,
        filterKeys: auditFilterKeys,
        filterPlaceholder: "filter by who, action, resource, id",
        columns,
        empty: "No events yet.",
        detail: (e) => ({
          title: <span class="font-data text-sm">{e.verb} {e.resource}{e.resource_id ? ` ${e.resource_id}` : ""}</span>,
          body: <AuditDetail e={e} />,
        }),
        footer: ({ shown, total, filtering }) => (
          <>
            <span>
              {shown}
              <Show when={filtering}> of {total} loaded</Show> shown
            </span>
            <Button
              class="text-xs"
              disabled={done() || loadingOlder() || loading()}
              onClick={() => load(oldest())}
            >
              {loadingOlder() ? "Loading…" : done() ? "No older events" : "Load older"}
            </Button>
          </>
        ),
      }}
    />
  );
}
