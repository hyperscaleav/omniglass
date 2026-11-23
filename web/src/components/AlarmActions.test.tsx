import "@testing-library/jest-dom/vitest";
import { describe, it, expect, afterEach } from "vitest";
import { render, cleanup } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import AlarmActions from "./AlarmActions";
import { ME_KEY, type Me } from "../lib/auth";

const me = (permissions: string[]): Me => ({ user: { id: "p", kind: "person" }, person: { username: "ada" }, permissions, grants: [] });
const alarm = { id: "a-1", message: "no signal", component: "cam-1", raised_at: "2026-11-23T00:00:00Z" };

function mount(who: Me, show: Parameters<typeof AlarmActions>[0]["show"]) {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  qc.setQueryData([...ME_KEY], who);
  return render(() => (
    <QueryClientProvider client={qc}>
      <AlarmActions componentId="u-1" alarm={alarm} show={show} />
    </QueryClientProvider>
  ));
}

afterEach(cleanup);

describe("AlarmActions", () => {
  // The group is not drawn when no member is: an empty flex item would still
  // take a gap in its row.
  it("draws no group for a caller who may use none of its members", () => {
    const { container } = mount(me(["component:read"]), { ack: true, snooze: true, troubleshoot: true });
    expect(container.querySelector("[data-alarm-actions]")).toBeNull();
  });

  it("draws the group once one member is allowed", () => {
    const { container, getByRole } = mount(me(["alarm:acknowledge"]), { ack: true, snooze: true });
    expect(container.querySelector("[data-alarm-actions]")).not.toBeNull();
    expect(getByRole("button", { name: "Acknowledge" })).toBeInTheDocument();
  });
});
