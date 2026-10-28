import { Show, createContext, createMemo, createSignal, useContext, type JSX } from "solid-js";
import { useNavigate, useSearchParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import Drawer from "../components/Drawer";
import BladeTitle from "../components/BladeTitle";
import KVStacked from "../components/KVStacked";
import Eyebrow from "../components/Eyebrow";
import InfoTip from "../components/InfoTip";
import TemplatePreview from "../components/TemplatePreview";
import StartFromTemplateForm, { useHeldRoles, useHeldStandards } from "../components/StartFromTemplateForm";
import { identityColumn } from "../components/IdentityCell";
import { Plus } from "../components/icons";
import { useMe, can } from "../lib/auth";
import { entityLabel } from "../lib/entities";
import { type Chip } from "../lib/predicate";
import { type BladeDef, createBladeController, useBladeEdit } from "../lib/blades";
import {
  type Template,
  KIND_FACET,
  KIND_WORD,
  TEMPLATES_KEY,
  TEMPLATE_HINT,
  TEMPLATE_KINDS,
  kindChip,
  listTemplates,
  missingNeeds,
  parseKinds,
  permText,
  splitKindChip,
  startNeeds,
  templateSummary,
} from "../lib/templates";

// Templates (Catalog, #1329): the catalog of examples the binary ships, one
// list across the four kinds, filtered by kind from the URL (`?kind=`, which is
// where the Catalog's Templates slots and the create flows' "Start from a
// template" land). A row opens a blade that renders what the template creates
// from its own body, and "Start from this template" (gated by the create
// permission of what it makes) opens the kind's form in a Drawer. A template is
// read-only and is never a row: what starts from one is the operator's own, and
// nothing points back (ADR-0071), which is what the rail's (i) teaches.

export { TEMPLATE_HINT };

const kindCell = (t: Template): JSX.Element => <span class="badge badge-ghost badge-sm">{KIND_WORD[t.kind]}</span>;

const columns: FlatColumn<Template>[] = [
  identityColumn<Template>(),
  { key: "kind", label: "Kind", width: "120px", sortVal: (t) => TEMPLATE_KINDS.indexOf(t.kind), cell: kindCell },
  { key: "creates", label: "Creates", width: "300px", cell: (t) => <span class="text-sm text-base-content/70">{templateSummary(t)}</span> },
];

type TemplatesUi = { openStart: (name: string) => void };
const TemplatesUiContext = createContext<TemplatesUi>();

const param = (v: string | string[] | undefined): string | undefined => (Array.isArray(v) ? v[0] : v) || undefined;

export default function Templates(): JSX.Element {
  const navigate = useNavigate();
  const [params, setParams] = useSearchParams();
  const templates = useQuery(() => ({ queryKey: TEMPLATES_KEY, queryFn: listTemplates }));
  const rows = createMemo(() => templates.data ?? []);
  const blades = createBladeController();

  // The kind facet lives in the URL; every other chip is the page's own.
  const kinds = createMemo(() => parseKinds(params.kind));
  const [rest, setRest] = createSignal<Chip[]>([]);
  const chips = () => [...kindChip(kinds()), ...rest()];
  const onChips = (next: Chip[]) => {
    const s = splitKindChip(next);
    setRest(s.rest);
    setParams({ kind: s.kinds.length ? s.kinds.join(",") : undefined }, { replace: true });
  };

  const [starting, setStarting] = createSignal<string | null>(null);
  const startRow = () => rows().find((t) => t.name === starting());

  return (
    <TemplatesUiContext.Provider value={{ openStart: (name) => { blades.close(); setStarting(name); } }}>
      <FlatList<Template>
        config={{
          entity: { name: "template", plural: "templates" },
          rows,
          loading: () => templates.isPending,
          error: () => templates.error,
          filterKeys: [
            { key: "name", type: "string", hint: "substring", get: (t) => `${entityLabel(t)} ${t.name}`, values: () => [] },
            { key: KIND_FACET, type: "string", hint: "exact", get: (t) => t.kind, values: () => [...TEMPLATE_KINDS], valueLabel: (v) => KIND_WORD[v as Template["kind"]] ?? v },
          ],
          filterPlaceholder: "filter templates by name or kind…",
          columns,
          empty: "No templates in this catalog.",
          rowId: (t) => t.name,
          openId: () => param(params.open),
          blades: { registry: { template: templateBlade }, rootKind: "template", controller: blades },
          chips,
          onChips,
          railExtra: () => (
            <span class="mr-1 inline-flex items-center gap-1.5 text-[12px] text-base-content/60" data-testid="template-teach">
              An example you clone, then own
              <InfoTip label="templates" text={TEMPLATE_HINT} />
            </span>
          ),
        }}
      />
      <Show when={starting() ? startRow() : undefined}>
        {(t) => (
          <Drawer open={true} onClose={() => setStarting(null)} title={<span>Start from {entityLabel(t())}</span>}>
            <StartFromTemplateForm template={t()} all={rows()} place={param(params.place)} onDone={(href) => { setStarting(null); navigate(href); }} />
          </Drawer>
        )}
      </Show>
    </TemplatesUiContext.Provider>
  );
}

function useTemplateRow(name: string): () => Template | undefined {
  const templates = useQuery(() => ({ queryKey: TEMPLATES_KEY, queryFn: listTemplates }));
  return () => (templates.data ?? []).find((t) => t.name === name);
}

function TemplateBladeTitle(p: { name: string }): JSX.Element {
  const row = useTemplateRow(p.name);
  return <BladeTitle row={row} fallback={p.name} />;
}

export const templateBlade: BladeDef = {
  Title: (p) => <TemplateBladeTitle name={p.id} />,
  Body: (p) => <TemplateBladeBody name={p.id} />,
};

function TemplateBladeBody(p: { name: string }): JSX.Element {
  const me = useMe();
  const ui = useContext(TemplatesUiContext);
  const templates = useQuery(() => ({ queryKey: TEMPLATES_KEY, queryFn: listTemplates }));
  const row = () => (templates.data ?? []).find((t) => t.name === p.name);
  const { held, byName } = useHeldStandards();
  // Everything the route asks of a start as the form opens (no address, the
  // template's own standard), not only the create permission it is gated by.
  const missing = createMemo(() => {
    const t = row();
    return t ? missingNeeds(startNeeds(t, { all: templates.data ?? [], held }), (r, a) => can(me.data, r, a)) : [];
  });
  const mayStart = () => !!row() && missing().length === 0;
  // A system template is staffed to the fleet's standard of that name when it holds one.
  const heldRoles = useHeldRoles(byName, () => row()?.system?.standard ?? "");

  useBladeEdit().bind({
    primary: () => (row() && mayStart() && ui ? { label: "Start from this template", icon: <Plus size={15} />, onClick: () => ui.openStart(p.name) } : undefined),
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Template not found.</p>}>
      {(t) => (
        <div class="flex flex-col gap-4">
          <Show when={t().description}>
            <p class="text-sm text-base-content/70">{t().description}</p>
          </Show>
          <div class="grid grid-cols-2 gap-3 text-sm">
            <KVStacked bind="name" value={<span class="font-data">{t().name}</span>} />
            <KVStacked label="Kind" hint={TEMPLATE_HINT} value={kindCell(t())} />
            <Show when={t().standard?.parent}>
              <KVStacked label="Variant of" mono hint="A variant is created only once you hold the standard it hangs off." value={t().standard!.parent!} />
            </Show>
            <Show when={!mayStart()}>
              <KVStacked label="To start one" hint="Starting one writes every piece as you, so it needs each permission those writes need." value={<span data-testid="start-needs" class="font-data text-xs text-base-content/60">needs {permText(missing())}</span>} />
            </Show>
          </div>
          <Eyebrow label="What it creates" hint="Read from the template itself. A system is staffed to the roles of the standard it is built to: yours of that name when you hold one, which may differ from the template's copy." />
          <TemplatePreview template={t()} all={templates.data ?? []} heldRoles={heldRoles()} />
        </div>
      )}
    </Show>
  );
}
