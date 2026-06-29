import { createSignal, Show, For, type JSX } from "solid-js";
import { useNavigate } from "@solidjs/router";
import { useQueryClient } from "@tanstack/solid-query";
import Page from "../components/Page";
import { createLocation, LOCATIONS_KEY } from "../lib/locations";

// LocationNew: the live create form. location_type is validated by the server
// (an FK); the datalist suggests the official types but free text is allowed so
// operator-defined types work once that lands. Errors (422 unknown type, 409
// duplicate, 403 out of scope) surface inline.
const OFFICIAL_TYPES = ["campus", "building", "floor", "room"];

export default function LocationNew() {
  const navigate = useNavigate();
  const qc = useQueryClient();

  const [name, setName] = createSignal("");
  const [type, setType] = createSignal("");
  const [displayName, setDisplayName] = createSignal("");
  const [parent, setParent] = createSignal("");
  const [err, setErr] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  async function onSubmit(e: SubmitEvent) {
    e.preventDefault();
    setErr(null);
    setBusy(true);
    try {
      const created = await createLocation({
        name: name().trim(),
        location_type: type().trim(),
        display_name: displayName().trim() || undefined,
        parent: parent().trim() || undefined,
      });
      await qc.invalidateQueries({ queryKey: LOCATIONS_KEY });
      navigate(`/locations/${encodeURIComponent(created.name)}`);
    } catch (e2) {
      const detail = (e2 as { detail?: string; title?: string })?.detail ?? (e2 as { title?: string })?.title;
      setErr(detail ?? "Could not create the location.");
    } finally {
      setBusy(false);
    }
  }

  return (
    <Page title="New location">
      <form onSubmit={onSubmit} class="card max-w-xl border border-base-300 bg-base-200">
        <div class="card-body gap-4">
          <Field label="Name" hint="Globally unique, the address (e.g. hq-b1-r204).">
            <input class="input input-bordered w-full" value={name()} onInput={(e) => setName(e.currentTarget.value)} required autofocus disabled={busy()} />
          </Field>
          <Field label="Type" hint="A location type (campus, building, floor, room, or a custom one).">
            <input class="input input-bordered w-full" list="loctypes" value={type()} onInput={(e) => setType(e.currentTarget.value)} required disabled={busy()} />
            <datalist id="loctypes"><For each={OFFICIAL_TYPES}>{(t) => <option value={t} />}</For></datalist>
          </Field>
          <Field label="Display name" hint="Optional human label.">
            <input class="input input-bordered w-full" value={displayName()} onInput={(e) => setDisplayName(e.currentTarget.value)} disabled={busy()} />
          </Field>
          <Field label="Parent" hint="Optional parent location name; leave blank for a root.">
            <input class="input input-bordered w-full" value={parent()} onInput={(e) => setParent(e.currentTarget.value)} disabled={busy()} />
          </Field>
          <Show when={err()}>
            <div role="alert" class="alert alert-error alert-soft text-sm"><span>{err()}</span></div>
          </Show>
          <div class="flex gap-2">
            <button type="submit" class="btn btn-primary" disabled={busy() || !name() || !type()}>{busy() ? "Creating…" : "Create location"}</button>
            <button type="button" class="btn btn-ghost" onClick={() => navigate("/locations")} disabled={busy()}>Cancel</button>
          </div>
        </div>
      </form>
    </Page>
  );
}

function Field(props: { label: string; hint?: string; children: JSX.Element }) {
  return (
    <div>
      <label class="eyebrow mb-1.5 block">{props.label}</label>
      {props.children}
      <Show when={props.hint}><p class="mt-1.5 px-0.5 text-[11px] text-base-content/40">{props.hint}</p></Show>
    </div>
  );
}
