import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import HealthReason from "./HealthReason";

describe("HealthReason", () => {
  it("states the health and the server's reason for it", () => {
    const { getByTestId } = render(() => <HealthReason health="degraded" reason="slot display is impaired (0 of 1 occupying)" />);
    expect(getByTestId("health-reason").textContent).toBe("degraded: slot display is impaired (0 of 1 occupying)");
  });

  it("says nothing for a healthy system: the badge already says it", () => {
    const { queryByTestId } = render(() => <HealthReason health="healthy" reason="no slot is impaired" />);
    expect(queryByTestId("health-reason")).toBeNull();
  });

  it("says nothing without a reason", () => {
    const { queryByTestId } = render(() => <HealthReason health="healthy" />);
    expect(queryByTestId("health-reason")).toBeNull();
  });
});
