import { type JSX, For, Show, createEffect, onCleanup } from "solid-js";
import { Dynamic } from "solid-js/web";
import { Ban, ChevronLeft, MoreHorizontal, Pencil, RotateCcw, Save, Trash, X } from "./icons";
import { type BladeController, type BladeDef, BladeEditContext, createEditSlot } from "../lib/blades";

// BladeStack: the Azure-style right-hand blade stack, lifted out of TreeList so
// the inventory tree AND the flat identity pages share one implementation. A row
// opens an ephemeral blade; drilling pushes another offset behind it; the covered
// blade dims and clicking it returns; Back pops the top, Close clears. Blades
// carry no URL of their own. Which kinds can appear and what each renders is the
// registry's job (kind -> { Title, Body, headerExtra }); the controller owns the
// stack state (see lib/blades). The stack holds cross-entity { kind, id } refs, so
// a user blade can carry a group blade drilled over it and vice versa.
export default function BladeStack(props: {
  controller: BladeController;
  registry: Record<string, BladeDef>;
}): JSX.Element {
  const stack = () => props.controller.stack();
  const top = () => stack().length - 1;

  // Trap Tab within the top blade so focus cannot wander to the covered page.
  const trapTab = (e: KeyboardEvent, el: HTMLElement) => {
    if (e.key !== "Tab") return;
    const items = [...el.querySelectorAll<HTMLElement>('a[href],button:not([disabled]),input,select,textarea,[tabindex]:not([tabindex="-1"])')].filter((x) => x.offsetParent !== null);
    if (!items.length) return;
    const first = items[0];
    const last = items[items.length - 1];
    const active = document.activeElement;
    if (e.shiftKey && (active === first || active === el)) {
      e.preventDefault();
      last.focus();
    } else if (!e.shiftKey && active === last) {
      e.preventDefault();
      first.focus();
    }
  };

  // Escape pops the top blade.
  const onKey = (e: KeyboardEvent) => {
    if (e.key === "Escape" && stack().length) {
      e.stopPropagation();
      props.controller.pop();
    }
  };
  window.addEventListener("keydown", onKey);
  onCleanup(() => window.removeEventListener("keydown", onKey));

  // Focus management: when the stack opens, remember the focused element and move
  // focus into the top blade; when it closes, restore focus to that element.
  let priorFocus: HTMLElement | null = null;
  let wasOpen = false;
  createEffect(() => {
    const open = stack().length > 0;
    if (open && !wasOpen) priorFocus = document.activeElement as HTMLElement | null;
    else if (!open && wasOpen) {
      const el = priorFocus;
      priorFocus = null;
      queueMicrotask(() => el?.focus?.());
    }
    wasOpen = open;
    if (open) {
      queueMicrotask(() => {
        const els = document.querySelectorAll<HTMLElement>("aside[data-blade]");
        els[els.length - 1]?.focus();
      });
    }
  });

  return (
    <Show when={stack().length}>
      <div class="fixed inset-0 z-60 bg-black/45" onClick={() => props.controller.close()} />
      <For each={stack()}>
        {(ref, i) => {
          const def = () => props.registry[ref.kind];
          const isTop = () => i() === top();
          const titleId = `blade-title-${ref.kind}-${ref.id}`;
          // The blade's read/edit/save slot: the header renders the pencil (read) or
          // Save / Cancel (edit); the body reads it to switch its sections. Provided
          // via context so the header chrome and the body share one editing state.
          const edit = createEditSlot();
          return (
            <Show when={def()}>
              {(d) => (
                <BladeEditContext.Provider value={edit}>
                  <aside
                    data-blade
                    tabindex={-1}
                    role="dialog"
                    aria-modal={isTop() ? "true" : undefined}
                    aria-labelledby={titleId}
                    class="fixed inset-y-0 flex w-full max-w-md flex-col border-l border-base-300 bg-base-100 shadow-2xl outline-none"
                    style={{ right: `${(top() - i()) * 40}px`, "z-index": 61 + i() }}
                    onKeyDown={(e) => isTop() && trapTab(e, e.currentTarget)}
                  >
                    <header class="flex items-center justify-between gap-3 border-b border-base-300 px-4 py-3">
                      <div class="flex min-w-0 items-center gap-2">
                        <Show when={i()}>
                          <button class="btn btn-quiet btn-sm btn-square" title="Back" aria-label="Back" onClick={() => props.controller.pop()}>
                            <ChevronLeft size={16} />
                          </button>
                        </Show>
                        <div id={titleId} class="min-w-0 truncate text-sm font-semibold">
                          <Dynamic component={d().Title} id={ref.id} />
                        </div>
                      </div>
                      <div class="flex flex-none items-center gap-1">
                        <Show when={d().headerExtra}>
                          <Dynamic component={d().headerExtra!} id={ref.id} />
                        </Show>
                        <button class="btn btn-quiet btn-sm btn-square" title="Close" aria-label="Close" onClick={() => props.controller.close()}>
                          <X size={16} />
                        </button>
                      </div>
                    </header>
                    <div class="flex-1 overflow-auto p-5" classList={{ "pointer-events-none opacity-55": !isTop() }}>
                      <Dynamic component={d().Body} id={ref.id} />
                    </div>
                    {/* The action bar: the entity's actions, not the blade's chrome.
                        Destructive (Delete / Disable) sits left and is always available;
                        secondary actions fold into a kebab; Edit / Save / Cancel is the
                        right cluster. Rendered only when the body registers an action, so
                        a read-only blade (a role) has no bar. */}
                    <Show when={edit.editable() || !!edit.destructive() || edit.secondary().length > 0}>
                      <footer class="flex flex-none items-center gap-2 border-t border-base-300 bg-base-100 px-4 py-3" classList={{ "pointer-events-none opacity-55": !isTop() }}>
                        <Show when={edit.destructive()}>
                          {(dst) => (
                            <button
                              class="btn btn-sm gap-1.5"
                              classList={{ "btn-danger": dst().tone !== "warn" && dst().tone !== "ok", "btn-warn": dst().tone === "warn", "btn-ok": dst().tone === "ok" }}
                              onClick={() => dst().onClick()}
                            >
                              {dst().tone === "warn" ? <Ban size={14} /> : dst().tone === "ok" ? <RotateCcw size={14} /> : <Trash size={14} />}
                              {dst().label}
                            </button>
                          )}
                        </Show>
                        <div class="ml-auto flex items-center gap-2">
                          <Show when={!edit.editing() && edit.secondary().length > 0}>
                            <div class="dropdown dropdown-top dropdown-end">
                              <button type="button" tabindex={0} class="btn btn-quiet btn-sm btn-square" aria-label="More actions">
                                <MoreHorizontal size={16} />
                              </button>
                              <ul tabindex={0} class="dropdown-content menu z-50 mb-1.5 w-48 rounded-box border border-base-300 bg-base-100 p-1.5 shadow-2xl">
                                <For each={edit.secondary()}>{(s) => <li><button class="flex items-center gap-2.5" classList={{ "text-error": s.tone === "danger" }} onClick={() => s.onClick()}>{s.icon}{s.label}</button></li>}</For>
                              </ul>
                            </div>
                          </Show>
                          <Show when={edit.editable()}>
                            <Show
                              when={edit.editing()}
                              fallback={
                                <button class="btn btn-action btn-sm gap-1.5" aria-label="Edit" onClick={() => edit.begin()}>
                                  <Pencil size={15} /> Edit
                                </button>
                              }
                            >
                              <button class="btn btn-quiet btn-sm gap-1.5" onClick={() => edit.cancel()} disabled={edit.saving()}>
                                <X size={15} /> Cancel
                              </button>
                              <button class="btn btn-action btn-sm gap-1.5" onClick={() => { edit.save().catch(() => {}); }} disabled={edit.saving() || !edit.valid()}>
                                <Show when={edit.saving()} fallback={<Save size={15} />}><span class="loading loading-spinner loading-xs" /></Show>
                                Save
                              </button>
                            </Show>
                          </Show>
                        </div>
                      </footer>
                    </Show>
                    {/* Clicking a covered blade returns to it: push its own ref, which
                        truncates-to-existing and folds the stack back to this depth. */}
                    <Show when={!isTop()}>
                      <div class="absolute inset-0 cursor-pointer" onClick={() => props.controller.push(ref)} />
                    </Show>
                  </aside>
                </BladeEditContext.Provider>
              )}
            </Show>
          );
        }}
      </For>
    </Show>
  );
}
