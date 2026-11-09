import { For, Show, type JSX } from "solid-js";
import { A, useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Page from "../components/Page";
import HealthBadge from "../components/HealthBadge";
import InfoTip from "../components/InfoTip";
import { ArrowUpRight } from "../components/icons";
import { fmtTime } from "../lib/format";
import { LEAF_POLL_MS } from "../lib/component_leaf";
import { entityLabel } from "../lib/entities";
import { verdictOf } from "../lib/health";
import type { NodeStatus } from "../lib/nodes";
import {
  NOT_REPORTED,
  PLATFORM_VIEW_KEY,
  figureText,
  nodePill,
  platformColumns,
  platformRefusal,
  platformView,
  retryPlatformView,
  serverUptime,
  type PlatformNode,
  type PlatformView,
} from "../lib/platformhealth";

// Platform health (#1644): whether Omniglass itself is well. Every figure here
// is a sample on a platform component the fleet already has (each node and each
// server is a component of the platform system), so the page is a reading of
// the same model an operator monitors their own fleet with, turned on the
// monitor. That is the teaching point: a row opens onto the node's platform
// component, the ordinary component detail, where each figure has its history.
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
      subtitle="Omniglass watching itself: the build running, how long it has been up, and whether each node is checking in, keeping its schedule and draining its command queue."
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
      <For each={props.view.servers ?? []}>
        {(s) => (
          <>
            <Fact label="Version" sub={s.name}>
              <span class="tnum" data-testid="platform-version" classList={{ "text-base-content/30": !s.version }}>{s.version ?? NOT_REPORTED}</span>
            </Fact>
            <Fact label="Up for" sub={s.started_at ? `since ${fmtTime(s.started_at)}` : "no start time written yet"}>
              <span class="tnum" data-testid="platform-uptime">{serverUptime(props.view.read_at, s.started_at) ?? NOT_REPORTED}</span>
            </Fact>
          </>
        )}
      </For>
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
            <For each={groups()}>
              {(g) => <th colSpan={(g.keys ?? []).length} class="border-l border-base-300 text-center">{g.label}</th>}
            </For>
            <th />
          </tr>
          <tr>
            <th>Node</th>
            <th>Status</th>
            <For each={columns()}>
              {(c) => (
                <th class="text-right">
                  <Show when={c.description} fallback={c.label}>
                    <InfoTip
                      text={c.description}
                      label={c.label}
                      class="cursor-help whitespace-nowrap underline decoration-base-content/30 decoration-dotted underline-offset-4 focus-visible:outline-none focus-visible:text-primary"
                    >
                      {c.label}
                    </InfoTip>
                  </Show>
                </th>
              )}
            </For>
            <th />
          </tr>
        </thead>
        <tbody>
          <For each={props.view.nodes ?? []} fallback={<tr><td colSpan={columns().length + 3} class="text-base-content/50">No nodes yet.</td></tr>}>
            {(n) => <NodeRow node={n} view={props.view} />}
          </For>
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
      <For each={platformColumns(props.view)}>
        {(c) => {
          const text = figureText(props.node, c);
          return (
            <td class="tnum whitespace-nowrap text-right" data-figure={c.key} classList={{ "text-base-content/30": text === NOT_REPORTED }}>{text}</td>
          );
        }}
      </For>
      <td class="text-right">
        <A href={`/nodes?n=${encodeURIComponent(props.node.name)}`} class="btn btn-quiet btn-xs btn-square" aria-label={`Open ${props.node.name} in Nodes`} title="Open this node's blade on the Nodes page" onClick={(e) => e.stopPropagation()}>
          <ArrowUpRight size={14} />
        </A>
      </td>
    </tr>
  );
}
