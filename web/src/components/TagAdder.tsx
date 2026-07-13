import { For, Show, createSignal, type JSX } from "solid-js";
import { useQuery, useQueryClient } from "@tanstack/solid-query";
import { X, Plus, Check } from "./icons";
import Button from "./Button";
import { tagHue } from "../lib/tagcolor";
import {
  type EntityKind,
  TAGS_KEY,
  listTags,
  listEntityTags,
  listTagValues,
  setTag,
  removeTag,
  entityTagsKey,
  tagValuesKey,
} from "../lib/tags";
import { keySuggestions, canCoin, isEnumKey, valueOptions, valueAllowed } from "../lib/tagdraft";
import { CreateTagForm } from "../pages/Tags";
import Drawer from "./Drawer";
import { describeError } from "../lib/format";

// TagAdder is the entity detail-blade panel for applying tags: it lists the tags
// bound DIRECTLY on the entity as removable colored chips, and (with the entity's
// own :update) offers a staged key -> value add row. The key stage autocompletes
// the registry, filtered by applies_to for this entity kind and by what is already
// bound; with tag:create, a "create new" affordance opens the shared Tags create
// form in a drawer and returns with the minted key selected. Writes are immediate
// (each is the ordinary entity write), so there is no separate Save. The resolved
// cascade (inherited tags) shows in the directory Tags column, not here.
export default function TagAdder(props: { kind: EntityKind; name: string; canUpdate: boolean; canCreateKey: boolean }): JSX.Element {
  const qc = useQueryClient();
  const bindings = useQuery(() => ({ queryKey: entityTagsKey(props.kind, props.name), queryFn: () => listEntityTags(props.kind, props.name) }));
  const registry = useQuery(() => ({ queryKey: TAGS_KEY, queryFn: listTags }));

  const [keyQuery, setKeyQuery] = createSignal("");
  const [keyFocused, setKeyFocused] = createSignal(false);
  const [pendKey, setPendKey] = createSignal(""); // the chosen key, moving to the value stage
  const [pendValue, setPendValue] = createSignal("");
  const [coining, setCoining] = createSignal(false);
  const [busy, setBusy] = createSignal(false);
  const [err, setErr] = createSignal<string | null>(null);

  const [valueFocused, setValueFocused] = createSignal(false);

  const boundKeys = () => (bindings.data ?? []).map((b) => b.key);
  const suggestions = () => keySuggestions(registry.data ?? [], props.kind, boundKeys(), keyQuery());
  const coinable = () => canCoin(registry.data ?? [], keyQuery(), props.canCreateKey);
  const listKey = () => entityTagsKey(props.kind, props.name);

  // The chosen key's registry row drives the value stage: an enum key shows its
  // allowed set, a free key its distinct in-use values (fetched lazily).
  const pendTag = () => (registry.data ?? []).find((t) => t.name === pendKey());
  const distinctValues = useQuery(() => ({
    queryKey: tagValuesKey(pendKey()),
    queryFn: () => listTagValues(pendKey()),
    enabled: !!pendKey() && !!pendTag() && !isEnumKey(pendTag()!),
  }));
  const valueOpts = () => {
    const t = pendTag();
    return t ? valueOptions(t, distinctValues.data ?? [], pendValue()) : [];
  };
  const canAdd = () => {
    const t = pendTag();
    return !!t && valueAllowed(t, pendValue());
  };

  function chooseKey(name: string) {
    setPendKey(name);
    setKeyQuery("");
    setKeyFocused(false);
    setPendValue("");
  }
  function resetAdd() {
    setPendKey("");
    setPendValue("");
    setKeyQuery("");
    setErr(null);
  }

  async function commit() {
    if (!canAdd()) return;
    setBusy(true);
    setErr(null);
    try {
      await setTag(props.kind, props.name, pendKey(), pendValue().trim());
      await qc.invalidateQueries({ queryKey: listKey() });
      resetAdd();
    } catch (e) {
      setErr(describeError(e));
    } finally {
      setBusy(false);
    }
  }

  async function unbind(key: string) {
    setErr(null);
    try {
      await removeTag(props.kind, props.name, key);
      await qc.invalidateQueries({ queryKey: listKey() });
    } catch (e) {
      setErr(describeError(e));
    }
  }

  return (
    <div class="flex flex-col gap-2">
      <span class="eyebrow">Tags</span>

      <Show
        when={(bindings.data ?? []).length}
        fallback={<span class="text-sm text-base-content/40">No tags on this {props.kind}.</span>}
      >
        <div class="flex flex-wrap items-center gap-1.5">
          <For each={bindings.data}>
            {(b) => (
              <span class="badge badge-sm tag-pill gap-1" style={{ "--tag-h": String(tagHue(b.key)) }}>
                <span class="font-medium">{b.key}</span>
                <span class="opacity-40">=</span>
                <span>{b.value}</span>
                <Show when={props.canUpdate}>
                  <button type="button" class="ml-0.5 inline-flex opacity-60 hover:opacity-100" aria-label={`Remove ${b.key}`} onClick={() => unbind(b.key)}>
                    <X size={11} />
                  </button>
                </Show>
              </span>
            )}
          </For>
        </div>
      </Show>

      <Show when={err()}>
        <div role="alert" class="alert alert-error alert-soft py-1.5 text-xs"><span>{err()}</span></div>
      </Show>

      <Show when={props.canUpdate}>
        <Show
          when={pendKey()}
          fallback={
            <div class="relative">
              <input
                class="input input-bordered input-sm w-full font-data"
                placeholder="Add a tag: type a key…"
                value={keyQuery()}
                onInput={(e) => setKeyQuery(e.currentTarget.value)}
                onFocus={() => setKeyFocused(true)}
                onBlur={() => setTimeout(() => setKeyFocused(false), 150)}
              />
              <Show when={keyFocused() && (suggestions().length || coinable())}>
                <ul class="absolute z-30 mt-1 max-h-56 w-full overflow-auto rounded-box border border-base-300 bg-base-100 py-1 shadow-lg">
                  <For each={suggestions()}>
                    {(t) => (
                      <li>
                        <button type="button" class="flex w-full items-center gap-2 px-3 py-1.5 text-left text-sm hover:bg-base-content/5" onClick={() => chooseKey(t.name)}>
                          <span class="badge badge-sm tag-pill" style={{ "--tag-h": String(tagHue(t.name)) }}>{t.name}</span>
                        </button>
                      </li>
                    )}
                  </For>
                  <Show when={coinable()}>
                    <li>
                      <button type="button" class="flex w-full items-center gap-1.5 px-3 py-1.5 text-left text-sm text-primary hover:bg-base-content/5" onClick={() => setCoining(true)}>
                        <Plus size={13} /> Create key "{keyQuery().trim()}"
                      </button>
                    </li>
                  </Show>
                </ul>
              </Show>
            </div>
          }
        >
          <div class="flex items-center gap-1.5">
            <span class="badge badge-sm tag-pill flex-none" style={{ "--tag-h": String(tagHue(pendKey())) }}>{pendKey()}</span>
            <span class="text-base-content/40">=</span>
            <div class="relative min-w-0 flex-1">
              <input
                class="input input-bordered input-sm w-full font-data"
                placeholder={pendTag() && isEnumKey(pendTag()!) ? "pick a value" : "value"}
                autofocus
                value={pendValue()}
                onInput={(e) => setPendValue(e.currentTarget.value)}
                onFocus={() => setValueFocused(true)}
                onBlur={() => setTimeout(() => setValueFocused(false), 150)}
                onKeyDown={(e) => { if (e.key === "Enter") { e.preventDefault(); commit(); } else if (e.key === "Escape") resetAdd(); }}
              />
              <Show when={valueFocused() && valueOpts().length}>
                <ul class="absolute z-30 mt-1 max-h-48 w-full overflow-auto rounded-box border border-base-300 bg-base-100 py-1 shadow-lg">
                  <For each={valueOpts()}>
                    {(v) => (
                      <li>
                        <button type="button" class="flex w-full px-3 py-1.5 text-left text-sm font-data hover:bg-base-content/5" onClick={() => { setPendValue(v); setValueFocused(false); }}>
                          {v}
                        </button>
                      </li>
                    )}
                  </For>
                </ul>
              </Show>
            </div>
            <Button square intent="action" icon={Check} label="Add tag" title="Add tag" disabled={busy() || !canAdd()} onClick={commit} />
            <Button square icon={X} label="Cancel" title="Cancel" onClick={resetAdd} />
          </div>
        </Show>
      </Show>

      <Drawer open={coining()} onClose={() => setCoining(false)} title="New tag key">
        <CreateTagForm
          initialName={keyQuery().trim()}
          onCreated={(name) => { setCoining(false); qc.invalidateQueries({ queryKey: TAGS_KEY }); chooseKey(name); }}
        />
      </Drawer>
    </div>
  );
}
