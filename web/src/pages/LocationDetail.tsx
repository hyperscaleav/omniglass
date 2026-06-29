import { Show, Suspense, createSignal } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams } from "@solidjs/router";
import Page from "../components/Page";
import { getLocation, deleteLocation, LOCATIONS_KEY } from "../lib/locations";
import { useMe, can } from "../lib/auth";
import { ArrowRight, Trash } from "../components/icons";

// LocationDetail: the live read view for one location, with a delete action
// gated by location:delete (the server is the authority; this only hides the
// button). A 409 (occupied) surfaces as an inline message.
export default function LocationDetail() {
  const params = useParams();
  const name = () => params.name ?? "";
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();
  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  const query = useQuery(() => ({
    queryKey: [...LOCATIONS_KEY, name()],
    queryFn: () => getLocation(name()),
  }));

  async function onDelete() {
    if (!confirm(`Delete location "${name()}"?`)) return;
    setErr(null);
    setBusy(true);
    try {
      await deleteLocation(name());
      await qc.invalidateQueries({ queryKey: LOCATIONS_KEY });
      navigate("/locations");
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  return (
    <Page
      title={name()}
      actions={
        <>
          <button class="btn btn-sm" onClick={() => navigate("/locations")}>All locations <ArrowRight size={14} /></button>
          <Show when={can(me.data, "location", "delete")}>
            <button class="btn btn-sm" style={{ color: "var(--high)" }} disabled={busy()} onClick={onDelete}><Trash size={14} /> Delete</button>
          </Show>
        </>
      }
    >
      <Show when={err()}>
        <div role="alert" class="badge" style={{ color: "var(--high)", "border-color": "color-mix(in oklch, var(--high) 45%, transparent)", background: "color-mix(in oklch, var(--high) 13%, transparent)", padding: "8px 10px" }}>{err()}</div>
      </Show>
      <Suspense fallback={<div class="card" style={{ padding: "var(--pad-card)", color: "var(--text-dim)" }}>Loading…</div>}>
        <Show when={!query.error} fallback={<div class="card" style={{ padding: "var(--pad-card)", color: "var(--text-dim)" }}>Not found, or outside your scope.</div>}>
          <Show when={query.data}>
            {(loc) => (
              <div class="card" style={{ padding: "var(--pad-card)", display: "grid", "grid-template-columns": "1fr 1fr", gap: "20px" }}>
                <Fact label="Name"><span class="mono">{loc().name}</span></Fact>
                <Fact label="Type"><span style={{ "text-transform": "capitalize" }}>{loc().location_type}</span></Fact>
                <Fact label="Display name">{loc().display_name || "—"}</Fact>
                <Fact label="Parent">{loc().parent_id ? <span class="mono">{loc().parent_id}</span> : "Root"}</Fact>
                <Fact label="ID"><span class="mono" style={{ "font-size": "12px", color: "var(--text-dim)" }}>{loc().id}</span></Fact>
              </div>
            )}
          </Show>
        </Show>
      </Suspense>
    </Page>
  );
}

import type { JSX } from "solid-js";
function Fact(props: { label: string; children: JSX.Element }) {
  return (
    <div>
      <div class="eyebrow" style={{ "margin-bottom": "5px" }}>{props.label}</div>
      <div style={{ "font-size": "13.5px" }}>{props.children}</div>
    </div>
  );
}

function describeError(e: unknown): string {
  const detail = (e as { detail?: string; title?: string })?.detail ?? (e as { title?: string })?.title;
  return detail ?? "The operation failed.";
}
