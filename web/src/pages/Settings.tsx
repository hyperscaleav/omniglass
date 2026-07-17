import { Show, For, createSignal, createEffect } from "solid-js";
import Button from "../components/Button";
import { RotateCcw, Save, ChevronDown } from "../components/icons";
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

// sourceBadge maps a provenance level to its operator-facing label and badge class.
function sourceBadge(level: string): { label: string; cls: string } {
  switch (level) {
    case "global":
      return { label: "Set in console", cls: "badge-primary" };
    case "file":
      return { label: "From settings file", cls: "badge-info" };
    default:
      return { label: "Default", cls: "badge-ghost" };
  }
}

// levelLabel names a cascade level for the expandable layer stack.
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
          <Button
            intent="danger"
            icon={RotateCcw}
            loading={restoringAll()}
            onClick={onRestoreAll}
            class="flex-none"
          >
            Restore all defaults
          </Button>
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
                    <Show when={canEdit()}>
                      <Button size="xs" icon={RotateCcw} onClick={() => onRestoreNamespace(namespace)}>
                        Restore section
                      </Button>
                    </Show>
                  </div>
                  <div class="flex flex-col divide-y divide-base-300">
                    <For each={Object.keys(data().values[namespace]).sort()}>
                      {(key) => (
                        <SettingRow
                          namespace={namespace}
                          settingKey={key}
                          value={data().values[namespace][key]}
                          source={data().sources[`${namespace}.${key}`] ?? "code"}
                          lockLevel={data().locks[`${namespace}.${key}`]}
                          canEdit={canEdit()}
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

// SettingRow is one settable key: its label, an editable control (a select for
// ui.theme, else a text input), a provenance badge, a lock chip when the key is
// locked, save and restore actions, and an expandable layer stack. It owns its own
// draft so a keystroke is not clobbered when the read query settles.
function SettingRow(props: {
  namespace: string;
  settingKey: string;
  value: unknown;
  source: string;
  lockLevel?: string;
  canEdit: boolean;
  onPatch: (namespace: string, patch: Record<string, unknown>) => Promise<{ ok: true } | { ok: false; message: string }>;
}) {
  const current = () => (props.value == null ? "" : String(props.value));
  const [draft, setDraft] = createSignal(current());
  const [expanded, setExpanded] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [rowErr, setRowErr] = createSignal<string | null>(null);

  // Re-seed the draft whenever the resolved value changes (a save or restore
  // elsewhere), unless the operator is mid-edit on this row. We track the
  // last-seeded value rather than comparing against the new current, so a
  // clean row (draft still equals what we last seeded) follows the new value
  // while a row the operator has typed into keeps its unsaved edit.
  let seeded = current();
  createEffect(() => {
    const next = current();
    if (draft() === seeded) setDraft(next);
    seeded = next;
  });
  const dirty = () => draft() !== current();
  const isThemeKey = () => props.namespace === "ui" && props.settingKey === "theme";
  const isOverridden = () => props.source === "global";
  const badge = () => sourceBadge(props.source);

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
    <div class="py-3 first:pt-0 last:pb-0">
      <div class="flex flex-wrap items-center gap-3">
        <div class="min-w-40 flex-1">
          <label class="eyebrow mb-1.5 block" for={`set-${props.namespace}-${props.settingKey}`}>
            {props.settingKey}
          </label>
          <Show
            when={isThemeKey()}
            fallback={
              <input
                id={`set-${props.namespace}-${props.settingKey}`}
                type="text"
                class="input input-bordered input-sm w-full font-data"
                value={draft()}
                disabled={!props.canEdit || Boolean(props.lockLevel)}
                onInput={(e) => setDraft(e.currentTarget.value)}
              />
            }
          >
            <select
              id={`set-${props.namespace}-${props.settingKey}`}
              class="select select-bordered select-sm w-full"
              value={draft()}
              disabled={!props.canEdit || Boolean(props.lockLevel)}
              onChange={(e) => setDraft(e.currentTarget.value)}
            >
              <For each={THEME_OPTIONS}>
                {(o) => <option value={o.value}>{o.label}</option>}
              </For>
            </select>
          </Show>
        </div>

        <div class="flex items-center gap-2 self-end pb-1">
          <span class={`badge badge-sm ${badge().cls}`}>{badge().label}</span>
          <Show when={props.lockLevel}>
            <span class="badge badge-sm badge-warning" title={`Locked at the ${levelLabel(props.lockLevel!)} level`}>
              Locked
            </span>
          </Show>
          <Show when={props.canEdit && dirty() && !props.lockLevel}>
            <Button size="xs" intent="action" icon={Save} loading={busy()} onClick={save}>
              Save
            </Button>
          </Show>
          <Show when={props.canEdit && isOverridden() && !props.lockLevel}>
            <Button size="xs" icon={RotateCcw} loading={busy()} onClick={restoreKey} title="Restore to default">
              Restore
            </Button>
          </Show>
          <Button
            size="xs"
            square
            icon={ChevronDown}
            label={expanded() ? "Hide layer stack" : "Show layer stack"}
            onClick={() => setExpanded((v) => !v)}
          />
        </div>
      </div>

      <Show when={rowErr()}>
        <p class="mt-1 text-[11px] text-error">{rowErr()}</p>
      </Show>

      <Show when={expanded()}>
        <div class="mt-2 rounded-box bg-base-100 p-3 text-xs">
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
          <Show when={props.lockLevel}>
            <p class="mt-2 text-base-content/60">
              Locked at the {levelLabel(props.lockLevel!)} level; more specific levels cannot override it.
            </p>
          </Show>
        </div>
      </Show>
    </div>
  );
}
