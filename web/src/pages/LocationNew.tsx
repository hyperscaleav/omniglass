import { createSignal } from "solid-js";
import { Show } from "solid-js";
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
    <Page title="New location" subtitle="A root location needs an all-scoped grant; a child must sit under a location you may create in.">
      <form onSubmit={onSubmit} class="card" style={{ padding: "var(--pad-card)", display: "flex", "flex-direction": "column", gap: "16px", "max-width": "520px" }}>
        <Field label="Name" hint="Globally unique, the address (e.g. hq-b1-r204).">
          <input class="input" style={{ width: "100%" }} value={name()} onInput={(e) => setName(e.currentTarget.value)} required autofocus disabled={busy()} />
        </Field>
        <Field label="Type" hint="A location type (campus, building, floor, room, or a custom one).">
          <input class="input" style={{ width: "100%" }} list="loctypes" value={type()} onInput={(e) => setType(e.currentTarget.value)} required disabled={busy()} />
          <datalist id="loctypes">
            {OFFICIAL_TYPES.map((t) => <option value={t} />)}
          </datalist>
        </Field>
        <Field label="Display name" hint="Optional human label.">
          <input class="input" style={{ width: "100%" }} value={displayName()} onInput={(e) => setDisplayName(e.currentTarget.value)} disabled={busy()} />
        </Field>
        <Field label="Parent" hint="Optional parent location name; leave blank for a root.">
          <input class="input" style={{ width: "100%" }} value={parent()} onInput={(e) => setParent(e.currentTarget.value)} disabled={busy()} />
        </Field>
        <Show when={err()}>
          <div role="alert" class="badge" style={{ color: "var(--high)", "border-color": "color-mix(in oklch, var(--high) 45%, transparent)", background: "color-mix(in oklch, var(--high) 13%, transparent)", padding: "8px 10px" }}>{err()}</div>
        </Show>
        <div style={{ display: "flex", gap: "8px" }}>
          <button type="submit" class="btn btn-primary" disabled={busy() || !name() || !type()}>{busy() ? "Creating…" : "Create location"}</button>
          <button type="button" class="btn" onClick={() => navigate("/locations")} disabled={busy()}>Cancel</button>
        </div>
      </form>
    </Page>
  );
}

import type { JSX } from "solid-js";
function Field(props: { label: string; hint?: string; children: JSX.Element }) {
  return (
    <div>
      <label class="eyebrow" style={{ display: "block", "margin-bottom": "6px" }}>{props.label}</label>
      {props.children}
      <Show when={props.hint}><p style={{ "font-size": "11.5px", color: "var(--text-faint)", margin: "5px 2px 0" }}>{props.hint}</p></Show>
    </div>
  );
}
