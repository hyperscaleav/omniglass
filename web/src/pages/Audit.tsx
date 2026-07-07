import { For, Show, createMemo, createSignal, onMount } from "solid-js";
import FilterBar from "../components/FilterBar";
import { type AuditEvent, AUDIT_PAGE, auditFilterKeys, listAuditLog, actorLabel, accountableLabel } from "../lib/audit";
import { buildPredicate, type Chip } from "../lib/predicate";
import { describeError } from "../lib/format";

// Audit: the read-only audit trail. Every privileged mutation and auth event,
// newest first, with the actor and, for an action taken while impersonating, the
// real admin behind it. Gated by audit:read (admin and owner), so a viewer never
// sees this page. It teaches the accountability model: nothing privileged happens
// without a named actor, and impersonation never hides who really acted.
//
// The page wears the console's list-view standard: the shared FilterBar faceted
// chip search (who / action / resource / id) over the loaded rows, and "load
// older" pages backward through the server `before` cursor. Filtering is
// client-side over what is loaded, so a narrow filter that comes up short is a cue
// to load older and search further back.

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

function Row(props: { e: AuditEvent }) {
  const e = props.e;
  return (
    <tr class="border-base-200">
      <td class="whitespace-nowrap tnum text-xs text-base-content/60">{when(e.ts)}</td>
      <td class="text-sm">
        {/* The accountable actor is the human who acted: the impersonator when the
            action was taken while impersonating, tagged with the identity they
            assumed ("admin as bob"), so accountability lands on the real person. */}
        <span class="font-data">{accountableLabel(e)}</span>
        <Show when={e.real_actor}>
          <span class="ml-1.5 badge badge-soft badge-warning badge-xs" title="Acted while impersonating this principal">
            as {actorLabel(e)}
          </span>
        </Show>
      </td>
      <td><span class={`badge badge-sm ${verbClass(e.verb)} font-data`}>{e.verb}</span></td>
      <td class="font-data text-xs text-base-content/70">{e.resource}</td>
      <td class="font-data text-[11px] text-base-content/40">{e.resource_id || ""}</td>
    </tr>
  );
}

export default function Audit() {
  const [rows, setRows] = createSignal<AuditEvent[]>([]);
  const [chips, setChips] = createSignal<Chip[]>([]);
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

  const shown = createMemo(() => rows().filter(buildPredicate(auditFilterKeys, chips())));
  const filtering = () => chips().length > 0;
  const oldest = () => {
    const r = rows();
    return r.length ? r[r.length - 1].ts : undefined;
  };

  // No page H1 or subtitle: like the inventory list views, the top bar labels the
  // page (see Page.tsx). The trail teaches itself through the coloured verb badges
  // and the "as" impersonation tag; the console guide carries the prose.
  return (
    <div class="og-stack flex flex-col">
      <Show when={error()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{describeError(error())}</span></div>
      </Show>

      <div class="card overflow-hidden border border-base-300 bg-base-200 p-0">
        <div class="border-b border-base-300 px-3 py-2.5">
          <FilterBar
            keys={auditFilterKeys}
            rows={rows()}
            chips={chips()}
            onChips={setChips}
            bare
            clearable
            placeholder="filter by who, action, resource, id"
          />
        </div>
        <div class="overflow-x-auto">
          <table class="table table-sm">
            <thead>
              <tr>
                <th>When</th>
                <th>Who</th>
                <th>Action</th>
                <th>Resource</th>
                <th>Id</th>
              </tr>
            </thead>
            <tbody>
              <For
                each={shown()}
                fallback={
                  <tr>
                    <td colspan="5" class="text-center text-base-content/40">
                      {loading() ? "Loading…" : filtering() ? "No events match. Load older to search further back." : "No events yet."}
                    </td>
                  </tr>
                }
              >
                {(e) => <Row e={e} />}
              </For>
            </tbody>
          </table>
        </div>
        <div class="flex items-center justify-between border-t border-base-300 px-3 py-2.5 text-xs text-base-content/50">
          <span>
            {shown().length}
            <Show when={filtering()}> of {rows().length} loaded</Show> shown
          </span>
          <button
            class="btn btn-quiet btn-sm text-xs"
            disabled={done() || loadingOlder() || loading()}
            onClick={() => load(oldest())}
          >
            {loadingOlder() ? "Loading…" : done() ? "No older events" : "Load older"}
          </button>
        </div>
      </div>
    </div>
  );
}
