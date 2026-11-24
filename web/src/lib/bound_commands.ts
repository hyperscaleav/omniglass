import { createMemo } from "solid-js";
import { useQuery } from "@tanstack/solid-query";
import { boundCommandTypes } from "./commands";
import { ENDPOINTS_KEY, listEndpoints } from "./endpoints";
import { DRIVERS_KEY, listDrivers } from "./drivers";
import { can, useMe } from "./auth";

// createBoundCommands is the one answer to "can a command reach this component":
// the command types a driver attached to one of its endpoints binds, for a caller
// who may issue. The panel's Issue button and an alarm row's Send command both
// ask it, so they can never disagree (#2466). `ready` is false until both reads
// have landed, so a caller does not read an empty list as "none can reach it".
export function createBoundCommands(componentId: () => string | undefined) {
  const me = useMe();
  const canIssue = () => can(me.data, "command", "issue");
  const on = () => canIssue() && !!componentId();
  const endpoints = useQuery(() => ({ queryKey: ENDPOINTS_KEY, queryFn: listEndpoints, enabled: on() }));
  const drivers = useQuery(() => ({ queryKey: DRIVERS_KEY, queryFn: listDrivers, enabled: on() }));
  const bound = createMemo(() => {
    const id = componentId();
    return id ? boundCommandTypes(id, endpoints.data ?? [], drivers.data ?? []) : [];
  });
  return { canIssue, bound, ready: () => !!endpoints.data && !!drivers.data, endpoints: () => endpoints.data ?? [], drivers: () => drivers.data ?? [] };
}
