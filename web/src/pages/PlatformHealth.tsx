import { For, Show, createSignal, type JSX } from "solid-js";
import { A, useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Eyebrow from "../components/Eyebrow";
import TimeseriesChart from "../components/TimeseriesChart";
import Page from "../components/Page";
import HealthBadge from "../components/HealthBadge";
import InfoTip from "../components/InfoTip";
import KeyedFor from "../components/KeyedFor";
import { ArrowUpRight, ChevronRight, TriangleAlert } from "../components/icons";
import { fmtTime } from "../lib/format";
import { LEAF_POLL_MS } from "../lib/component_leaf";
import { entityLabel } from "../lib/entities";
import { verdictOf } from "../lib/health";
import type { NodeStatus } from "../lib/nodes";
import { SERIES_POLL_MS, metricBuckets, metricBucketsKey } from "../lib/series";
import {
  NOT_REPORTED,
  PLATFORM_VIEW_KEY,
  figureText,
  nodePill,
  PANEL_SERIES_HOURS,
  PANEL_SERIES_RESOLUTION_S,
  panelAlarms,
  panelRowText,
  panelSections,
  panelSeriesRequest,
  platformColumns,
  platformRefusal,
  platformView,
  retryPlatformView,
  serverUptime,
  type PanelRow,
  type PanelSection,
  type PlatformNode,
  type PlatformPanel,
  type PlatformPanelKey,
  type PlatformServer,
  type PlatformView,
} from "../lib/platformhealth";

// Platform health (#1644): whether Omniglass itself is well. Every figure here
// is a sample on a platform component the fleet already has (each node and each
// server is a component of the platform system), so the page is a reading of
// the same model an operator monitors their own fleet with, turned on the
// monitor. That is the teaching point: a row opens onto the node's platform
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
// its component's recorded verdict, which the seeded rules decide. One clock:
// the pill and the uptime are both judged at the view's read_at.

const NEUTRAL_PILL = "bg-base-content/10 text-base-content/70 border-transparent";
const PILL: Record<NodeStatus, string> = {
  up: "badge-soft badge-success",
  down: "badge-soft badge-error",
  never: NEUTRAL_PILL,
};

// A row's tone is its verdict: the failure hues only, so a healthy fleet reads
// calm and the rows that need a look stand out.
const ROW_TONE: Record<string, string> = {
  outage: "bg-error/5",
  degraded: "bg-warning/5",
};

export default function PlatformHealth() {
  const view = useQuery(() => ({ queryKey: PLATFORM_VIEW_KEY, queryFn: platformView, retry: retryPlatformView, refetchInterval: LEAF_POLL_MS }));

  return (
    <Page
      title="Platform health"
      subtitle="Omniglass watching itself: the build running and how long it has been up, how each server is serving, ingesting, queueing and keeping its retention, and whether each node is checking in, keeping its schedule and draining its command queue."
      actions={
        <Show when={view.data?.lab}>
          <span class="badge badge-info badge-sm font-semibold uppercase tracking-wide" data-testid="platform-lab">Lab instance</span>
        </Show>
      }
    >
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
                Platform figures need a fleet-wide read: your component read does not reach the platform components, so each node shows only its check-in.
              </div>
            </Show>
            <KeyedFor each={v().servers} by={(s) => s.component}>
              {(s, i) => <ServerPanels server={s()} view={v()} open={i() === 0} />}
            </KeyedFor>
            <NodeTable view={v()} />
          </>
        )}
      </Show>
    </Page>
  );
}

function Header(props: { view: PlatformView }) {
  return (
    <div class="grid grid-cols-[repeat(auto-fit,minmax(190px,1fr))] gap-4" data-testid="platform-header">
      <Fact label="Platform" sub={props.view.system ? "the platform system's verdict" : "needs a fleet-wide system read"}>
        <Show when={props.view.system} fallback={<span class="text-base-content/30">-</span>}>
          {(s) => (
            <A href={`/systems/${s().id}`} class="inline-flex items-center gap-1 hover:underline">
              <span data-testid="platform-verdict">
                <HealthBadge verdict={s().verdict} size="md" />
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
              <span class="tnum" data-testid="platform-version" classList={{ "text-base-content/30": !s().version }}>{s().version ?? NOT_REPORTED}</span>
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
    <div class="card border border-base-300 bg-base-200">
      <div class="card-body gap-1 p-5">
        <div class="eyebrow">{props.label}</div>
        <div class="text-2xl font-semibold leading-none">{props.children}</div>
        <div class="text-xs text-base-content/50">{props.sub}</div>
      </div>
    </div>
  );
}

function NodeTable(props: { view: PlatformView }) {
  const columns = () => platformColumns(props.view);
  const groups = () => (props.view.groups ?? []).filter((g) => (g.keys ?? []).length > 0);
  return (
    <div class="overflow-x-auto rounded-box border border-base-300">
      <table class="table table-sm" data-testid="platform-nodes">
        <thead>
          <tr>
            <th colSpan={2} />
            <KeyedFor each={groups()} by={(g) => g.name}>
              {(g) => <th colSpan={(g().keys ?? []).length} class="border-l border-base-300 text-center">{g().label}</th>}
            </KeyedFor>
            <th />
          </tr>
          <tr>
            <th>Node</th>
            <th>Status</th>
            <KeyedFor each={columns()} by={(c) => c.key}>
              {(c) => (
                <th class="text-right">
                  <Show when={c().description} fallback={c().label}>
                    <InfoTip
                      text={c().description}
                      label={c().label}
                      class="cursor-help whitespace-nowrap underline decoration-base-content/30 decoration-dotted underline-offset-4 focus-visible:outline-none focus-visible:text-primary"
                    >
                      {c().label}
                    </InfoTip>
                  </Show>
                </th>
              )}
            </KeyedFor>
            <th />
          </tr>
        </thead>
        <tbody>
          <KeyedFor each={props.view.nodes} by={(n) => n.name} fallback={<tr><td colSpan={columns().length + 3} class="text-base-content/50">No nodes yet.</td></tr>}>
            {(n) => <NodeRow node={n()} view={props.view} />}
          </KeyedFor>
        </tbody>
      </table>
    </div>
  );
}

function NodeRow(props: { node: PlatformNode; view: PlatformView }) {
  const navigate = useNavigate();
  const pill = () => nodePill(props.node, props.view.read_at);
  const verdict = () => verdictOf(props.node.verdict);
  const title = () => entityLabel(props.node);
  const open = () => props.node.component && navigate(`/components/${props.node.component}`);
  return (
    <tr
      data-testid={`platform-node-${props.node.name}`}
      data-verdict={verdict() ?? undefined}
      class={ROW_TONE[verdict() ?? ""] ?? ""}
      classList={{ "cursor-pointer hover:bg-base-200": !!props.node.component }}
      onClick={open}
    >
      <td class="whitespace-nowrap">
        <div class="flex flex-col">
          <Show when={props.node.component} fallback={<span class="font-medium">{title()}</span>}>
            <A href={`/components/${props.node.component}`} class="font-medium hover:underline" onClick={(e) => e.stopPropagation()}>{title()}</A>
          </Show>
          <Show when={props.node.label && props.node.label !== props.node.name}>
            <span class="text-xs text-base-content/50">{props.node.name}</span>
          </Show>
        </div>
      </td>
      <td>
        <div class="flex items-center gap-2">
          <span class={`badge badge-sm ${PILL[pill()]}`}>{pill()}</span>
          <Show when={verdict()}>{(v) => <HealthBadge verdict={v()} size="xs" />}</Show>
        </div>
      </td>
      <KeyedFor each={platformColumns(props.view)} by={(c) => c.key}>
        {(c) => {
          const text = () => figureText(props.node, c());
          return (
            <td class="tnum whitespace-nowrap text-right" data-figure={c().key} classList={{ "text-base-content/30": text() === NOT_REPORTED }}>{text()}</td>
          );
        }}
      </KeyedFor>
      <td class="text-right">
        <A href={`/nodes?n=${encodeURIComponent(props.node.name)}`} class="btn btn-quiet btn-xs btn-square" aria-label={`Open ${props.node.name} in Nodes`} title="Open this node's blade on the Nodes page" onClick={(e) => e.stopPropagation()}>
          <ArrowUpRight size={14} />
        </A>
      </td>
    </tr>
  );
}

// ServerPanels is one server's panel set: a disclosure (the first one open)
// over its four panels. A closed set reads no series; its figures are the
// view's, already on the page.
function ServerPanels(props: { server: PlatformServer; view: PlatformView; open: boolean }) {
  const [open, setOpen] = createSignal(props.open);
  return (
    <details
      class="rounded-box border border-base-300 bg-base-100"
      data-testid={`server-panels-${props.server.name}`}
      // The set's own state, seeded once from props.open: a server that later
      // sorts ahead moves the index, not the set the operator is reading.
      open={open()}
      onToggle={(e) => setOpen(e.currentTarget.open)}
    >
      <summary class="flex cursor-pointer items-center gap-2 px-4 py-3">
        {/* A flex summary drops the native marker, so the chevron says the set opens. */}
        <span data-disclosure class="text-base-content/60 transition-transform" classList={{ "rotate-90": open() }}><ChevronRight size={14} /></span>
        <span class="eyebrow">Server</span>
        <A href={`/components/${props.server.component}`} class="font-medium hover:underline" onClick={(e) => e.stopPropagation()}>{props.server.name}</A>
        <HealthBadge verdict={props.server.verdict} size="xs" />
        <Show when={(props.server.alarms ?? []).length > 0}>
          <span class="badge badge-soft badge-warning badge-sm">{(props.server.alarms ?? []).length} open</span>
        </Show>
      </summary>
      <div class="grid gap-4 px-4 pb-4 xl:grid-cols-2">
        <KeyedFor each={props.server.groups} by={(p) => p.name}>
          {(p) => <Panel panel={p()} server={props.server} view={props.view} live={open()} />}
        </KeyedFor>
      </div>
    </details>
  );
}

function Panel(props: { panel: PlatformPanel; server: PlatformServer; view: PlatformView; live: boolean }) {
  const alarms = () => panelAlarms(props.server, props.panel);
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
                {(sec) => <Section section={sec()} server={props.server} view={props.view} live={props.live} />}
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
function Section(props: { section: PanelSection; server: PlatformServer; view: PlatformView; live: boolean }) {
  const single = () => {
    const rows = props.section.rows;
    return rows.length === 1 && rows[0].instance === "" && (!props.section.ranked || rows[0].value === null);
  };
  return (
    <Show
      when={!single()}
      fallback={
        <Row row={props.section.rows[0]} server={props.server} view={props.view} live={props.live}>
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
          <Row row={r()} server={props.server} view={props.view} live={props.live} indent>
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

// A row is one series: its name, its last hour as a sparkline, its latest
// figure. Clicking it opens the full chart beneath. A key never sampled reads
// not reported and asks for no series.
function Row(props: { row: PanelRow; server: PlatformServer; view: PlatformView; live: boolean; indent?: boolean; children: JSX.Element }) {
  const [expanded, setExpanded] = createSignal(false);
  const reported = () => props.row.value !== null;
  const req = () => panelSeriesRequest(props.server, props.row);
  const series = useQuery(() => ({
    queryKey: metricBucketsKey(req().kind, req().owner, req().metric, req().hours, req().resolutionSeconds, req().instance),
    queryFn: () => metricBuckets(req().kind, req().owner, req().metric, req().hours, req().resolutionSeconds, req().instance),
    enabled: props.live && reported(),
    refetchInterval: SERIES_POLL_MS,
  }));
  const text = () => panelRowText(props.row);
  const now = () => Date.parse(props.view.read_at);
  const chart = (spark: boolean) => (
    <TimeseriesChart
      spark={spark}
      buckets={series.data?.buckets ?? []}
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
