import { describe, it, expect, vi } from "vitest";
import { render, fireEvent, waitFor } from "@solidjs/testing-library";
import { RevealButton, CopyButton, GenerateButton } from "./InlineActions";

describe("RevealButton", () => {
  it("labels 'Reveal' when hidden and calls onToggle on click", () => {
    const onToggle = vi.fn();
    const { getByRole } = render(() => <RevealButton revealed={false} onToggle={onToggle} label="secret" />);
    fireEvent.click(getByRole("button", { name: "Reveal secret" }));
    expect(onToggle).toHaveBeenCalledTimes(1);
  });

  it("labels 'Hide' when revealed", () => {
    const { getByRole } = render(() => <RevealButton revealed={true} onToggle={() => {}} label="secret" />);
    expect(getByRole("button", { name: "Hide secret" })).toBeTruthy();
  });
});

describe("CopyButton", () => {
  it("calls onCopy on click", () => {
    const onCopy = vi.fn(() => true);
    const { getByRole } = render(() => <CopyButton onCopy={onCopy} label="token" />);
    fireEvent.click(getByRole("button", { name: "Copy token" }));
    expect(onCopy).toHaveBeenCalledTimes(1);
  });

  it("does not throw when onCopy reports failure (the parent owns the error)", async () => {
    const onCopy = vi.fn(async () => false);
    const { getByRole } = render(() => <CopyButton onCopy={onCopy} label="token" />);
    fireEvent.click(getByRole("button", { name: "Copy token" }));
    await waitFor(() => expect(onCopy).toHaveBeenCalled());
  });
});

describe("GenerateButton", () => {
  it("calls onGenerate on click", () => {
    const onGenerate = vi.fn();
    const { getByRole } = render(() => <GenerateButton onGenerate={onGenerate} label="Generate a strong password" />);
    fireEvent.click(getByRole("button", { name: "Generate a strong password" }));
    expect(onGenerate).toHaveBeenCalledTimes(1);
  });
});
