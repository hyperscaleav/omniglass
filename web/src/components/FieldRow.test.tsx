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

  it("labels a bound field from the identity map rather than from a prop", () => {
    // A create form sits Display name next to Name, which is exactly where the
    // two got swapped twice. A bound field takes its label from lib/entities, so
    // there is no prop to get wrong.
    const { getByText } = render(() => (
      <>
        <FieldRow bind="display_name"><input type="text" /></FieldRow>
        <FieldRow bind="name"><input type="text" /></FieldRow>
      </>
    ));
    expect(getByText("Display name")).toBeTruthy();
    expect(getByText("Name")).toBeTruthy();
  });

  it("labels with the eyebrow when asked, so a blade field keeps one label style", () => {
    const { getByText } = render(() => (
      <FieldRow label="Website" eyebrow><input type="text" /></FieldRow>
    ));
    expect(getByText("Website").className).toContain("eyebrow");
  });

  it("joins inline actions to the control, and still labels the control", () => {
    // The field-level version of KVRow's join: the control and its buttons read
    // as ONE bordered control. FieldRow builds the wrapper itself rather than
    // taking one from the caller, and that is the whole point: it labels the
    // first ELEMENT it resolves from children, so a caller passing its own
    // `<div class="join">` would aim `for` at the div, and a <label> pointing at
    // a non-labelable element labels nothing at all.
    const { getByText, getByRole } = render(() => (
      <FieldRow label="Name" actions={<button type="button">Override</button>}>
        <input type="text" class="join-item" />
      </FieldRow>
    ));
    const control = getByRole("textbox");
    const action = getByRole("button");
    expect(control.closest(".join")).toBeTruthy();
    expect(action.closest(".join")).toBe(control.closest(".join"));
    // The empty-string guard first: `for=""` and an unlabelled control compare
    // equal, so without it this passes for a field that labels nothing at all.
    expect(control.id).toBeTruthy();
    expect((getByText("Name") as HTMLLabelElement).getAttribute("for")).toBe(control.id);
    // And the button stays OUT of the <label>, which is what keeps a labelable
    // element from stealing the control's accessible name.
    expect((getByText("Name") as HTMLLabelElement).querySelector("button")).toBeNull();
  });

  it("leaves a field with no inline actions unwrapped", () => {
    // Every other field in the console renders through here; growing a join
    // wrapper around all of them would restyle nineteen pages by accident.
    const { getByRole } = render(() => (
      <FieldRow label="Website"><input type="text" /></FieldRow>
    ));
    expect(getByRole("textbox").closest(".join")).toBeNull();
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

  // The provenance mark goes beside the LABEL rather than beside the value,
  // because the label occupies the same position whether the field is being
  // read or edited and a full-width input's value does not.
  it("marks a field whose value comes from elsewhere, beside the label and outside it", () => {
    const { getByText, getByRole } = render(() => (
      <FieldRow label="Stem" provenance="ceiling-mic"><input type="text" /></FieldRow>
    ));
    const label = getByText("Stem") as HTMLLabelElement;
    const dot = getByRole("button", { name: /Stem is inherited from ceiling-mic/ });
    // Inside the label's row, never inside the <label>: a labelable button in
    // there steals the control's accessible name and kills the hover.
    expect(label.querySelector("button")).toBeNull();
    expect(label.parentElement!.contains(dot)).toBe(true);
    expect(label.getAttribute("for")).toBe(getByRole("textbox").id);
  });

  it("marks nothing when the field states its own value", () => {
    const { queryByRole } = render(() => (
      <FieldRow label="Stem" provenance=""><input type="text" /></FieldRow>
    ));
    expect(queryByRole("button")).toBeNull();
  });

  // The dot sits between the label and the (i), so its position is the same on
  // a field that carries help text and on one that does not, which is what the
  // read state can match.
  it("puts the mark immediately after the label, ahead of the (i)", () => {
    const { getByText, getByRole } = render(() => (
      <FieldRow label="Stem" info="The prefix." provenance="mic"><input type="text" /></FieldRow>
    ));
    const row = (getByText("Stem") as HTMLLabelElement).parentElement!;
    const dot = getByRole("button", { name: /Stem is inherited from mic/ });
    const info = getByRole("button", { name: /More about Stem/ });
    expect(Array.from(row.children).indexOf(dot)).toBe(1);
    expect(Array.from(row.children).indexOf(info)).toBe(2);
  });
});
