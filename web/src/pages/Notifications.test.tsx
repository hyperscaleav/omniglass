import { describe, it, expect, afterEach, vi } from "vitest";
import { render, fireEvent, screen, waitFor, within } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import Notifications from "./Notifications";
import * as lib from "../lib/notification_types";
import { NOTIFICATION_TYPES_KEY, type NotificationTypeRow } from "../lib/notification_types";
import { COMPONENTS_KEY, type Component } from "../lib/components";
import { ME_KEY, type Me } from "../lib/auth";
import { uuidFor } from "../lib/testids";

// The Notifications page (#618) is a FlatList over the /notification-types
// registry; official types are read-only, an operator's are editable by a
// caller holding notification_type:update, and the blade previews a type
// against a real alarm through the server, drafts included while editing.
const at = "2026-10-28T00:00:00Z";
const seed: NotificationTypeRow[] = [
  { id: uuidFor("alarm-raised"), name: "alarm-raised", label: "Alarm raised", subject: "[{{.SeverityLabel}}] {{.ComponentLabel}}", text: "{{.Message}}", official: true, created_at: at, updated_at: at },
  { id: uuidFor("alarm-cleared"), name: "alarm-cleared", label: "Alarm cleared", subject: "[Cleared] {{.ComponentLabel}}", text: "{{.Message}}", official: true, created_at: at, updated_at: at },
  { id: uuidFor("page-oncall"), name: "page-oncall", label: "Page on-call", subject: "{{.Severity | upper}} {{.Component}}", text: "{{.Message}}", official: false, created_at: at, updated_at: at },
];
const comps = [{ id: uuidFor("display-1"), name: "display-1", label: "Lobby display", system_count: 0 }] as Component[];

const admin: Me = { principal: { id: "u-root", kind: "human" }, human: { username: "root" }, permissions: [">"], grants: [] };
const viewer: Me = { principal: { id: "u-view", kind: "human" }, human: { username: "viewer" }, permissions: ["*:read"], grants: [] };

const asides = () => document.querySelectorAll("aside[data-blade]");

function mount(me: Me = admin) {
  const qc = new QueryClient({ defaultOptions: { queries: { staleTime: Infinity, retry: false } } });
  qc.setQueryData([...NOTIFICATION_TYPES_KEY], seed);
  qc.setQueryData([...COMPONENTS_KEY], comps);
  qc.setQueryData([...ME_KEY], me);
  return render(() => (
    <QueryClientProvider client={qc}>
      <Notifications />
    </QueryClientProvider>
  ));
}

async function openBlade(label: string): Promise<HTMLElement> {
  fireEvent.click(screen.getByText(label));
  return await waitFor(() => {
    const el = asides()[0];
    if (!el) throw new Error("no blade yet");
    return el as HTMLElement;
  });
}

describe("Notifications page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("lists the notification types with their subject and origin", () => {
    mount();
    expect(screen.getByText("alarm-raised")).toBeTruthy();
    expect(screen.getByText("page-oncall")).toBeTruthy();
    expect(screen.getByText("{{.Severity | upper}} {{.Component}}")).toBeTruthy();
    expect(screen.getAllByText("official").length).toBe(2);
  });

  it("offers New notification type to a writer and hides it from a viewer", () => {
    const { unmount } = mount(admin);
    expect(screen.getByText("New notification type")).toBeTruthy();
    unmount();
    mount(viewer);
    expect(screen.queryByText("New notification type")).toBeNull();
  });

  it("greys Edit on an official type", async () => {
    mount(admin);
    const blade = await openBlade("Alarm raised");
    expect((within(blade).getByLabelText("Edit") as HTMLButtonElement).disabled).toBe(true);
  });

  it("previews against a component's alarm and lists the facts a template reads", async () => {
    const spy = vi.spyOn(lib, "previewNotificationType").mockResolvedValue({
      subject: "HIGH display-1",
      text: "no video",
      transition: "raised",
      facts: [{ name: "Component", value: "display-1" }, { name: "Place", value: "" }],
    } as lib.NotificationPreview);
    mount(admin);
    const blade = await openBlade("Page on-call");
    const picker = within(blade).getByLabelText("Component") as HTMLSelectElement;
    fireEvent.change(picker, { target: { value: uuidFor("display-1") } });
    fireEvent.click(within(blade).getByText("Preview", { selector: "button" }));
    await waitFor(() => expect(within(blade).getByText("HIGH display-1")).toBeTruthy());
    expect(spy).toHaveBeenCalledWith("page-oncall", { component: uuidFor("display-1"), transition: undefined });
    expect(within(blade).getByText(".Component")).toBeTruthy();
    expect(within(blade).getByText("empty")).toBeTruthy();
  });

  it("previews the draft while editing, so what it shows is what Save would store", async () => {
    const spy = vi.spyOn(lib, "previewNotificationType").mockResolvedValue({
      subject: "x", text: "y", transition: "cleared", facts: [],
    } as lib.NotificationPreview);
    mount(admin);
    const blade = await openBlade("Page on-call");
    fireEvent.click(within(blade).getByLabelText("Edit"));
    const subject = within(blade).getByLabelText("Subject") as HTMLInputElement;
    fireEvent.input(subject, { target: { value: "{{.Transition}}" } });
    fireEvent.change(within(blade).getByLabelText("Component"), { target: { value: uuidFor("display-1") } });
    fireEvent.change(within(blade).getByLabelText("Transition"), { target: { value: "cleared" } });
    fireEvent.click(within(blade).getByText("Preview", { selector: "button" }));
    await waitFor(() => expect(spy).toHaveBeenCalled());
    expect(spy).toHaveBeenCalledWith("page-oncall", { component: uuidFor("display-1"), transition: "cleared", subject: "{{.Transition}}", text: "{{.Message}}" });
  });
});
