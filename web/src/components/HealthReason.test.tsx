import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import HealthReason from "./HealthReason";

describe("HealthReason", () => {
  it("states the health and the server's reason for it", () => {
    const { getByTestId } = render(() => <HealthReason health="degraded" reason="1 member outside a slot has an open alarm (amp-1)" />);
    expect(getByTestId("health-reason").textContent).toBe("degraded: 1 member outside a slot has an open alarm (amp-1)");
  });

  it("says nothing for a healthy system: the badge already says it", () => {
    const { queryByTestId } = render(() => <HealthReason health="healthy" reason="no slot is impaired and no member has an open alarm" />);
    expect(queryByTestId("health-reason")).toBeNull();
  });

  it("says nothing without a reason", () => {
    const { queryByTestId } = render(() => <HealthReason health="healthy" />);
    expect(queryByTestId("health-reason")).toBeNull();
  });
});
