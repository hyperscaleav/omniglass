import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import Eyebrow from "./Eyebrow";
import FieldRow from "./FieldRow";
import InfoTip from "./InfoTip";
import TemplatePreview from "./TemplatePreview";
import StartFromTemplateForm, { useHeldRoles, useHeldStandards } from "./StartFromTemplateForm";
import { RotateCcw } from "./icons";
import { byLabel, entityLabel } from "../lib/entities";
import { describeError } from "../lib/format";
import { useMe, can } from "../lib/auth";
import { TEMPLATES_KEY, TEMPLATE_HINT, type TemplateKind, listTemplates, ofKinds, templateSummary } from "../lib/templates";

// The create dialogs' two ways in (#1329): from scratch (the kind's own form) or
// from a template. CreateModeChoice is the choice, offered only to a caller who
// may read templates; TemplateCreate is the template face: a picker of the
// kind's templates, what the chosen one creates (TemplatePreview, the same render
// the Templates panel shows), and only the inputs it needs (StartFromTemplateForm,
// the same form the Templates page opens), which binds the host's action rail.

export type CreateMode = "scratch" | "template";

export function useTemplateChoice() {
  const me = useMe();
  const [mode, setMode] = createSignal<CreateMode>("scratch");
  const offered = () => can(me.data, "template", "read");
  return { mode: () => (offered() ? mode() : "scratch"), setMode, offered };
}

export function CreateModeChoice(props: { mode: CreateMode; onChange: (m: CreateMode) => void }): JSX.Element {
  // A segmented toggle, not two buttons: the form below owns the one primary
  // action, so the choice wears the tab chrome rather than an action intent.
  const opt = (m: CreateMode, label: string) => (
    <button
      type="button"
      class="tab"
      classList={{ "tab-active": props.mode === m }}
      aria-pressed={props.mode === m}
      onClick={() => props.onChange(m)}
    >
      {label}
    </button>
  );
  return (
    <div class="flex items-center gap-2" data-testid="create-mode">
      <div role="group" aria-label="How to start" class="tabs tabs-box tabs-sm w-fit">
        {opt("scratch", "From scratch")}
        {opt("template", "From a template")}
      </div>
      <InfoTip label="templates" text={TEMPLATE_HINT} />
    </div>
  );
}

export default function TemplateCreate(props: { kind: TemplateKind; place?: string; onDone: (href: string) => void }): JSX.Element {
  const templates = useQuery(() => ({ queryKey: TEMPLATES_KEY, queryFn: listTemplates }));
  const options = createMemo(() => [...ofKinds(templates.data ?? [], [props.kind])].sort(byLabel));
  const [chosen, setChosen] = createSignal("");
  const picked = () => options().find((t) => t.name === chosen());
  const { byName } = useHeldStandards();
  const heldRoles = useHeldRoles(byName, () => picked()?.system?.standard ?? "");
  return (
    <div class="flex flex-col gap-4" data-testid="template-create">
      <Show when={templates.error && !templates.data}>
        <div role="alert" class="alert alert-error alert-soft text-sm">
          <span class="flex-1">Could not read the templates: {describeError(templates.error)}</span>
          <Button size="xs" icon={RotateCcw} loading={templates.isFetching} onClick={() => void templates.refetch()}>Retry</Button>
        </div>
      </Show>
      <FieldRow
        label="Template"
        info="The examples this release ships for this kind. What the chosen one creates shows below, read from the template itself."
        hint={templates.data && options().length === 0 ? "This release ships no templates of this kind." : undefined}
      >
        <select class="select select-bordered w-full" value={chosen()} onChange={(e) => setChosen(e.currentTarget.value)} disabled={templates.isPending}>
          <option value="" disabled>{templates.isPending ? "Loading templates..." : "Choose a template..."}</option>
          <For each={options()}>{(t) => <option value={t.name}>{entityLabel(t)} ({templateSummary(t)})</option>}</For>
        </select>
      </FieldRow>
      <Show when={picked()} keyed>
        {(t) => (
          <>
            <section class="flex flex-col gap-2 rounded-box border border-base-300 bg-base-200 p-3" data-testid="template-create-preview">
              <Eyebrow label="What it creates" hint="Read from the template itself. A system is staffed to the roles of the standard it is built to: yours of that name when you hold one." />
              <Show when={t.description}><p class="text-sm text-base-content/70">{t.description}</p></Show>
              <TemplatePreview template={t} all={templates.data ?? []} heldRoles={heldRoles()} />
            </section>
            <StartFromTemplateForm template={t} all={templates.data ?? []} place={props.place} onDone={props.onDone} />
          </>
        )}
      </Show>
    </div>
  );
}
