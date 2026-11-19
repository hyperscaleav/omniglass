import { describe, it, expect, afterEach } from "vitest";
import { render, screen, fireEvent, cleanup } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import TroubleshootButton from "./TroubleshootButton";
import { ME_KEY, type Me } from "../lib/auth";
import { pendingTriage, requestTriage } from "../lib/handover";

// The troubleshoot button (#2164) asks omnibot to triage one alarm: it shows
// only for a person who may ask omnibot, and a click requests the triage-alarm
// run on the alarm by its component's name and rule, never its message.
const person = (permissions: string[]): Me => ({ user: { id: "u-1", kind: "person" }, person: { username: "jo" }, permissions, grants: [] });

function mount(me: Me, alarm = { component: "videobar-1", raised_by_rule: "icmp-unreachable", raised_at: "2026-11-18T10:00:00Z", message: "Video feed lost" }) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <TroubleshootButton alarm={alarm} />
    </QueryClientProvider>
  ));
}

describe("TroubleshootButton", () => {
  afterEach(() => {
    cleanup();
    requestTriage(undefined);
  });

  it("requests a triage of the alarm by its component and rule", () => {
    mount(person(["assistant:use"]));
    fireEvent.click(screen.getByRole("button", { name: "Troubleshoot Video feed lost with omnibot" }));
    expect(pendingTriage()).toEqual({ kind: "alarm", ref: "videobar-1/icmp-unreachable" });
  });

  it("names an alarm no rule raised by when it was raised", () => {
    mount(person(["assistant:use"]), { component: "videobar-1", raised_by_rule: "", raised_at: "2026-11-18T10:00:00Z", message: "" });
    fireEvent.click(screen.getByRole("button", { name: "Troubleshoot with omnibot" }));
    expect(pendingTriage()).toEqual({ kind: "alarm", ref: "videobar-1@2026-11-18T10:00:00Z" });
  });

  it("is absent for a person who may not ask omnibot", () => {
    mount(person(["component:read"]));
    expect(screen.queryByRole("button", { name: /with omnibot/ })).toBeNull();
  });
});
