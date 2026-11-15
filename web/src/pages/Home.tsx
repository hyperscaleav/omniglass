import { Show, Suspense } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useQuery } from "@tanstack/solid-query";
import { useNavigate } from "@solidjs/router";
import Page from "../components/Page";
import { fleetView, FLEET_VIEW_KEY } from "../lib/fleet";
import { listLocations, LOCATIONS_KEY } from "../lib/locations";
import { listNodes, NODES_KEY } from "../lib/nodes";
import { can, useMe } from "../lib/auth";
import { VERDICT_HUE, verdictOf } from "../lib/health";
import { notUpCount, platformView, PLATFORM_VIEW_KEY, retryPlatformView } from "../lib/platformhealth";

// Home is the situation room. Every tile is a real, scoped read: locations,
// systems and open alarms (the fleet view), collectors (the nodes list) and
// the platform's own verdict. A tile that cannot say what it counts reads why
// (loading, an error, a missing permission), never a placeholder.
export default function Home() {
  const navigate = useNavigate();
  const me = useMe();
  const locs = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const fleet = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
  const readsNodes = () => can(me.data, "node", "read");
  // The Collectors tile counts the nodes list, which the node read opens.
  const nodes = useQuery(() => ({ queryKey: NODES_KEY, queryFn: listNodes, enabled: readsNodes() }));
  // The Platform tile (#1644): the platform system's verdict and how many
  // nodes are not up, the door to Platform health. Shown only where the page
  // itself opens, a node read. A node read held for only some places is
  // refused by the view, and the tile says so rather than reading forever.
  const platform = useQuery(() => ({ queryKey: PLATFORM_VIEW_KEY, queryFn: platformView, retry: retryPlatformView, enabled: readsNodes() }));
  const platformVerdict = () => verdictOf(platform.data?.system?.verdict);
  const platformUnit = () => {
    const v = platform.data;
    if (!v) {
      if (!platform.error) return "reading the platform";
      return (platform.error as { status?: number }).status === 403 ? "needs a fleet-wide node read" : "could not read the platform";
    }
    const down = notUpCount(v);
    return `${down} ${down === 1 ? "node" : "nodes"} not up`;
  };
  const who = () => me.data?.human?.username ?? me.data?.service?.name ?? "operator";

  return (
    <Page title={`Welcome, ${who()}`}>
      <div class="grid grid-cols-[repeat(auto-fit,minmax(190px,1fr))] gap-4">
        <Suspense fallback={<Stat label="Locations" value="…" unit="in scope" />}>
          <Stat label="Locations" value={String(locs.data?.length ?? 0)} unit="in your scope" tone="text-primary" onClick={() => navigate("/locations")} />
        </Suspense>
        {/* /alarms is a stub page until the alarm list lands, so this tile is a plain figure. */}
        <Suspense fallback={<Stat label="Open alarms" testid="home-open-alarms" value="…" unit="reading alarms" />}>
          <Stat
            label="Open alarms"
            testid="home-open-alarms"
            {...countOf(fleet.isError, fleet.data?.open_alarms, "open now in your scope", "needs a component read")}
          />
        </Suspense>
        <Suspense fallback={<Stat label="Systems" testid="home-systems" value="…" unit="in scope" />}>
          <Stat
            label="Systems"
            testid="home-systems"
            {...countOf(fleet.isError, fleet.data && (fleet.data.systems ?? []).length, "in your scope")}
            onClick={() => navigate("/systems")}
          />
        </Suspense>
        <Show when={readsNodes()}>
          <Suspense fallback={<Stat label="Collectors" testid="home-collectors" value="…" unit="enrolled" />}>
            <Stat
              label="Collectors"
              testid="home-collectors"
              {...countOf(nodes.isError, nodes.data && nodes.data.length, "in your scope")}
              onClick={() => navigate("/nodes")}
            />
          </Suspense>
        </Show>
        <Show when={readsNodes()}>
          <Stat
            label="Platform"
            testid="home-platform"
            value={platformVerdict() ?? "-"}
            unit={platformUnit()}
            tone={platformVerdict() ? VERDICT_HUE[platformVerdict()!] : undefined}
            onClick={() => navigate("/platform")}
          />
        </Show>
      </div>
    </Page>
  );
}

// countOf is a count tile's value and unit. An error reads as one, and a read
// that succeeded without the number (a caller short of the permission behind
// it) says what it lacks; a real zero stays a zero.
function countOf(failed: boolean, count: number | undefined, unit: string, missing = "could not be read") {
  if (failed) return { value: "error", unit: "could not be read", tone: "text-error" };
  if (count === undefined) return { value: "-", unit: missing };
  return { value: String(count), unit, tone: "text-primary" };
}

function Stat(props: { label: string; value: string; unit: string; tone?: string; testid?: string; onClick?: () => void }) {
  // A clickable stat renders as a real <button> (keyboard-operable); a static one
  // stays a plain div.
  return (
    <Dynamic
      component={props.onClick ? "button" : "div"}
      type={props.onClick ? "button" : undefined}
      class="card w-full border border-base-300 bg-base-200 text-left"
      classList={{ "cursor-pointer hover:border-base-content/20": !!props.onClick }}
      onClick={props.onClick}
      data-testid={props.testid}
    >
      <div class="card-body gap-1 p-5">
        <div class="eyebrow">{props.label}</div>
        <div class="tnum text-3xl font-semibold leading-none" classList={{ [props.tone ?? ""]: !!props.tone, "text-base-content/30": !props.tone }}>{props.value}</div>
        <div class="text-xs text-base-content/50">{props.unit}</div>
      </div>
    </Dynamic>
  );
}
