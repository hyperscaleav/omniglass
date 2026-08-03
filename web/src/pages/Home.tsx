import { type JSX } from "solid-js";
import Page from "../components/Page";
import LineSeries from "../components/views/LineSeries";
import StatTiles from "../components/views/StatTiles";
import StatusGrid from "../components/views/StatusGrid";
import ViewTable from "../components/views/ViewTable";
import { useMe } from "../lib/auth";
import { useView } from "../lib/views";

// Home is the situation room, and the first coded page of the views tier. Every
// panel names a default view, a renderer, and the view's field-mapping, and owns
// no query of its own. That is the argument for the views layer made concrete: a
// page that reached for /components and /events directly would be one more
// bespoke read to keep in step with the estate, while this one gains whatever a
// view gains.
//
// Every panel is live. useView subscribes to each view's change stream, so a
// verdict that flips or an occurrence that lands appears here with no reload and
// no poll loop per widget.
export default function Home() {
  const me = useMe();
  const who = () => me.data?.human?.username ?? me.data?.service?.label ?? "operator";

  const counts = useView("estate-counts");
  const reachability = useView("component-reachability");
  // A short feed: the situation room wants the last few occurrences, not the
  // paged history an events surface is for.
  const feed = useView("event-feed", () => ["limit=8"]);

  return (
    <Page title={`Welcome, ${who()}`} subtitle="Your estate right now, live.">
      <div class="og-stack flex flex-col">
        <StatTiles result={() => counts.data} error={() => counts.error} mapping={TILE_MAP} />

        <Panel
          title="Reachability"
          hint="Every interface in your scope with its latest verdict. A never-probed interface reads unknown, so this is the whole fleet, not only the answered part."
        >
          <StatusGrid result={() => reachability.data} error={() => reachability.error} mapping={REACH_MAP} />
        </Panel>

        <Panel title="Recent occurrences" hint="The newest events across your scope, newest first.">
          <ViewTable result={() => feed.data} error={() => feed.error} />
        </Panel>
      </div>
    </Page>
  );
}

// The field-mappings the default views publish, restated here until the console
// reads them from the directory (#550). Restated is the right word and the known
// cost: rename a mapped column server-side and this copy resolves to nothing,
// silently, which is why closing that gap has its own issue rather than a note.
const TILE_MAP = { label: "tile", value: "count" };
const REACH_MAP = { label: "component", sublabel: "interface", value: "state", time: "since" };
const HISTORY_MAP = { time: "ts", series: "instance", value: "value" };

function Panel(props: { title: string; hint?: string; children: JSX.Element }) {
  return (
    <section class="card border border-base-300 bg-base-200">
      <div class="card-body gap-2 p-4">
        <div>
          <h2 class="text-sm font-semibold">{props.title}</h2>
          {props.hint ? <p class="text-xs text-base-content/60">{props.hint}</p> : null}
        </div>
        {props.children}
      </div>
    </section>
  );
}

// SampleHistoryPanel is the component blade's chart: one property key's readings
// over a window, plotted through the same renderer Home uses. Strictly
// historical, so the current value stays where it already is (the properties
// panel) rather than being restated as the last point of a series.
export function SampleHistoryPanel(props: { component: string; propertyKey: string; instance?: string }) {
  const history = useView("sample-history", () => {
    const params = [`owner=${props.component}`, `key=${props.propertyKey}`];
    if (props.instance) params.push(`instance=${props.instance}`);
    return params;
  });
  return (
    <Panel title={props.propertyKey} hint="The last 24 hours of readings.">
      <LineSeries result={() => history.data} error={() => history.error} mapping={HISTORY_MAP} />
    </Panel>
  );
}
