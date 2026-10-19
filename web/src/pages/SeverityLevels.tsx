import { Show, type JSX } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import FlatList, { type FlatColumn } from "../components/FlatList";
import BladeTitle from "../components/BladeTitle";
import KVStacked from "../components/KVStacked";
import InfoTip from "../components/InfoTip";
import SeverityBadge from "../components/SeverityBadge";
import { identityColumn } from "../components/IdentityCell";
import { entityLabel } from "../lib/entities";
import { type SeverityLevel, listSeverityLevels, severityLevelsKey } from "../lib/severity";
import { type BladeDef, useBladeEdit } from "../lib/blades";
import { OFFICIAL_LOCK } from "../lib/catalog";

// Severity levels (Catalog > Actions > Severity, #921): the ranked registry an
// event rule raises at (#917), beside the rules. Each row is a level's rank, its
// label in its own color (the chip every alarm at it wears), whether an active
// alarm at it takes its component out, and its origin, worst first, the order an
// operator triages in. A read view: an operator level is authored from the CLI
// and API today (#930), so the blade's Edit / Delete stay greyed for everyone.

export const SEVERITY_HINTS = {
  levels:
    "How bad an alarm is. Every alarm, hand-raised or raised by a rule, names one of these levels; the console colors and sorts by it.",
  rank:
    "Higher is worse. The shipped levels are spaced ten apart, so a level of your own slots between two without renumbering.",
  outage:
    "An active alarm at an outage level takes its component out (it stops filling its roles); at any other level it degrades the component and leaves it in place.",
} as const;

// What an active alarm at the level does to its component, in one word. Only
// the outage wears a hue: a warning-toned "degrades" beside every lesser level
// competed with the level chips themselves.
function effectChip(l: SeverityLevel): JSX.Element {
  return l.outage
    ? <span class="badge badge-soft badge-error badge-sm">outage</span>
    : <span class="text-[12px] text-base-content/60">degrades</span>;
}

function originBadge(official: boolean): JSX.Element {
  return official
    ? <span class="badge badge-ghost badge-sm">official</span>
    : <span class="badge badge-outline badge-sm">custom</span>;
}

const columns: FlatColumn<SeverityLevel>[] = [
  identityColumn<SeverityLevel>(),
  { key: "rank", label: "Rank", width: "72px", sortVal: (l) => l.rank, cell: (l) => <span class="font-data tabular-nums">{l.rank}</span> },
  { key: "looks", label: "Looks like", width: "110px", cell: (l) => <SeverityBadge name={l.name} /> },
  { key: "outage", label: "Effect", width: "96px", sortVal: (l) => String(l.outage), cell: effectChip },
  { key: "official", label: "Origin", width: "84px", sortVal: (l) => String(l.official), cell: (l) => originBadge(l.official) },
];

export default function SeverityLevels(): JSX.Element {
  const levels = useQuery(() => ({ queryKey: severityLevelsKey, queryFn: listSeverityLevels }));
  // Worst first.
  const rows = () => [...(levels.data ?? [])].sort((a, b) => b.rank - a.rank);

  return (
    <FlatList<SeverityLevel>
      config={{
        entity: { name: "severity level", plural: "severity levels" },
        rows,
        loading: () => levels.isPending,
        error: () => levels.error,
        filterKeys: [
          { key: "name", type: "string", hint: "substring", get: (l) => `${entityLabel(l)} ${l.name}`, values: () => [] },
          { key: "effect", type: "string", hint: "exact", get: (l) => (l.outage ? "outage" : "degrades"), values: () => ["outage", "degrades"] },
          { key: "official", type: "string", hint: "exact", get: (l) => (l.official ? "official" : "custom"), values: () => ["official", "custom"] },
        ],
        filterPlaceholder: "filter severity levels by name…",
        columns,
        empty: "No severity levels.",
        rowId: (l) => l.name,
        blades: { registry: { severity_level: severityLevelBlade }, rootKind: "severity_level" },
        railExtra: () => (
          <span class="mr-1 inline-flex items-center gap-1.5 text-[12px] text-base-content/50">
            What a level does
            <InfoTip label="severity levels" text={SEVERITY_HINTS.levels} />
          </span>
        ),
        // No create here: levels are authored from the CLI and API (#930).
      }}
    />
  );
}

function useLevelRow(name: string): () => SeverityLevel | undefined {
  const levels = useQuery(() => ({ queryKey: severityLevelsKey, queryFn: listSeverityLevels }));
  return () => (levels.data ?? []).find((l) => l.name === name);
}

export const severityLevelBlade: BladeDef = {
  Title: (p) => <BladeTitle row={useLevelRow(p.id)} fallback={p.id} />,
  Body: (p) => <SeverityLevelBladeBody name={p.id} />,
};

// The lock an operator level carries until the console authors levels (#930).
export const LEVEL_AUTHORING_LOCK = "Author and edit operator levels with omniglass severity-level, or the API.";

function SeverityLevelBladeBody(p: { name: string }): JSX.Element {
  const row = useLevelRow(p.name);
  useBladeEdit().bind({
    locked: () => {
      const l = row();
      if (!l) return null;
      return l.official ? OFFICIAL_LOCK : LEVEL_AUTHORING_LOCK;
    },
  });

  return (
    <Show when={row()} fallback={<p class="text-sm text-base-content/50">Severity level not found.</p>}>
      {(l) => (
        <div class="grid grid-cols-2 gap-3 text-sm">
          <KVStacked bind="name" value={<span class="font-data">{l().name}</span>} />
          <KVStacked label="Origin" value={originBadge(l().official)} />
          <KVStacked
            label="Rank"
            value={
              <span class="inline-flex items-center gap-1.5">
                <span class="font-data tabular-nums">{l().rank}</span>
                <InfoTip label="rank" text={SEVERITY_HINTS.rank} />
              </span>
            }
          />
          <KVStacked label="Looks like" value={<SeverityBadge name={l().name} />} />
          <KVStacked
            label="Effect"
            value={
              <span class="inline-flex items-center gap-1.5">
                {effectChip(l())}
                <InfoTip label="effect" text={SEVERITY_HINTS.outage} />
              </span>
            }
          />
          <KVStacked label="Color" value={<span class="font-data">{l().color}</span>} />
        </div>
      )}
    </Show>
  );
}
