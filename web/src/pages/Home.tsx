import { Show, Suspense } from "solid-js";
import { Dynamic } from "solid-js/web";
import { useQuery } from "@tanstack/solid-query";
import { useNavigate } from "@solidjs/router";
import Page from "../components/Page";
import { listLocations, LOCATIONS_KEY } from "../lib/locations";
import { can, useMe } from "../lib/auth";
import { VERDICT_HUE, verdictOf } from "../lib/health";
import { notUpCount, platformView, PLATFORM_VIEW_KEY, retryPlatformView } from "../lib/platformhealth";

// Home is the situation room. It shows real signal where the platform has it
// (locations, this slice) and honest "coming soon" cards for the metrics whose
// collection backends land later, rather than mock data.
export default function Home() {
  const navigate = useNavigate();
  const me = useMe();
  const locs = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const readsNodes = () => can(me.data, "node", "read");
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
        <Stat label="Open alarms" value="—" unit="collection pending" />
        <Stat label="Systems" value="—" unit="collection pending" />
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
