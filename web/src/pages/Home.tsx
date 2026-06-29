import { Suspense } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { useNavigate } from "@solidjs/router";
import Page from "../components/Page";
import { listLocations, LOCATIONS_KEY } from "../lib/locations";
import { useMe } from "../lib/auth";

// Home is the situation room. It shows real signal where the platform has it
// (locations, this slice) and honest "coming soon" cards for the metrics whose
// collection backends land later, rather than mock data.
export default function Home() {
  const navigate = useNavigate();
  const me = useMe();
  const locs = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const who = () => me.data?.human?.username ?? me.data?.service?.label ?? "operator";

  return (
    <Page title={`Welcome, ${who()}`}>
      <div class="grid grid-cols-[repeat(auto-fit,minmax(190px,1fr))] gap-4">
        <Suspense fallback={<Stat label="Locations" value="…" unit="in scope" />}>
          <Stat label="Locations" value={String(locs.data?.length ?? 0)} unit="in your scope" tone="text-primary" onClick={() => navigate("/locations")} />
        </Suspense>
        <Stat label="Open alarms" value="—" unit="collection pending" />
        <Stat label="Systems" value="—" unit="collection pending" />
        <Stat label="Collectors" value="—" unit="collection pending" />
      </div>
    </Page>
  );
}

function Stat(props: { label: string; value: string; unit: string; tone?: string; onClick?: () => void }) {
  return (
    <div
      class="card border border-base-300 bg-base-200"
      classList={{ "cursor-pointer hover:border-base-content/20": !!props.onClick }}
      onClick={props.onClick}
    >
      <div class="card-body gap-1 p-5">
        <div class="eyebrow">{props.label}</div>
        <div class="tnum text-3xl font-semibold leading-none" classList={{ [props.tone ?? ""]: !!props.tone, "text-base-content/30": !props.tone }}>{props.value}</div>
        <div class="text-xs text-base-content/50">{props.unit}</div>
      </div>
    </div>
  );
}
