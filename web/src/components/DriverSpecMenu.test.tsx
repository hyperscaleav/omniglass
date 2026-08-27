import { describe, it, expect } from "vitest";
import { render, screen } from "@solidjs/testing-library";
import DriverSpecMenu from "./DriverSpecMenu";
import type { DriverSpec } from "../lib/drivers";

// The menu is a pure render of the spec (#813): the transport it rides, the
// inputs an attach must supply, and the three function families. These pin
// that each family surfaces, and that a spec without a family renders no empty
// section header for it.

const fullSpec: DriverSpec = {
  version: 1,
  transport: "tcp",
  inputs: [
    { name: "host", kind: "string", required: true },
    { name: "port", kind: "number", default: "51325" },
    { name: "login", kind: "secret", secret_type: "basic-auth", required: true },
  ],
  polls: [
    {
      name: "status",
      schedule: { every: "30s" },
      request: { line: "GET INPUT" },
      emits: [{ name: "video-input", extract: { regex: "^INPUT (\\S+)$" } }],
    },
  ],
  listeners: [
    {
      name: "events",
      arm: ["SUBSCRIBE INPUT"],
      match: { prefix: "EVT " },
      emits: [{ name: "video-input", extract: { regex: "^EVT INPUT (\\S+)$" } }],
    },
  ],
  commands: [{ command_type: "set-input", request: { line: "SET INPUT ${arg.input}" } }],
};

describe("DriverSpecMenu", () => {
  it("renders the transport, inputs, and all three families", () => {
    render(() => <DriverSpecMenu spec={fullSpec} />);
    expect(screen.getByText("tcp")).toBeTruthy();
    expect(screen.getByText("host")).toBeTruthy();
    expect(screen.getByText("basic-auth secret")).toBeTruthy();
    expect(screen.getAllByText("required").length).toBe(2);
    expect(screen.getByText("status")).toBeTruthy();
    expect(screen.getByText("every 30s")).toBeTruthy();
    expect(screen.getByText("events")).toBeTruthy();
    expect(screen.getByText('claims lines starting "EVT "')).toBeTruthy();
    expect(screen.getByText("set-input")).toBeTruthy();
    // The emit chips carry the canon names.
    expect(screen.getAllByText("video-input").length).toBe(2);
  });

  it("omits a family the spec does not declare", () => {
    render(() => (
      <DriverSpecMenu
        spec={{
          version: 1,
          transport: "snmp",
          polls: [
            {
              name: "scalars",
              schedule: { every: "60s" },
              request: { get: ["1.3.6.1.2.1.1.3.0"] },
              emits: [{ name: "uptime", extract: { oid: "1.3.6.1.2.1.1.3.0" } }],
            },
          ],
        }}
      />
    ));
    expect(screen.getByText("scalars")).toBeTruthy();
    expect(screen.queryByText("Listeners")).toBeNull();
    expect(screen.queryByText("Commands")).toBeNull();
    expect(screen.queryByText("Inputs")).toBeNull();
  });
});
