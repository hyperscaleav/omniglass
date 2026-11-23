import { For, Show, createSignal, type JSX } from "solid-js";
import { A, useNavigate, useSearchParams } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Eyebrow from "../components/Eyebrow";
import TimeseriesChart from "../components/TimeseriesChart";
import Page from "../components/Page";
import HealthBadge from "../components/HealthBadge";
import InfoTip from "../components/InfoTip";
import KeyedFor from "../components/KeyedFor";
import PipelineFlow from "../components/PipelineFlow";
import PlatformSection from "../components/PlatformSection";
import PlatformSimulation from "./PlatformSimulation";
import { ArrowUpRight, TriangleAlert } from "../components/icons";
import { fmtTime } from "../lib/format";
import { PLATFORM_HEALTH_NAME } from "../lib/nav";
import { LEAF_POLL_MS } from "../lib/component_leaf";
import { entityLabel } from "../lib/entities";
import { healthOf } from "../lib/health";
import type { OutpostStatus } from "../lib/outposts";
import { drawerHref } from "../lib/drawer_address";
import { createViewBinding } from "../lib/binding";
import { OPEN_PARAM, isSectionOpen, outpostsSummary, pipelineSummary, serverSummary, setSectionOpen } from "../lib/platform_sections";
import {
  NOT_REPORTED,
  PLATFORM_VIEW_KEY,
  figureText,
  figureTinted,
  lossTinted,
  outpostLossHint,
  outpostLossText,
  outpostPill,
  PANEL_SERIES_HOURS,
  PANEL_SERIES_RESOLUTION_S,
  panelAlarms,
  panelRowText,
  panelSections,
  panelBinding,
  panelRowBuckets,
  panelSeriesFold,
  panelSeriesInit,
  platformColumns,
  platformRefusal,
  platformView,
  retryPlatformView,
  serverUptime,
  type PanelRow,
  type PanelSection,
  type PanelSeries,
  type PlatformOutpost,
  type PlatformPanel,
  type PlatformPanelKey,
  type PlatformServer,
  type PlatformView,
} from "../lib/platformhealth";

// Platform health (#1644): whether Omniglass itself is well. Every figure here
// is a sample on a platform component the fleet already has (each outpost and each
// server is a component of the platform system), so the page is a reading of
// the same model an operator monitors their own fleet with, turned on the
// monitor. That is the teaching point: a row opens onto the outpost's platform
// component, the ordinary component detail, where each figure has its history.
//
// Each server gets a panel set (#1645): Requests, Ingest, Queues and Retention,
// the groups the view returns for it, the first set open. A panel row is a key's
// latest figure beside its last hour as a sparkline; a ranked key (the slowest
// and the busiest routes) is the server's own cut of its newest window, so the
// page sorts nothing. A panel is flagged only by an open alarm the view
// carries on one of its keys, never by a threshold of the page's.
//
// The page holds no key list and no thresholds. The columns are the groups the
// view returns, each headed with its catalog description, and a row's tone is
// its component's recorded health, which the seeded rules decide. One clock:
// the pill and the uptime are both judged at the view's read_at.

const NEUTRAL_PILL = "bg-base-content/10 text-base-content/70 border-transparent";
const PILL: Record<OutpostStatus, string> = {
  up: "badge-soft badge-success",
  down: "badge-soft badge-error",
  never: NEUTRAL_PILL,
};

// A row's tone is its health: the failure hues only, so a healthy fleet reads
// calm and the rows that need a look stand out.
const ROW_TONE: Record<string, string> = {
  outage: "bg-error/5",
  degraded: "bg-warning/5",
};

// The sections fold to one line each (#2446) and the open ones are held in the
// URL (?open=pipeline,server-lab-server,outposts,simulation), so a reload, a back
// step and a shared link keep what the operator was reading.
export default function PlatformHealth() {
  const view = useQuery(() => ({ queryKey: PLATFORM_VIEW_KEY, queryFn: platformView, retry: retryPlatformView, refetchInterval: LEAF_POLL_MS }));
  const [params, setParams] = useSearchParams();
  const openRaw = () => params[OPEN_PARAM];
  const isOpen = (id: string) => isSectionOpen(openRaw(), id);
  const setOpen = (id: string, open: boolean) => setParams({ [OPEN_PARAM]: setSectionOpen(openRaw(), id, open) }, { replace: true });

  return (
    <Page title={PLATFORM_HEALTH_NAME}>
      <Show when={view.error}>
        <div
          role="alert"
          class="alert alert-soft text-sm"
          classList={{ "alert-warning": (view.error as { status?: number }).status === 403, "alert-error": (view.error as { status?: number }).status !== 403 }}
        >
          {platformRefusal(view.error)}
        </div>
      </Show>
      <Show when={view.data} fallback={<Show when={!view.error}><div class="text-sm text-base-content/50">Loading the platform…</div></Show>}>
        {(v) => (
          <>
            <Header view={v()} />
            <Show when={!v().figures_read}>
              <div role="note" class="alert text-sm" data-testid="platform-reach">
                Platform figures need a fleet-wide read: your component read does not reach the platform components, so each outpost shows only its check-in.
              </div>
            </Show>
            <Show when={v().pipeline}>
              {(p) => (
                <PlatformSection testid="section-pipeline" label="Where work is lost" summary={pipelineSummary(p())} open={isOpen("pipeline")} onOpenChange={(o) => setOpen("pipeline", o)}>
                  <PipelineFlow pipeline={p()} bare />
                </PlatformSection>
              )}
            </Show>
            <KeyedFor each={v().servers} by={(s) => s.component}>
              {(s) => <ServerPanels server={s()} view={v()} open={isOpen(`server-${s().name}`)} onOpenChange={(o) => setOpen(`server-${s().name}`, o)} />}
            </KeyedFor>
            <PlatformSection testid="section-outposts" label="Outposts" summary={outpostsSummary(v())} open={isOpen("outposts")} onOpenChange={(o) => setOpen("outposts", o)}>
              <OutpostTable view={v()} />
            </PlatformSection>
            <PlatformSimulation open={isOpen("simulation")} onOpenChange={(o) => setOpen("simulation", o)} />
          </>
        )}
      </Show>
    </Page>
  );
}

function Header(props: { view: PlatformView }) {
  return (
    <div class="grid grid-cols-[repeat(auto-fit,minmax(190px,1fr))] gap-4" data-testid="platform-header">
      <Fact label="Platform" sub={props.view.system ? "the platform system's health" : "needs a fleet-wide system read"}>
        <Show when={props.view.system} fallback={<span class="text-base-content/30">-</span>}>
          {(s) => (
            <A href={`/systems/${s().id}`} class="inline-flex items-center gap-1 hover:underline">
              <span data-testid="platform-health">
                <HealthBadge health={s().health} size="md" />
              </span>
              <ArrowUpRight size={14} />
            </A>
          )}
        </Show>
      </Fact>
      <KeyedFor each={props.view.servers} by={(s) => s.component}>
        {(s) => (
          <>
            <Fact label="Version" sub={s().name}>
              <span class="tnum block min-w-0 overflow-hidden text-ellipsis whitespace-nowrap" title={s().version ?? undefined} data-testid="platform-version" classList={{ "text-base-content/30": !s().version }}>{s().version ?? NOT_REPORTED}</span>
            </Fact>
            <Fact label="Up for" sub={s().started_at ? `since ${fmtTime(s().started_at!)}` : "no start time written yet"}>
              <span class="tnum" data-testid="platform-uptime">{serverUptime(props.view.read_at, s().started_at) ?? NOT_REPORTED}</span>
            </Fact>
          </>
        )}
      </KeyedFor>
    </div>
  );
}

function Fact(props: { label: string; sub: string; children: JSX.Element }) {
  return (
    <div class="card min-w-0 border border-base-300 bg-base-200">
      <div class="card-body min-w-0 gap-1 p-5">
        <div class="eyebrow">{props.label}</div>
        <div class="min-w-0 overflow-hidden text-2xl font-semibold leading-none">{props.children}</div>
        <div class="text-xs text-base-content/50">{props.sub}</div>
      </div>
    </div>
  );
}

// HEAD_TIP is a column header's tooltip trigger: the label itself, dotted, so
// the description is a keyboard focus away.
const HEAD_TIP = "inline-block max-w-24 cursor-help whitespace-normal text-right underline decoration-base-content/30 decoration-dotted underline-offset-4 focus-visible:outline-none focus-visible:text-primary";

function OutpostTable(props: { view: PlatformView }) {
  const columns = () => platformColumns(props.view);
  const groups = () => (props.view.groups ?? []).filter((g) => (g.keys ?? []).length > 0);
  // The Loss group (#1646) is a composed figure, not a key: the view says which
  // hops it composes, and a view that names none has no Loss column.
  const lossShown = () => (props.view.loss_hops ?? []).length > 0;
  const width = () => columns().length + 3 + (lossShown() ? 1 : 0);
  return (
    <>
    <OutpostCards view={props.view} />
    <div class="hidden overflow-x-auto rounded-box border border-base-300 xl:block">
      <table class="table table-xs [&_td]:px-2 [&_th]:px-2 [&_th]:align-bottom [&_th]:leading-tight" data-testid="platform-outposts">
        <thead>
          <tr>
            <th colSpan={2} />
            <KeyedFor each={groups()} by={(g) => g.name}>
              {(g) => <th colSpan={(g().keys ?? []).length} class="border-l border-base-300 text-center">{g().label}</th>}
            </KeyedFor>
            <Show when={lossShown()}>
              <th class="border-l border-base-300 text-center">Loss</th>
            </Show>
            <th />
          </tr>
          <tr>
            <th>Outpost</th>
            <th>Status</th>
            <KeyedFor each={columns()} by={(c) => c.key}>
              {(c) => (
                <th class="text-right">
                  <Show when={c().description} fallback={c().label}>
                    <InfoTip text={c().description} label={c().label} class={HEAD_TIP}>
                      {c().label}
                    </InfoTip>
                  </Show>
                </th>
              )}
            </KeyedFor>
            <Show when={lossShown()}>
              <th class="text-right">
                <InfoTip text={outpostLossHint(props.view)} label="Outpost Loss" class={HEAD_TIP}>
                  Outpost Loss
                </InfoTip>
              </th>
            </Show>
            <th />
          </tr>
        </thead>
        <tbody>
          <KeyedFor each={props.view.outposts} by={(n) => n.name} fallback={<tr><td colSpan={width()} class="text-base-content/50">No outposts yet.</td></tr>}>
            {(n) => <OutpostRow outpost={n()} view={props.view} />}
          </KeyedFor>
        </tbody>
      </table>
    </div>
    </>
  );
}

// OutpostCards is the outposts below the table's width (#2446): the table has a
// column per platform key and needs the full console width, so a narrower window
// reads each outpost as a card, its figures in a grid, and nothing runs off an edge.
function OutpostCards(props: { view: PlatformView }) {
  return (
    <ul class="flex flex-col gap-3 xl:hidden" data-testid="platform-outpost-cards">
      <KeyedFor each={props.view.outposts} by={(n) => n.name} fallback={<li class="text-sm text-base-content/50">No outposts yet.</li>}>
        {(n) => {
          const pill = () => outpostPill(n(), props.view.read_at);
          const rowHealth = () => healthOf(n().health);
          return (
            <li class="rounded-box border border-base-300 bg-base-200 p-3" classList={{ [ROW_TONE[rowHealth() ?? ""] ?? ""]: !!ROW_TONE[rowHealth() ?? ""] }} data-testid={`platform-outpost-card-${n().name}`}>
              <div class="flex flex-wrap items-center gap-2">
                <Show when={n().component} fallback={<span class="font-medium">{entityLabel(n())}</span>}>
                  <A href={`/components/${n().component}`} class="font-medium hover:underline">{entityLabel(n())}</A>
                </Show>
                <span class={`badge badge-sm ${PILL[pill()]}`}>{pill()}</span>
                <Show when={rowHealth()}>{(h) => <HealthBadge health={h()} size="xs" />}</Show>
                <A href={`/outposts?n=${encodeURIComponent(n().name)}`} class="btn btn-quiet btn-xs btn-square ml-auto" aria-label={`Open ${n().name} in Outposts`}>
                  <ArrowUpRight size={14} />
                </A>
              </div>
              <dl class="mt-2 grid grid-cols-2 gap-x-4 gap-y-1 text-xs sm:grid-cols-3">
                <KeyedFor each={platformColumns(props.view)} by={(c) => c.key}>
                  {(c) => {
                    const text = () => figureText(n(), c());
                    return (
                      <div class="flex min-w-0 items-baseline justify-between gap-2">
                        <dt class="truncate text-base-content/60" title={c().label}>{c().label}</dt>
                        <dd class="tnum whitespace-nowrap" classList={{ "text-base-content/30": text() === NOT_REPORTED, "font-semibold text-error": figureTinted(n(), c()) }}>{text()}</dd>
                      </div>
                    );
                  }}
                </KeyedFor>
                <Show when={(props.view.loss_hops ?? []).length > 0}>
                  <div class="flex min-w-0 items-baseline justify-between gap-2">
                    <dt class="truncate text-base-content/60">Outpost Loss</dt>
                    <dd class="tnum whitespace-nowrap" classList={{ "text-base-content/30": n().loss === undefined, "font-semibold text-error": lossTinted(n().loss) }}>{outpostLossText(n())}</dd>
                  </div>
                </Show>
              </dl>
            </li>
          );
        }}
      </KeyedFor>
    </ul>
  );
}

function OutpostRow(props: { outpost: PlatformOutpost; view: PlatformView }) {
  const navigate = useNavigate();
  const pill = () => outpostPill(props.outpost, props.view.read_at);
  const rowHealth = () => healthOf(props.outpost.health);
  const title = () => entityLabel(props.outpost);
  const open = () => props.outpost.component && navigate(`/components/${props.outpost.component}`);
  return (
    <tr
      data-testid={`platform-outpost-${props.outpost.name}`}
      data-health={rowHealth() ?? undefined}
      class={ROW_TONE[rowHealth() ?? ""] ?? ""}
      classList={{ "cursor-pointer hover:bg-base-200": !!props.outpost.component }}
      onClick={open}
    >
      <td class="max-w-36">
        <div class="flex flex-col">
          <Show when={props.outpost.component} fallback={<span class="font-medium">{title()}</span>}>
            <A href={`/components/${props.outpost.component}`} class="font-medium hover:underline" onClick={(e) => e.stopPropagation()}>{title()}</A>
          </Show>
          <Show when={props.outpost.label && props.outpost.label !== props.outpost.name}>
            <span class="text-xs text-base-content/50">{props.outpost.name}</span>
          </Show>
        </div>
      </td>
      <td>
        <div class="flex flex-col items-start gap-1">
          <span class={`badge badge-sm ${PILL[pill()]}`}>{pill()}</span>
          <Show when={rowHealth()}>{(v) => <HealthBadge health={v()} size="xs" />}</Show>
        </div>
      </td>
      <KeyedFor each={platformColumns(props.view)} by={(c) => c.key}>
        {(c) => {
          const text = () => figureText(props.outpost, c());
          return (
            <td class="tnum min-w-24 whitespace-nowrap text-right" data-figure={c().key} classList={{ "text-base-content/30": text() === NOT_REPORTED, "font-semibold text-error": figureTinted(props.outpost, c()) }}>{text()}</td>
          );
        }}
      </KeyedFor>
      <Show when={(props.view.loss_hops ?? []).length > 0}>
        <td
          class="tnum whitespace-nowrap text-right"
          data-figure="loss"
          classList={{ "text-base-content/30": props.outpost.loss === undefined, "font-semibold text-error": lossTinted(props.outpost.loss) }}
        >
          {outpostLossText(props.outpost)}
        </td>
      </Show>
      <td class="text-right">
        <A href={drawerHref("outpost", props.outpost.name)} class="btn btn-quiet btn-xs btn-square" aria-label={`Open ${props.outpost.name} in Outposts`} title="Open this outpost's blade on the Outposts page" onClick={(e) => e.stopPropagation()}>
          <ArrowUpRight size={14} />
        </A>
      </td>
    </tr>
  );
}

// ServerPanels is one server's panel set: a folded section (its verdict and its
// open alarms on the line) over its four panels. A closed set mounts nothing, so
// it reads no series; its figures are the view's, already on the page.
function ServerPanels(props: { server: PlatformServer; view: PlatformView; open: boolean; onOpenChange: (open: boolean) => void }) {
  return (
    <div data-testid={`server-panels-${props.server.name}`}>
      <PlatformSection
        testid={`section-server-${props.server.name}`}
        label="Server"
        lead={
          <span class="flex items-center gap-2">
            <span class="font-medium">{props.server.name}</span>{" "}
            <HealthBadge health={props.server.health} size="xs" />
          </span>
        }
        summary={serverSummary(props.server)}
        open={props.open}
        onOpenChange={props.onOpenChange}
      >
        <div class="mb-3 text-xs">
          <A href={`/components/${props.server.component}`} class="inline-flex items-center gap-1 hover:underline">
            Open {props.server.name}'s component <ArrowUpRight size={12} />
          </A>
        </div>
        <div class="grid gap-4 xl:grid-cols-2">
          <KeyedFor each={props.server.groups} by={(p) => p.name}>
            {(p) => <Panel panel={p()} server={props.server} view={props.view} live />}
          </KeyedFor>
        </div>
      </PlatformSection>
    </div>
  );
}

// A panel is one view binding (#1830): its rows' history in one read, then
// their live samples off the page's one stream, so a sparkline moves as the
// server records rather than on a poll. A closed set binds nothing.
function Panel(props: { panel: PlatformPanel; server: PlatformServer; view: PlatformView; live: boolean }) {
  const alarms = () => panelAlarms(props.server, props.panel);
  const series = createViewBinding({
    binding: () => (props.live ? panelBinding(props.server, props.panel) : undefined),
    init: panelSeriesInit,
    fold: panelSeriesFold,
  });
  return (
    <section
      class="card border bg-base-200"
      classList={{ "border-warning/60": alarms().length > 0, "border-base-300": alarms().length === 0 }}
      data-testid={`server-panel-${props.panel.name}`}
      data-panel={props.panel.name}
      data-flagged={alarms().length > 0 ? "true" : "false"}
    >
      <div class="card-body gap-2 p-4">
        <Eyebrow label={props.panel.label} />
        <For each={alarms()}>
          {(a) => (
            <div role="status" class="flex items-start gap-1.5 text-xs text-warning">
              <span class="mt-0.5 flex-none"><TriangleAlert size={13} /></span>
              <span>{a.message}</span>
            </div>
          )}
        </For>
        <div class="overflow-x-auto">
          <table class="table table-xs">
            <tbody>
              <KeyedFor each={panelSections(props.panel)} by={(sec) => sec.key.name}>
                {(sec) => <Section section={sec()} server={props.server} view={props.view} live={props.live} series={series.state()} />}
              </KeyedFor>
            </tbody>
          </table>
        </div>
      </div>
    </section>
  );
}

// KeyTip is a key's name as its tooltip trigger: the catalog description, and
// the way onto its series on the server's platform component.
function KeyTip(props: { k: PlatformPanelKey; server: PlatformServer }) {
  return (
    <Show when={props.k.description} fallback={<span>{props.k.label}</span>}>
      <InfoTip
        text={props.k.description!}
        label={props.k.label}
        href={`/components/${props.server.component}`}
        hrefText={`Open its series on ${props.server.name}`}
        sameTab
        class="cursor-help underline decoration-base-content/30 decoration-dotted underline-offset-4 focus-visible:outline-none focus-visible:text-primary"
      >
        {props.k.label}
      </InfoTip>
    </Show>
  );
}

// A section is one key. An unsplit key is a single row named by the key; a
// split or ranked key is a heading row over one row per instance. A ranked key
// with no window yet has nothing to rank, so it too is one not-reported row.
function Section(props: { section: PanelSection; server: PlatformServer; view: PlatformView; live: boolean; series?: PanelSeries }) {
  const single = () => {
    const rows = props.section.rows;
    return rows.length === 1 && rows[0].instance === "" && (!props.section.ranked || rows[0].value === null);
  };
  return (
    <Show
      when={!single()}
      fallback={
        <Row row={props.section.rows[0]} server={props.server} view={props.view} live={props.live} series={props.series}>
          <KeyTip k={props.section.key} server={props.server} />
        </Row>
      }
    >
      <tr>
        <th colSpan={3} class="pt-2 text-xs font-medium text-base-content/70">
          <KeyTip k={props.section.key} server={props.server} />
          <Show when={props.section.ranked}>
            <span class="ml-1.5 font-normal text-base-content/40">top {props.section.key.rank}, newest window</span>
          </Show>
        </th>
      </tr>
      <KeyedFor each={props.section.rows} by={(r) => r.instance}>
        {(r) => (
          <Row row={r()} server={props.server} view={props.view} live={props.live} series={props.series} indent>
            <span class="font-mono text-[11px] [overflow-wrap:anywhere]" title={r().instance}>
              <Show when={r().instance} fallback="owner-level">
                <PathBreaks text={r().instance} />
              </Show>
            </span>
          </Row>
        )}
      </KeyedFor>
    </Show>
  );
}

// PathBreaks lets a route template wrap after a slash, not mid-segment.
function PathBreaks(props: { text: string }) {
  const parts = () => props.text.split("/");
  return (
    <For each={parts()}>
      {(seg, i) => (
        <>
          {seg}
          {i() < parts().length - 1 ? "/" : ""}
          <wbr />
        </>
      )}
    </For>
  );
}

// A row is one series: its name, its last hour as a sparkline (its own key
// and instance of the panel's binding), its latest figure. Clicking it opens
// the full chart beneath. A key never sampled reads not reported.
function Row(props: { row: PanelRow; server: PlatformServer; view: PlatformView; live: boolean; series?: PanelSeries; indent?: boolean; children: JSX.Element }) {
  const [expanded, setExpanded] = createSignal(false);
  const reported = () => props.row.value !== null;
  const text = () => panelRowText(props.row);
  const now = () => Date.parse(props.view.read_at);
  const chart = (spark: boolean) => (
    <TimeseriesChart
      spark={spark}
      buckets={panelRowBuckets(props.series, props.row)}
      resolutionMs={PANEL_SERIES_RESOLUTION_S * 1000}
      now={now()}
      windowMs={PANEL_SERIES_HOURS * 3600_000}
      unit={props.row.unit ? ` ${props.row.unit}` : ""}
    />
  );
  return (
    <>
      <tr
        data-testid={`panel-row-${props.row.key}`}
        data-instance={props.row.instance}
        classList={{ "cursor-pointer outline-none hover:bg-base-content/5 focus-visible:bg-base-content/5 focus-visible:ring-1 focus-visible:ring-inset focus-visible:ring-primary": reported() }}
        aria-expanded={reported() ? expanded() : undefined}
        tabIndex={reported() ? 0 : undefined}
        onClick={() => reported() && setExpanded(!expanded())}
        onKeyDown={(e) => {
          // The row's own keys only: a tooltip trigger inside it keeps its Enter.
          if (e.target !== e.currentTarget || !reported() || (e.key !== "Enter" && e.key !== " ")) return;
          e.preventDefault();
          setExpanded(!expanded());
        }}
      >
        <td classList={{ "pl-5": !!props.indent }}>{props.children}</td>
        <td class="hidden w-40 sm:table-cell">
          <Show when={reported() && props.live}>{chart(true)}</Show>
        </td>
        <td class="tnum whitespace-nowrap text-right" classList={{ "text-base-content/30": !reported() }}>{text()}</td>
      </tr>
      <Show when={expanded()}>
        <tr>
          <td colSpan={3} class="bg-base-100">{chart(false)}</td>
        </tr>
      </Show>
    </>
  );
}
