import { Show, For, createSignal, createEffect } from "solid-js";
import Button from "../components/Button";
import KVRow from "../components/KVRow";
import { RotateCcw, Save, Pencil, Check } from "../components/icons";
import { useMe, can } from "../lib/auth";
import {
  useSettings,
  usePatchNamespace,
  useRestoreNamespace,
  useRestoreAllDefaults,
} from "../lib/settings";

// Settings is the Admin platform-settings surface (issue #271). It resolves the
// effective settings document from the cascade (code defaults, the operator file,
// the console override) and lets an admin edit each key, see where its value came
// from (provenance), restore a key or a namespace to defaults, and read the layer
// stack. Slice-0 acts on the global scope; the per-group and per-user levels are a
// fast-follow. The write path is an RFC 7386 merge patch: a null on a key restores
// it. Locks are shown when present (a broader level pinning a value); the write
// path to set a lock lands with the group/user levels, so the chip is read-only in
// slice-0.
//
// Each key renders through the shared KVRow primitive, so the surface reads like
// Fields, Variables, and Secrets: read mode is a slim value scan with the origin
// weighted, and an Edit toggle swaps in the inline inputs.

// originLabel maps a provenance level to the neutral origin-badge text KVRow shows
// (weight, not colour, carries the signal). The code default returns "" so KVRow
// suppresses the badge entirely.
function originLabel(level: string): string {
  switch (level) {
    case "global":
      return "Set in console";
    case "file":
      return "From settings file";
    default:
      return "";
  }
}

// levelLabel names a cascade level for the drill-in layer stack.
function levelLabel(level: string): string {
  switch (level) {
    case "global":
      return "Console override (global)";
    case "file":
      return "Operator settings file";
    default:
      return "Code default";
  }
}

// The cascade order, broad to specific, for the layer-stack display.
const CASCADE = ["code", "file", "global"] as const;

// The theme key is a select, not a free input: only the two shipped daisyUI themes
// are valid, and the SPA reads the effective value at boot.
const THEME_OPTIONS = [
  { value: "omniglass-dark", label: "Dark" },
  { value: "omniglass-light", label: "Light" },
];

export default function Settings() {
  const me = useMe();
  const settings = useSettings();
  const patchNamespace = usePatchNamespace();
  const restoreNamespace = useRestoreNamespace();
  const restoreAll = useRestoreAllDefaults();

  const canEdit = () => can(me.data, "settings", "update");

  const [editing, setEditing] = createSignal(false);
  const [restoringAll, setRestoringAll] = createSignal(false);
  const [pageErr, setPageErr] = createSignal<string | null>(null);

  async function onRestoreAll() {
    if (!window.confirm("Restore every platform setting to its default? This clears all console overrides.")) return;
    setRestoringAll(true);
    setPageErr(null);
    const r = await restoreAll();
    if (!r.ok) setPageErr(r.message);
    setRestoringAll(false);
  }

  async function onRestoreNamespace(namespace: string) {
    if (!window.confirm(`Restore all "${namespace}" settings to their defaults?`)) return;
    setPageErr(null);
    const r = await restoreNamespace(namespace);
    if (!r.ok) setPageErr(r.message);
  }

  return (
    <section class="og-stack flex flex-col gap-4">
      <div class="flex items-start justify-between gap-4">
        <p class="max-w-160 text-sm text-base-content/60">
          Platform preferences resolved down the settings cascade: code defaults, the operator
          settings file, and the console override. Each value shows where it came from; editing it
          sets a console override, and restoring drops that override so the value falls back to the
          layer below.
        </p>
        <Show when={canEdit()}>
          <div class="flex flex-none items-center gap-2">
            <Button
              intent={editing() ? "action" : "quiet"}
              icon={editing() ? Check : Pencil}
              onClick={() => setEditing((v) => !v)}
            >
              {editing() ? "Done" : "Edit"}
            </Button>
            <Button intent="danger" icon={RotateCcw} loading={restoringAll()} onClick={onRestoreAll}>
              Restore all defaults
            </Button>
          </div>
        </Show>
      </div>

      <Show when={pageErr()}>
        <div class="alert alert-error text-sm" role="alert">{pageErr()}</div>
      </Show>

      <Show when={settings.isPending}>
        <div class="flex items-center gap-2 text-sm text-base-content/60">
          <span class="loading loading-spinner loading-sm" /> Loading settings.
        </div>
      </Show>

      <Show when={settings.isError}>
        <div class="alert alert-warning text-sm" role="alert">
          You do not have access to the platform settings, or they could not be loaded.
        </div>
      </Show>

      <Show when={settings.data}>
        {(data) => (
          <For each={Object.keys(data().values).sort()}>
            {(namespace) => (
              <div class="card border border-base-300 bg-base-200">
                <div class="card-body gap-3">
                  <div class="flex items-center justify-between gap-3">
                    <h2 class="card-title font-data text-base lowercase">{namespace}</h2>
                    <Show when={canEdit() && editing()}>
                      <Button size="xs" icon={RotateCcw} onClick={() => onRestoreNamespace(namespace)}>
                        Restore section
                      </Button>
                    </Show>
                  </div>
                  <div class="overflow-hidden rounded-box border border-base-300 bg-base-100">
                    <For each={Object.keys(data().values[namespace]).sort()}>
                      {(key, i) => (
                        <SettingRow
                          first={i() === 0}
                          namespace={namespace}
                          settingKey={key}
                          value={data().values[namespace][key]}
                          source={data().sources[`${namespace}.${key}`] ?? "code"}
                          lockLevel={data().locks[`${namespace}.${key}`]}
                          editing={editing() && canEdit()}
                          onPatch={patchNamespace}
                        />
                      )}
                    </For>
                  </div>
                </div>
              </div>
            )}
          </For>
        )}
      </Show>
    </section>
  );
}

// SettingRow is one settable key rendered through KVRow: read mode shows the value
// with its provenance origin (weighted when overridden) and a drill-in to the layer
// stack; edit mode swaps in the control (a select for ui.theme, else a text input)
// with inline save and restore in the daisyUI join. A locked key stays read-only
// even in edit mode. It owns its own draft so a keystroke is not clobbered when the
// read query settles.
function SettingRow(props: {
  first: boolean;
  namespace: string;
  settingKey: string;
  value: unknown;
  source: string;
  lockLevel?: string;
  editing: boolean;
  onPatch: (namespace: string, patch: Record<string, unknown>) => Promise<{ ok: true } | { ok: false; message: string }>;
}) {
  const current = () => (props.value == null ? "" : String(props.value));
  const [draft, setDraft] = createSignal(current());
  const [expanded, setExpanded] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [rowErr, setRowErr] = createSignal<string | null>(null);

  // Re-seed the draft whenever the resolved value changes (a save or restore
  // elsewhere), unless the operator is mid-edit on this row. We track the
  // last-seeded value rather than comparing against the new current, so a clean
  // row (draft still equals what we last seeded) follows the new value while a row
  // the operator has typed into keeps its unsaved edit.
  let seeded = current();
  createEffect(() => {
    const next = current();
    if (draft() === seeded) setDraft(next);
    seeded = next;
  });

  const dirty = () => draft() !== current();
  const isThemeKey = () => props.namespace === "ui" && props.settingKey === "theme";
  const isOverridden = () => props.source === "global";
  const locked = () => Boolean(props.lockLevel);
  // A locked key is never editable, so it stays in read mode even on the edit pass.
  const rowEditing = () => props.editing && !locked();

  async function save() {
    setBusy(true);
    setRowErr(null);
    const r = await props.onPatch(props.namespace, { [props.settingKey]: draft() });
    if (!r.ok) setRowErr(r.message);
    setBusy(false);
  }

  async function restoreKey() {
    setBusy(true);
    setRowErr(null);
    // A null value in a merge patch deletes the key from the override, restoring it
    // to the layer below.
    const r = await props.onPatch(props.namespace, { [props.settingKey]: null });
    if (r.ok) setDraft(current());
    else setRowErr(r.message);
    setBusy(false);
  }

  return (
    <>
      <KVRow
        first={props.first}
        label={props.settingKey}
        labelMono
        typeBadge={locked() ? "Locked" : undefined}
        editing={rowEditing()}
        emphasize={isOverridden()}
        origin={originLabel(props.source)}
        onDrillIn={() => setExpanded((v) => !v)}
        value={current() || <span class="text-base-content/40">not set</span>}
        input={
          <Show
            when={isThemeKey()}
            fallback={
              <input
                type="text"
                class="input input-bordered input-sm join-item min-w-0 grow font-data"
                value={draft()}
                onInput={(e) => setDraft(e.currentTarget.value)}
              />
            }
          >
            <select
              class="select select-bordered select-sm join-item min-w-0 grow"
              value={draft()}
              onChange={(e) => setDraft(e.currentTarget.value)}
            >
              <For each={THEME_OPTIONS}>{(o) => <option value={o.value}>{o.label}</option>}</For>
            </select>
          </Show>
        }
        actions={
          <Show when={rowEditing()}>
            <Show when={dirty()}>
              <Button type="button" size="sm" square intent="action" icon={Save} class="join-item" loading={busy()} title="Save" label="Save" onClick={save} />
            </Show>
            <Show when={isOverridden()}>
              <Button type="button" size="sm" square icon={RotateCcw} class="join-item" loading={busy()} title="Restore to default" label="Restore to default" onClick={restoreKey} />
            </Show>
          </Show>
        }
      />

      <Show when={rowErr()}>
        <p class="border-t border-base-300 px-3 py-1 text-[11px] text-error">{rowErr()}</p>
      </Show>

      <Show when={expanded()}>
        <div class="border-t border-base-300 bg-base-200/40 px-3 py-3 text-xs">
          <p class="eyebrow mb-2">Layer stack</p>
          <ul class="flex flex-col gap-1">
            <For each={CASCADE}>
              {(level) => (
                <li class="flex items-center gap-2">
                  <span class={props.source === level ? "font-semibold text-base-content" : "text-base-content/40"}>
                    {levelLabel(level)}
                  </span>
                  <Show when={props.source === level}>
                    <span class="badge badge-xs badge-ghost">effective</span>
                  </Show>
                  <Show when={props.lockLevel === level}>
                    <span class="badge badge-xs badge-warning">lock</span>
                  </Show>
                </li>
              )}
            </For>
          </ul>
          <Show when={locked()}>
            <p class="mt-2 text-base-content/60">
              Locked at the {levelLabel(props.lockLevel!)} level; more specific levels cannot override it.
            </p>
          </Show>
        </div>
      </Show>
    </>
  );
}
