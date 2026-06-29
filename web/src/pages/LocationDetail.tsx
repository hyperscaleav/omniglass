import { Show, Suspense, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { useNavigate, useParams } from "@solidjs/router";
import Page from "../components/Page";
import { getLocation, deleteLocation, LOCATIONS_KEY } from "../lib/locations";
import { useMe, can } from "../lib/auth";
import { ArrowRight, Trash } from "../components/icons";

// LocationDetail: the live read view for one location, with a delete action
// gated by location:delete (the server is the authority; this only hides the
// button). A 409 (occupied) surfaces as an inline alert.
export default function LocationDetail() {
  const params = useParams();
  const name = () => params.name ?? "";
  const navigate = useNavigate();
  const qc = useQueryClient();
  const me = useMe();
  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  const query = useQuery(() => ({ queryKey: [...LOCATIONS_KEY, name()], queryFn: () => getLocation(name()) }));

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
            <button class="btn btn-sm btn-outline btn-error" disabled={busy()} onClick={onDelete}><Trash size={14} /> Delete</button>
          </Show>
        </>
      }
    >
      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
      </Show>
      <Suspense fallback={<div class="card border border-base-300 bg-base-200"><div class="card-body text-base-content/50">Loading…</div></div>}>
        <Show when={!query.error} fallback={<div class="card border border-base-300 bg-base-200"><div class="card-body text-base-content/50">Not found, or outside your scope.</div></div>}>
          <Show when={query.data}>
            {(loc) => (
              <div class="card border border-base-300 bg-base-200">
                <div class="card-body grid grid-cols-2 gap-5">
                  <Fact label="Name"><span class="font-data">{loc().name}</span></Fact>
                  <Fact label="Type"><span class="capitalize">{loc().location_type}</span></Fact>
                  <Fact label="Display name">{loc().display_name || "—"}</Fact>
                  <Fact label="Parent">{loc().parent_id ? <span class="font-data">{loc().parent_id}</span> : "Root"}</Fact>
                  <Fact label="ID"><span class="font-data text-xs text-base-content/50">{loc().id}</span></Fact>
                </div>
              </div>
            )}
          </Show>
        </Show>
      </Suspense>
    </Page>
  );
}

function Fact(props: { label: string; children: JSX.Element }) {
  return (
    <div>
      <div class="eyebrow mb-1.5">{props.label}</div>
      <div class="text-sm">{props.children}</div>
    </div>
  );
}

function describeError(e: unknown): string {
  const detail = (e as { detail?: string; title?: string })?.detail ?? (e as { title?: string })?.title;
  return detail ?? "The operation failed.";
}
