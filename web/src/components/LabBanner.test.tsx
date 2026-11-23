import { afterEach, describe, expect, it } from "vitest";
import { cleanup, render } from "@solidjs/testing-library";
import LabBanner from "./LabBanner";

afterEach(cleanup);

// A lab says it is one on every screen (#1195): its data is disposable and its
// login is well known, so nobody should mistake it for a real deployment. A
// server that is not a lab shows nothing.
describe("LabBanner", () => {
  it("names a lab instance and why it differs", () => {
    const { getByRole } = render(() => <LabBanner lab={true} />);
    const banner = getByRole("status");
    expect(banner.textContent).toContain("Lab instance");
    expect(banner.textContent).toMatch(/disposable/i);
    // One thin line (#2433): the long explanation lives in the tooltip trigger.
    expect(banner.className).toMatch(/\bh-6\b/);
    expect(getByRole("button", { name: /about the lab instance/i })).toBeTruthy();
  });
  it("shows nothing on a server that is not a lab", () => {
    const { container } = render(() => <LabBanner lab={false} />);
    expect(container.textContent).toBe("");
  });
  it("shows nothing while the status is unknown", () => {
    const { container } = render(() => <LabBanner lab={undefined} />);
    expect(container.textContent).toBe("");
  });
});
