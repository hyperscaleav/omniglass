import { For, Show, createMemo, createSignal, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import Eyebrow from "./Eyebrow";
import FieldRow from "./FieldRow";
import InfoTip from "./InfoTip";
import SegmentedControl from "./ui/SegmentedControl";
import TemplatePreview from "./TemplatePreview";
import StartFromTemplateForm, { useHeldSlots, useHeldStandards } from "./StartFromTemplateForm";
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
  // A segmented choice, not two buttons: the form below owns the one primary
  // action, so the choice wears the tab chrome rather than an action intent.
  return (
    <div class="flex items-center gap-2" data-testid="create-mode">
      <SegmentedControl
        label="How to start"
        value={props.mode}
        onChange={(v) => props.onChange(v as CreateMode)}
        options={[
          { value: "scratch", label: "From scratch" },
          { value: "template", label: "From a template" },
        ]}
      />
      <InfoTip label="templates" text={TEMPLATE_HINT} />
    </div>
  );
}

export default function TemplateCreate(props: { kind: TemplateKind; location?: string; onDone: (href: string) => void; onCancel?: () => void }): JSX.Element {
  const templates = useQuery(() => ({ queryKey: TEMPLATES_KEY, queryFn: listTemplates }));
  // byLabel stays here (#757): the template catalog is served by kind then name
  // from the binary, not by the SQL label rule, so the order is composed here.
  const options = createMemo(() => [...ofKinds(templates.data ?? [], [props.kind])].sort(byLabel));
  const [chosen, setChosen] = createSignal("");
  const picked = () => options().find((t) => t.name === chosen());
  const { byName, standards } = useHeldStandards();
  // A system template is a system built to ONE standard, so only a standard that
  // ships one is offered; the rest are not hidden, they just have no example yet.
  const standardScope = () => {
    if (props.kind !== "system_template" || !standards.data) return null;
    const covered = new Set(options().map((t) => t.system?.standard).filter(Boolean)).size;
    const total = standards.data.length;
    return total > covered ? { covered, total } : null;
  };
  const heldSlots = useHeldSlots(byName, () => picked()?.system?.standard ?? "");
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
      <Show when={standardScope()}>
        {(sc) => (
          <p class="-mt-2 text-[11px] text-base-content/50" data-testid="template-scope">
            {sc().covered} of the {sc().total} standards ship a system template. A standard with none has no system template yet: start from scratch and choose that standard.
          </p>
        )}
      </Show>
      <Show when={picked()} keyed>
        {(t) => (
          <>
            <section class="flex flex-col gap-2 rounded-box border border-base-300 bg-base-200 p-3" data-testid="template-create-preview">
              <Eyebrow label="What it creates" hint="Read from the template itself. A system is staffed to the slots of the standard it is built to: yours of that name when you hold one." />
              <Show when={t.description}><p class="text-sm text-base-content/70">{t.description}</p></Show>
              <TemplatePreview template={t} all={templates.data ?? []} heldSlots={heldSlots()} />
            </section>
            <StartFromTemplateForm template={t} all={templates.data ?? []} location={props.location} onDone={props.onDone} onCancel={props.onCancel} />
          </>
        )}
      </Show>
    </div>
  );
}
