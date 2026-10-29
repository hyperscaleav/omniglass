import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { A } from "@solidjs/router";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import Button from "./Button";
import Eyebrow from "./Eyebrow";
import { ArrowRight, Check, Plus, RefreshCw, X } from "./icons";
import { COMPONENTS_KEY, listComponents } from "../lib/components";
import { SYSTEMS_KEY, listSystems } from "../lib/systems";
import { entityLabel } from "../lib/entities";
import { useMe } from "../lib/auth";
import { CLOCK_TICK_MS, createClock } from "../lib/clock";
import { LEAF_POLL_MS } from "../lib/component_leaf";
import { describeError, rel } from "../lib/format";
import {
  bindIdentity,
  identityBindingsKey,
  identityRowKey,
  listIdentityBindings,
  listUnboundIdentities,
  ownerChoices,
  refusalWords,
  unbindIdentity,
  unboundIdentitiesKey,
  type IdentityBinding,
  type IdentityBindings,
  type OwnerKind,
  type UnboundIdentities,
  type UnboundIdentity,
} from "../lib/identity_bindings";

// IdentitiesPanel is a conduit component's identities (#1389, ADR-1282): a
// component that speaks for many owners collects once, and each value it
// reports under an identity (a match key and its value) lands on the owner a
// binding names. The panel teaches that on the real data, two lists under one
// card: the identities it FANS OUT (each value and the system or component its
// samples land on), and the ones it HEARD that nothing places (their values
// land nowhere until bound), each bindable in place. A component that has
// neither renders nothing: most components speak only for themselves.
//
// An owner the caller cannot read is counted, never named (the server already
// leaves it out of the list). Binding writes onto both ends, so the bind and
// unbind controls show only when the caller may update the conduit, and the
// picker offers only owners whose own row says the caller may update them; the
// server is still the authority, and its refusal is shown in plain words
// against the row that asked.
//
// Rows iterate stable keys (match key and value), never the objects a poll
// hands back, so an open picker and its focus survive the next read.

const hasContent = (b?: IdentityBindings, u?: UnboundIdentities) =>
  (b?.bindings ?? []).length + (b?.hidden ?? 0) + (u?.identities ?? []).length > 0;

const stableKeys = { equals: (a: string[], b: string[]) => a.length === b.length && a.every((x, i) => x === b[i]) };

export default function IdentitiesPanel(props: { component: string; canUpdate: boolean }): JSX.Element {
  const qc = useQueryClient();
  const me = useMe();
  const now = createClock(CLOCK_TICK_MS);
  // Both reads poll while the panel shows (the inbox moves as the conduit
  // reports), and neither polls on a component that speaks only for itself.
  const poll = () =>
    hasContent(
      qc.getQueryData<IdentityBindings>(identityBindingsKey(props.component)),
      qc.getQueryData<UnboundIdentities>(unboundIdentitiesKey(props.component)),
    )
      ? LEAF_POLL_MS
      : false;
  const bindingsQ = useQuery(() => ({
    queryKey: identityBindingsKey(props.component),
    queryFn: () => listIdentityBindings(props.component),
    refetchInterval: poll,
  }));
  const unboundQ = useQuery(() => ({
    queryKey: unboundIdentitiesKey(props.component),
    queryFn: () => listUnboundIdentities(props.component),
    refetchInterval: poll,
  }));
  const systems = useQuery(() => ({ queryKey: SYSTEMS_KEY, queryFn: listSystems }));
  const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));

  const bindings = createMemo(() => bindingsQ.data?.bindings ?? []);
  const unbound = createMemo(() => unboundQ.data?.identities ?? []);
  const hidden = () => bindingsQ.data?.hidden ?? 0;
  const bindingKeys = createMemo(() => bindings().map((b) => identityRowKey(b.key, b.value)), undefined, stableKeys);
  const bindingByKey = createMemo(() => new Map(bindings().map((b) => [identityRowKey(b.key, b.value), b])));
  const unboundKeys = createMemo(() => unbound().map((u) => identityRowKey(u.key, u.value)), undefined, stableKeys);
  const unboundByKey = createMemo(() => new Map(unbound().map((u) => [identityRowKey(u.key, u.value), u])));

  // What an owner reads as: its label from the lists the console already
  // holds, its name until they answer.
  const ownerLabel = (b: IdentityBinding) => {
    const rows: { id: string; name: string; label?: string }[] = b.owner_kind === "system" ? (systems.data ?? []) : (components.data ?? []);
    const hit = rows.find((r) => r.id === b.owner_id);
    return hit ? entityLabel(hit) : b.owner_name;
  };
  const ownerHref = (b: IdentityBinding) => (b.owner_kind === "system" ? `/systems/${b.owner_id}` : `/components/${b.owner_id}`);
  const conduitId = () => (components.data ?? []).find((c) => c.id === props.component || c.name === props.component)?.id ?? props.component;
  const choices = createMemo(() => ownerChoices(systems.data ?? [], components.data ?? [], conduitId(), me.data));

  // One picker open at a time: the row it belongs to, the owner picked, and
  // the server's refusal when it said no.
  const [open, setOpen] = createSignal<string | null>(null);
  const [picked, setPicked] = createSignal("");
  const [refusal, setRefusal] = createSignal<string | null>(null);
  const [unbindRefusal, setUnbindRefusal] = createSignal<string | null>(null);
  const [busy, setBusy] = createSignal(false);

  const refresh = () =>
    Promise.all([
      qc.invalidateQueries({ queryKey: identityBindingsKey(props.component) }),
      qc.invalidateQueries({ queryKey: unboundIdentitiesKey(props.component) }),
    ]);

  const openPicker = (row: string) => {
    setOpen(open() === row ? null : row);
    setPicked("");
    setRefusal(null);
  };

  async function bind(u: UnboundIdentity) {
    const [kind, owner] = picked().split(":") as [OwnerKind, string];
    if (!owner) return;
    setBusy(true);
    setRefusal(null);
    try {
      await bindIdentity(props.component, u.key, u.value, kind, owner);
      setOpen(null);
      setPicked("");
      await refresh();
    } catch (e) {
      setRefusal(refusalWords(e));
    } finally {
      setBusy(false);
    }
  }

  async function unbind(b: IdentityBinding) {
    if (!window.confirm(`Unbind ${b.value} from ${ownerLabel(b)}? Its values stop landing there and wait under Heard, not bound until it is bound again.`)) return;
    setBusy(true);
    setUnbindRefusal(null);
    try {
      await unbindIdentity(props.component, b.key, b.value);
      await refresh();
    } catch (e) {
      setUnbindRefusal(`${b.value}: ${refusalWords(e)}`);
    } finally {
      setBusy(false);
    }
  }

  const failure = () => bindingsQ.error ?? unboundQ.error;
  const shown = () => !!failure() || hasContent(bindingsQ.data, unboundQ.data);

  return (
    <Show when={shown()}>
      <section data-testid="leaf-identities" class="rounded-box border border-base-300 bg-base-100 p-3.5 text-sm">
        <Eyebrow
          label="Identities"
          hint="This component speaks for many owners. Each value it reports names an identity, a match key and its value; a binding ties that identity to the system or component it is about, and its values land there instead of here. A binding writes onto both ends, so it needs update on this component and on the owner."
          right={
            <span class="text-xs tabular-nums text-base-content/55">
              {bindings().length + hidden()} bound · {unbound().length} unbound
            </span>
          }
        />
        <Show when={failure()}>
          {(err) => (
            <div role="alert" class="mt-2 flex items-center gap-2 rounded-box border border-error/40 bg-error/5 px-3 py-2 text-xs text-error">
              <span class="flex-1">Could not read its identities: {describeError(err())}</span>
              <Button size="xs" icon={RefreshCw} onClick={() => void refresh()}>Retry</Button>
            </div>
          )}
        </Show>

        <div class="mt-3 flex flex-col gap-4">
          <div class="flex flex-col gap-1.5">
            <Eyebrow label="Fans out to" hint="Each identity this component reported that has an owner. Its values land on that system or component, observed, with this component named as their source." />
            <Show when={bindings().length + hidden() > 0} fallback={<p class="text-base-content/60">Nothing bound yet.</p>}>
              <div class="overflow-x-auto rounded-field border border-base-300">
                <table data-testid="identity-bindings" class="table table-sm">
                  <thead>
                    <tr class="text-[11px] text-base-content/50">
                      <th class="w-32">Match key</th>
                      <th>Value</th>
                      <th class="w-6" aria-hidden="true" />
                      <th>Owner</th>
                      <Show when={props.canUpdate}><th class="w-10"><span class="sr-only">Actions</span></th></Show>
                    </tr>
                  </thead>
                  <tbody>
                    <For each={bindingKeys()}>
                      {(key) => (
                        <Show when={bindingByKey().get(key)}>
                          {(b) => (
                            <tr data-testid={`binding-${b().key}-${b().value}`}>
                              <td class="font-data text-xs text-base-content/60">{b().key}</td>
                              <td class="font-data text-xs">{b().value}</td>
                              <td class="text-base-content/40"><ArrowRight size={13} /></td>
                              <td>
                                <span class="inline-flex items-baseline gap-2">
                                  <A href={ownerHref(b())} class="hover:underline">{ownerLabel(b())}</A>
                                  <span class="text-[11px] text-base-content/45">{b().owner_kind}</span>
                                </span>
                              </td>
                              <Show when={props.canUpdate}>
                                <td class="text-right">
                                  <Button square size="xs" icon={X} label={`Unbind ${b().value}`} title="Unbind" disabled={busy()} onClick={() => void unbind(b())} />
                                </td>
                              </Show>
                            </tr>
                          )}
                        </Show>
                      )}
                    </For>
                    <Show when={hidden() > 0}>
                      <tr>
                        <td colSpan={props.canUpdate ? 5 : 4} class="text-xs text-base-content/55">
                          and {hidden()} more you cannot see
                        </td>
                      </tr>
                    </Show>
                  </tbody>
                </table>
              </div>
            </Show>
            <Show when={unbindRefusal()}>
              {(r) => (
                <div role="alert" class="rounded-box border border-error/40 bg-error/5 px-3 py-2 text-xs text-error">{r()}</div>
              )}
            </Show>
          </div>

          <div class="flex flex-col gap-1.5">
            <Eyebrow label="Heard, not bound" hint="Identities this component reported that no binding places. Their values land on no owner until one is bound (the raw log line still lands here), and the next value after a bind lands on its owner. Kept for seven days after last heard." />
            <Show when={unbound().length > 0} fallback={<p class="text-base-content/60">Every identity it reported has an owner.</p>}>
              <div class="overflow-x-auto rounded-field border border-base-300">
                <table data-testid="identity-unbound" class="table table-sm">
                  <thead>
                    <tr class="text-[11px] text-base-content/50">
                      <th class="w-32">Match key</th>
                      <th>Value</th>
                      <th>Heard</th>
                      <th>Keys it carried</th>
                      <Show when={props.canUpdate}><th class="w-20"><span class="sr-only">Actions</span></th></Show>
                    </tr>
                  </thead>
                  <tbody>
                    <For each={unboundKeys()}>
                      {(key) => (
                        <Show when={unboundByKey().get(key)}>
                          {(u) => (
                            <>
                              <tr data-testid={`unbound-${u().key}-${u().value}`}>
                                <td class="font-data text-xs text-base-content/60">{u().key}</td>
                                <td class="font-data text-xs">{u().value}</td>
                                <td class="text-xs text-base-content/70">
                                  <span class="tabular-nums">heard {u().count} {u().count === 1 ? "time" : "times"}</span>
                                  <span class="text-base-content/45"> · last {rel(u().last_heard_at, now())}</span>
                                </td>
                                <td>
                                  <span class="flex flex-wrap gap-1">
                                    <For each={u().keys_carried ?? []}>
                                      {(k) => <span class="badge badge-ghost badge-sm font-data">{k}</span>}
                                    </For>
                                  </span>
                                </td>
                                <Show when={props.canUpdate}>
                                  <td class="text-right">
                                    <Button
                                      size="xs"
                                      icon={Plus}
                                      label={`Bind ${u().value}`}
                                      expanded={open() === key}
                                      controls={`bind-${u().key}-${u().value}`}
                                      onClick={() => openPicker(key)}
                                    >
                                      Bind
                                    </Button>
                                  </td>
                                </Show>
                              </tr>
                              <Show when={props.canUpdate && open() === key}>
                                <tr id={`bind-${u().key}-${u().value}`} data-testid={`bind-row-${u().value}`} class="bg-base-200/60">
                                  <td colSpan={5}>
                                    <div class="flex flex-col gap-1.5">
                                      <div class="flex flex-wrap items-center gap-1.5">
                                        <span class="text-xs text-base-content/60">Its values land on</span>
                                        <select
                                          class="select select-bordered select-sm min-w-0 max-w-xs flex-1"
                                          aria-label={`Owner for ${u().value}`}
                                          onChange={(e) => setPicked(e.currentTarget.value)}
                                        >
                                          <option value="">Pick a system or component…</option>
                                          <Show when={choices().systems.length > 0}>
                                            <optgroup label="Systems">
                                              <For each={choices().systems}>{(s) => <option value={`system:${s.id}`}>{entityLabel(s)}</option>}</For>
                                            </optgroup>
                                          </Show>
                                          <Show when={choices().components.length > 0}>
                                            <optgroup label="Components">
                                              <For each={choices().components}>{(c) => <option value={`component:${c.id}`}>{entityLabel(c)}</option>}</For>
                                            </optgroup>
                                          </Show>
                                        </select>
                                        <Button
                                          size="sm"
                                          intent="action"
                                          icon={Check}
                                          label={`Bind ${u().value} to the picked owner`}
                                          loading={busy()}
                                          disabled={!picked()}
                                          onClick={() => void bind(u())}
                                        >
                                          Bind
                                        </Button>
                                        <Button square size="sm" icon={X} label="Cancel" title="Cancel" disabled={busy()} onClick={() => setOpen(null)} />
                                      </div>
                                      <Show when={refusal()}>
                                        {(r) => (
                                          <div role="alert" class="rounded-box border border-error/40 bg-error/5 px-3 py-2 text-xs text-error">{r()}</div>
                                        )}
                                      </Show>
                                    </div>
                                  </td>
                                </tr>
                              </Show>
                            </>
                          )}
                        </Show>
                      )}
                    </For>
                  </tbody>
                </table>
              </div>
            </Show>
          </div>
        </div>
      </section>
    </Show>
  );
}
