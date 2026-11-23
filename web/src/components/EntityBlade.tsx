import { useNavigate } from "@solidjs/router";
import { useQuery } from "@tanstack/solid-query";
import Button from "./Button";
import ComponentDetail from "./ComponentDetail";
import SystemDetail from "./SystemDetail";
import LocationDetail from "./LocationDetail";
import { Maximize } from "./icons";
import { useBlades, type BladeDef } from "../lib/blades";
import { FLEET_VIEW_KEY, fleetView } from "../lib/fleet";
import { entityLabel } from "../lib/entities";
import { LOCATIONS_KEY, listLocations } from "../lib/locations";
import { COMPONENTS_KEY, listComponents } from "../lib/components";

// The fleet blades (#799, refit in #826): ONE blade per fleet kind, where an
// operator lands from a row. Since #2423 a fleet blade's body IS the kind's
// detail body, the one its page renders (ComponentDetail, SystemDetail,
// LocationDetail with host "blade"), so a blade shows everything the page
// shows, reflowed for the narrower container. The blade adds only what a
// blade needs: its title, and Expand to the page's shareable address.
// fleetBlades.guard.test.ts fails if a kind forks a body of its own again.

function ExpandButton(props: { to: string }) {
  const navigate = useNavigate();
  const blades = useBlades();
  return (
    <Button
      square
      icon={Maximize}
      title="Expand"
      label="Expand"
      onClick={() => {
        blades.close();
        navigate(props.to);
      }}
    />
  );
}

export const systemBlade: BladeDef = {
  Title: (p) => {
    const view = useQuery(() => ({ queryKey: FLEET_VIEW_KEY, queryFn: fleetView }));
    const s = () => view.data?.systems?.find((x) => x.id === p.id);
    return <>{s() ? entityLabel(s()!) : "System"}</>;
  },
  Body: (p) => <SystemDetail id={p.id} host="blade" />,
  headerExtra: (p) => <ExpandButton to={`/systems/${p.id}`} />,
  width: "wide",
};

export const componentBlade: BladeDef = {
  Title: (p) => {
    const components = useQuery(() => ({ queryKey: COMPONENTS_KEY, queryFn: listComponents }));
    const c = () => (components.data ?? []).find((x) => x.id === p.id);
    return <>{c() ? entityLabel(c()!) : "Component"}</>;
  },
  Body: (p) => <ComponentDetail id={p.id} host="blade" />,
  headerExtra: (p) => <ExpandButton to={`/components/${p.id}`} />,
  width: "wide",
};

export const locationBlade: BladeDef = {
  Title: (p) => {
    const locations = useQuery(() => ({ queryKey: LOCATIONS_KEY, queryFn: listLocations }));
    const l = () => (locations.data ?? []).find((x) => x.id === p.id);
    return <>{l() ? entityLabel(l()!) : "Location"}</>;
  },
  Body: (p) => <LocationDetail id={p.id} host="blade" />,
  headerExtra: (p) => <ExpandButton to={`/locations/${p.id}`} />,
  width: "wide",
};
