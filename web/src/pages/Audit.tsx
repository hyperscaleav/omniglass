import { For, Show, createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import { type AuditEvent, AUDIT_KEY, listAuditLog, actorLabel } from "../lib/audit";
import { describeError } from "../lib/format";

// Audit: the read-only audit trail. Every privileged mutation and auth event,
// newest first, with the actor and, for an action taken while impersonating, the
// real admin behind it. Gated by audit:read (admin and owner), so a viewer never
// sees this page. It teaches the accountability model: nothing privileged happens
// without a named actor, and impersonation never hides who really acted.

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
        <span class="font-data">{actorLabel(e)}</span>
        <Show when={e.real_actor}>
          <span class="ml-1.5 badge badge-soft badge-warning badge-xs" title="Taken while impersonating">
            as {e.real_actor_name || e.real_actor}
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
  const events = useQuery(() => ({ queryKey: AUDIT_KEY, queryFn: () => listAuditLog() }));
  const rows = createMemo(() => events.data ?? []);

  return (
    <Page title="Audit">
      <p class="mb-4 text-sm text-base-content/60">
        Every privileged action and sign-in, newest first. The <span class="font-data">as</span> tag marks an
        action taken while impersonating, naming the real administrator behind it, so accountability survives
        impersonation. Read-only, and only administrators and owners can see it.
      </p>

      <Show when={events.error}>
        <div role="alert" class="alert alert-error alert-soft mb-4 text-sm"><span>{describeError(events.error)}</span></div>
      </Show>

      <div class="overflow-x-auto rounded-box border border-base-300">
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
            <For each={rows()} fallback={<tr><td colspan="5" class="text-center text-base-content/40">{events.isLoading ? "Loading…" : "No events yet."}</td></tr>}>
              {(e) => <Row e={e} />}
            </For>
          </tbody>
        </table>
      </div>
    </Page>
  );
}
