import { Show, createSignal, onMount } from "solid-js";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Button from "../components/Button";
import { type AuditEvent, AUDIT_PAGE, auditFilterKeys, listAuditLog, actorLabel, accountableLabel } from "../lib/audit";

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
