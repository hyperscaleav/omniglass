import { describe, it, expect, vi } from "vitest";
import { render, fireEvent } from "@solidjs/testing-library";
import FieldControl from "./FieldControl";

describe("FieldControl read mode", () => {
  it("renders the resolved value, muted, for an inherited field", () => {
    const { getByText, queryByLabelText } = render(() => (
      <FieldControl label="Diagonal inches" dataType="int" resolved="55" isSet={false} />
    ));
    expect(getByText("55").className).toContain("text-base-content/70"); // muted
    expect(queryByLabelText("override")).toBeNull(); // no dot when inherited
  });

  it("marks an override with an accent dot on the key and an accent value", () => {
    const { getByText, getByLabelText } = render(() => (
      <FieldControl label="Diagonal inches" dataType="int" resolved="65" isSet={true} />
    ));
    expect(getByLabelText("override")).toBeTruthy(); // the dot
    expect(getByText("65").className).toContain("text-primary"); // accent value
  });

  it("shows a dash for an unset field with no default", () => {
    const { getByText } = render(() => (
      <FieldControl label="Asset tag" dataType="string" resolved="" isSet={false} />
    ));
    expect(getByText("—")).toBeTruthy();
  });
});

describe("FieldControl edit mode", () => {
  it("off shows the resolved value and no input; on shows the input", () => {
    const off = render(() => (
      <FieldControl label="Diagonal inches" dataType="int" resolved="55" isSet={false} editing overriding={false} canToggle />
    ));
    expect(off.queryByRole("textbox")).toBeNull();
    expect(off.getByText("55")).toBeTruthy();

    const on = render(() => (
      <FieldControl label="Diagonal inches" dataType="int" resolved="55" isSet editing overriding draft="65" canToggle onInput={() => {}} />
    ));
    expect((on.getByRole("spinbutton") as HTMLInputElement).value).toBe("65"); // number input seeded from the draft
  });

  it("toggling the Override switch calls onToggle", () => {
    const onToggle = vi.fn();
    const { getByRole } = render(() => (
      <FieldControl label="Diagonal inches" dataType="int" resolved="55" isSet={false} editing overriding={false} canToggle onToggle={onToggle} />
    ));
    fireEvent.click(getByRole("checkbox")); // the Override switch
    expect(onToggle).toHaveBeenCalledWith(true);
  });

  it("a bool inherits as the resolved word and overrides as a real toggle", () => {
    const inherited = render(() => (
      <FieldControl label="Wall mounted" dataType="bool" resolved="true" isSet={false} editing overriding={false} canToggle />
    ));
    // Off: just the Override switch, plus the resolved word; no value toggle.
    expect(inherited.getAllByRole("checkbox").length).toBe(1);
    expect(inherited.getByText("true")).toBeTruthy();

    const overridden = render(() => (
      <FieldControl label="Wall mounted" dataType="bool" resolved="true" isSet editing overriding draft="true" canToggle onInput={() => {}} />
    ));
    // On: the Override switch AND the editable bool toggle.
    expect(overridden.getAllByRole("checkbox").length).toBe(2);
  });
});

describe("FieldControl required", () => {
  it("marks required with a red asterisk and locks the switch on", () => {
    const { getByLabelText, getByRole } = render(() => (
      <FieldControl label="Serial number" dataType="string" resolved="" isSet={false} required editing overriding draft="" canToggle />
    ));
    expect(getByLabelText("required").textContent).toBe("*");
    expect((getByRole("checkbox") as HTMLInputElement).disabled).toBe(true); // cannot switch off
  });

  it("shows the required error only when invalid is set", () => {
    const calm = render(() => (
      <FieldControl label="Serial number" dataType="string" resolved="" isSet={false} required editing overriding draft="" canToggle />
    ));
    expect(calm.queryByText("This value is required")).toBeNull();

    const failed = render(() => (
      <FieldControl label="Serial number" dataType="string" resolved="" isSet={false} required editing overriding draft="" invalid canToggle />
    ));
    expect(failed.getByText("This value is required")).toBeTruthy();
  });
});
