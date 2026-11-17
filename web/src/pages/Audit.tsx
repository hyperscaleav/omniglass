import { For, Show, createSignal, onMount, type JSX } from "solid-js";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Button from "../components/Button";
import KVStacked from "../components/KVStacked";
import { type AuditEvent, AUDIT_PAGE, AUDIT_LOADING_ROWS, auditFilterKeys, listAuditLog, actorLabel, accountableLabel, resourceLabel, channelOf } from "../lib/audit";
import { diffFields, type DiffRow } from "../lib/auditdiff";

// Audit: the read-only audit trail, now a config over the shared FlatList (the flat
// sibling of the inventory TreeList, both wearing ListShell's chrome). Every
// privileged mutation and auth event, newest first, with the accountable actor
// and, for an impersonated action, the identity assumed. Gated by audit:read
// (admin and owner), so a viewer never sees it. Filtering is client-side over the
// loaded rows; "Load older" pages backward through the server `before` cursor.

// Colour a verb so the trail scans at a glance: destructive red, additive
// green, mutating amber, auth blue. Every arm returns the same badge-soft
// treatment, the fallback included, so an unmapped verb reads as one more
// pill in a neutral tone rather than a second visual style (set and enroll
// used to render as outlined ghost boxes beside the tinted pills).
const VERB_FAMILY: Record<string, string> = {
  delete: "badge-error", purge: "badge-error", revoke_session: "badge-error", login_failed: "badge-error", login_locked: "badge-error",
  create: "badge-success", enroll: "badge-success", issue: "badge-success", restore: "badge-success", enable: "badge-success",
  update: "badge-warning", set: "badge-warning", unset: "badge-warning", archive: "badge-warning", disable: "badge-warning",
  change_password: "badge-warning", reset_password: "badge-warning", clear_avatar: "badge-warning",
  login_denied: "badge-warning", reveal: "badge-warning", copy: "badge-warning", agent_refused: "badge-warning",
  login: "badge-info", logout: "badge-info",
  impersonate: "badge-primary",
};
const verbClass = (verb: string): string => `badge-soft ${VERB_FAMILY[verb] ?? "badge-ghost"}`;

// A channel pill names how a row arrived, in the same soft treatment as the
// verbs. A direct write (no channel) takes no pill: the column marks only the
// rows that came through something other than a plain API call.
const CHANNEL_FAMILY: Record<string, string> = { mcp: "badge-secondary", omnibot: "badge-accent" };
const channelClass = (channel: string): string => `badge-soft ${CHANNEL_FAMILY[channel] ?? "badge-ghost"}`;

// What a channel proves and what it does not, taught where it is read.
const CHANNEL_HINT =
  "How the call reached the server: mcp is a tool call on the MCP server, direct a plain API call. " +
  "The server stamps it, so no caller can claim one. It says how the call arrived, not what held the token: " +
  "an agent token names its agent, but a script holding a person's token still reads as direct.";

// The agent tag rides beside the person it acted for, the way "as bob" rides
// beside an impersonator: the person is accountable, the agent is who acted.
const AgentTag = (p: { agent?: string }) => (
  <Show when={p.agent}>
    <span class="badge badge-soft badge-secondary badge-xs whitespace-nowrap" title="The agent that acted on this person's behalf, from its agent token">agent {p.agent}</span>
  </Show>
);

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
      <span class="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-sm">
        <span class="font-data">{accountableLabel(e)}</span>
        <Show when={e.real_actor}>
          <span class="badge badge-soft badge-warning badge-xs whitespace-nowrap" title="Acted while impersonating this principal">as {actorLabel(e)}</span>
        </Show>
        <AgentTag agent={e.agent} />
      </span>
    ),
  },
  {
    key: "channel", label: "Channel", width: "170px", cell: (e) => (
      <Show when={e.channel}>
        <span class="flex flex-col items-start gap-0.5">
          <span data-testid="audit-channel" class={`badge badge-sm ${channelClass(e.channel!)} font-data`}>{e.channel}</span>
          <Show when={e.client}>
            <span class="font-data text-[11px] text-base-content/50 truncate self-stretch" title={e.client}>{e.client}</span>
          </Show>
        </span>
      </Show>
    ),
  },
  { key: "action", label: "Action", width: "120px", cell: (e) => <span class={`badge badge-sm ${verbClass(e.verb)} font-data`}>{e.verb}</span> },
  { key: "resource", label: "Resource", width: "170px", cell: (e) => <span class="font-data text-xs text-base-content/70">{e.resource}</span> },
  { key: "id", label: "Id", cell: (e) => <span class="font-data text-[11px] text-base-content/40" title={e.resource_id && e.resource_id !== resourceLabel(e) ? e.resource_id : undefined}>{resourceLabel(e)}</span> },
];

// The state-to-style vocabulary for a diff row: an added field tints the After
// cell, a removed one the Before cell, a changed one both; unchanged rows dim.
const diffCellClass = (row: DiffRow, side: "before" | "after"): string => {
  if (row.state === "same") return "text-base-content/45";
  if (row.state === "changed") return side === "before" ? "bg-error/10 text-error" : "bg-success/10 text-success";
  if (row.state === "added") return side === "after" ? "bg-success/10 text-success" : "text-base-content/30";
  return side === "before" ? "bg-error/10 text-error" : "text-base-content/30";
};

// A value cell keeps a JSON value's newlines and breaks a long unbroken one
// (a base64 blob, a fingerprint) inside its column.
const VALUE_CELL = "font-data text-xs align-top whitespace-pre-wrap wrap-break-word";

// AuditDetail is the drawer body: who did what and when, then the field-level
// before/after diff over the row images the write recorded. An auth event (no
// images) says so instead of rendering an empty table; a create renders only
// its After column and a delete only its Before, so the one-sided catalog
// writes (#228) still read honestly.
function AuditDetail(p: { e: AuditEvent }): JSX.Element {
  const e = p.e;
  const rows = diffFields(e.old, e.new);
  // A side with no image at all (a create has no Before, a delete no After)
  // takes no column: a column of "(none)" would hold half the drawer while the
  // side that carries the change wraps tight.
  const hasBefore = e.old !== undefined && e.old !== null;
  const hasAfter = e.new !== undefined && e.new !== null;
  return (
    <div class="flex flex-col gap-4">
      <div class="grid grid-cols-2 gap-3 text-sm">
        <KVStacked label="When" value={<span class="tnum text-xs">{when(e.ts)}</span>} />
        <KVStacked
          label="Who"
          value={
            <span class="flex flex-wrap items-center gap-x-1.5 gap-y-0.5 text-sm">
              <span class="font-data">{accountableLabel(e)}</span>
              <Show when={e.real_actor}>
                <span class="badge badge-soft badge-warning badge-xs whitespace-nowrap">as {actorLabel(e)}</span>
              </Show>
              <AgentTag agent={e.agent} />
            </span>
          }
        />
        <KVStacked label="Action" value={<span class={`badge badge-sm ${verbClass(e.verb)} font-data`}>{e.verb}</span>} />
        <KVStacked label="Resource" value={<span class="font-data text-xs" title={e.resource_id || undefined}>{e.resource}{resourceLabel(e) ? ` ${resourceLabel(e)}` : ""}</span>} />
        <KVStacked
          label="Channel"
          hint={CHANNEL_HINT}
          value={
            <Show when={e.channel} fallback={<span class="font-data text-xs text-base-content/60">{channelOf(e)}</span>}>
              <span class={`badge badge-sm ${channelClass(e.channel!)} font-data`}>{e.channel}</span>
            </Show>
          }
        />
        <Show when={e.client}>
          <KVStacked
            label="Client"
            value={
              <span class="flex flex-col">
                <span class="font-data text-xs wrap-break-word">{e.client}</span>
                <span class="text-xs text-base-content/50">as the agent reported it</span>
              </span>
            }
          />
        </Show>
      </div>
      <div class="flex flex-col gap-1.5">
        <span class="eyebrow">Change</span>
        <Show
          when={rows.length}
          fallback={<span class="text-sm text-base-content/50">This event recorded no row images (auth events and credential changes carry none).</span>}
        >
          {/* A fixed layout with declared widths: Field takes a label's width
              and Before and After split the rest, each value wrapping in its
              own cell with its newlines kept, so a long one (an endpoint's
              params, a key fingerprint) grows the row downward and never pushes
              After off the drawer (#1088). */}
          <div class="rounded-box border border-base-300">
            <table class="table table-xs table-fixed w-full">
              <colgroup>
                <col class="w-28" />
                <Show when={hasBefore}>
                  <col />
                </Show>
                <Show when={hasAfter}>
                  <col />
                </Show>
              </colgroup>
              <thead>
                <tr>
                  <th>Field</th>
                  <Show when={hasBefore}>
                    <th>Before</th>
                  </Show>
                  <Show when={hasAfter}>
                    <th>After</th>
                  </Show>
                </tr>
              </thead>
              <tbody>
                <For each={rows}>
                  {(r) => (
                    <tr>
                      <td class="font-data text-xs align-top wrap-break-word">{r.key}</td>
                      <Show when={hasBefore}>
                        <td class={`${VALUE_CELL} ${diffCellClass(r, "before")}`}>{r.before ?? "(none)"}</td>
                      </Show>
                      <Show when={hasAfter}>
                        <td class={`${VALUE_CELL} ${diffCellClass(r, "after")}`}>{r.after ?? "(none)"}</td>
                      </Show>
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
  // Starts true: the first read is issued on mount, so the first paint is already the loading state.
  const [loading, setLoading] = createSignal(true);
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

  // The document scrolls, so the list fills a box of definite height: the top
  // bar, the main padding and room for the lab banner come off the viewport,
  // and the rows scroll inside it (#1461).
  return (
    <div class="h-[calc(100dvh-9rem)] min-h-96">
      <FlatList<AuditEvent>
        config={{
          entity: { name: "audit", plural: "events" },
          rows,
          loading,
          loadingRows: AUDIT_LOADING_ROWS,
          // A page of 100 is taller than the skeleton: scrolling the rows inside
          // the card keeps the footer where the first paint put it (#1461).
          fill: true,
          error,
          filterKeys: auditFilterKeys,
          filterPlaceholder: "filter by who, action, resource, id, channel, client",
          columns,
          empty: "No events yet.",
          detail: (e) => ({
            title: <span class="font-data text-sm" title={e.resource_id || undefined}>{e.verb} {e.resource}{resourceLabel(e) ? ` ${resourceLabel(e)}` : ""}</span>,
            body: <AuditDetail e={e} />,
            wide: true,
          }),
          footer: ({ shown, total, filtering }) => (
            <>
              <span>
                {shown}
                <Show when={filtering}> of {total} loaded</Show> shown
              </span>
              <Button
                class="min-w-28 text-xs"
                disabled={done() || loadingOlder() || loading()}
                onClick={() => load(oldest())}
              >
                {loadingOlder() ? "Loading…" : done() ? "No older events" : "Load older"}
              </Button>
            </>
          ),
        }}
      />
    </div>
  );
}
