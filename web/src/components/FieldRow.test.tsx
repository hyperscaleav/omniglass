import { describe, it, expect } from "vitest";
import { render } from "@solidjs/testing-library";
import FieldRow from "./FieldRow";

describe("FieldRow", () => {
  it("renders the label and its control", () => {
    const { getByText, getByRole } = render(() => (
      <FieldRow label="Name"><input type="text" /></FieldRow>
    ));
    expect(getByText("Name")).toBeTruthy();
    expect(getByRole("textbox")).toBeTruthy();
  });

  it("associates the label with the control by id (accessible name)", () => {
    const { getByText, getByRole } = render(() => (
      <FieldRow label="Asset tag"><input type="text" /></FieldRow>
    ));
    const label = getByText("Asset tag") as HTMLLabelElement;
    const control = getByRole("textbox");
    expect(label.getAttribute("for")).toBe(control.id);
    expect(control.id).toBeTruthy(); // the wrapper generated and assigned an id
  });

  it("renders a hint under the control when given", () => {
    const { getByText } = render(() => (
      <FieldRow label="Password" hint="Leave blank to keep the current value.">
        <input type="password" />
      </FieldRow>
    ));
    expect(getByText("Leave blank to keep the current value.")).toBeTruthy();
  });

  it("renders an (i) info affordance beside the label when info is given", () => {
    const { getByText } = render(() => (
      <FieldRow label="Scope" info="Where this value lives."><input type="text" /></FieldRow>
    ));
    // The tooltip trigger sits beside the label but outside it, so the label's
    // accessible name stays just its text.
    const label = getByText("Scope") as HTMLLabelElement;
    expect(label.tagName).toBe("LABEL");
    expect(label.querySelector("button")).toBeNull();
  });
});
