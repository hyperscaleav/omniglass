import { Show, Suspense } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { useNavigate } from "@solidjs/router";
import Page from "../components/Page";
import { listLocations, LOCATIONS_KEY } from "../lib/locations";
import { useMe } from "../lib/auth";

// Home is the situation room. It shows real signal where the platform has it
// (locations, this slice) and honest "coming soon" cards for the metrics whose
// collection backends land later, rather than mock data. The visual grid
// matches the design's Home.
export default function Home() {
  const navigate = useNavigate();
  const me = useMe();
  const locs = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
  const who = () => me.data?.human?.username ?? me.data?.service?.label ?? "operator";

  return (
    <Page title={`Welcome, ${who()}`} subtitle="Your environment at a glance. More lands here as collection comes online.">
      <div style={{ display: "grid", gap: "16px", "grid-template-columns": "repeat(auto-fit, minmax(190px, 1fr))" }}>
        <Suspense fallback={<Stat label="Locations" value="…" unit="in scope" />}>
          <Stat label="Locations" value={String(locs.data?.length ?? 0)} unit="in your scope" tone="var(--up)" onClick={() => navigate("/locations")} />
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
      class="card"
      onClick={props.onClick}
      style={{ padding: "16px 18px", cursor: props.onClick ? "pointer" : "default" }}
    >
      <div class="eyebrow" style={{ "margin-bottom": "8px" }}>{props.label}</div>
      <div class="tnum" style={{ "font-size": "30px", "font-weight": 600, "line-height": 1, color: props.tone ?? "var(--text-faint)" }}>{props.value}</div>
      <div style={{ "font-size": "12px", color: "var(--text-dim)", "margin-top": "5px" }}>{props.unit}</div>
    </div>
  );
}
