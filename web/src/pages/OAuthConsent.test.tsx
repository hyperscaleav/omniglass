import { describe, it, expect, vi, afterEach } from "vitest";
import { render, fireEvent, screen, waitFor } from "@solidjs/testing-library";
import { QueryClient, QueryClientProvider } from "@tanstack/solid-query";
import { Router, Route } from "@solidjs/router";
import OAuthConsent from "./OAuthConsent";
import { consentRequest, redirectHost } from "../lib/oauth";

// The consent page names the client asking to act for the person and the roles
// they may hand it; the answer (approve with roles, or deny) goes to the server,
// and the browser follows the redirect it returns. The server is stubbed on
// fetch and the redirect on window.location.assign.
const REQ =
  "/oauth/authorize?client_id=c-1&redirect_uri=http%3A%2F%2F127.0.0.1%3A5173%2Fcb&code_challenge=abc&code_challenge_method=S256&state=s-1&resource=http%3A%2F%2Flocalhost%2Fmcp";

const CLIENT = {
  id: "c-1",
  client_name: "Claude Code",
  redirect_uris: ["http://127.0.0.1:5173/cb"],
  roles: ["auditor", "operator"],
};

function json(body: unknown, status = 200) {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

function mount() {
  const qc = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(() => (
    <QueryClientProvider client={qc}>
      <Router>
        <Route path="*" component={OAuthConsent} />
      </Router>
    </QueryClientProvider>
  ));
}

// stub opens the page at path, answers the client read, and records the
// authorize body. The location stub copies the location after the push, so
// the router reads the request from it.
function stub(path: string, client: unknown = CLIENT, clientStatus = 200) {
  window.history.pushState({}, "", path);
  const posted: unknown[] = [];
  vi.spyOn(globalThis, "fetch").mockImplementation(async (input) => {
    const req = input as Request;
    if (req.url.includes(":authorize")) {
      const body = await req.json();
      posted.push(body);
      const v = body.approved
        ? "code=k&iss=x&state=s-1"
        : "error=access_denied&iss=x&state=s-1";
      return json({ redirect_to: `http://127.0.0.1:5173/cb?${v}` });
    }
    return json(client, clientStatus);
  });
  const assign = vi.fn();
  vi.spyOn(window, "location", "get").mockReturnValue({
    ...window.location,
    assign,
  } as Location);
  return { posted, assign };
}

describe("consentRequest", () => {
  it("reads the forwarded request and refuses one missing a field or not S256", () => {
    const q = {
      client_id: "c",
      redirect_uri: "http://127.0.0.1/cb",
      code_challenge: "x",
      code_challenge_method: "S256",
      resource: "r",
    };
    expect(consentRequest(q)).toEqual({ ...q, state: "" });
    expect(consentRequest({ ...q, client_id: undefined })).toBeNull();
    expect(consentRequest({ ...q, code_challenge_method: "plain" })).toBeNull();
    expect(consentRequest({ ...q, resource: "" })).toBeNull();
  });

  it("names the redirect by its host", () => {
    expect(redirectHost("http://127.0.0.1:5173/cb")).toBe("127.0.0.1:5173");
    expect(redirectHost("not a url")).toBe("not a url");
  });
});

describe("OAuth consent page", () => {
  afterEach(() => vi.restoreAllMocks());

  it("approves the chosen roles and follows the code back to the client", async () => {
    const { posted, assign } = stub(REQ);
    mount();
    expect(await screen.findByText("Connect Claude Code")).toBeTruthy();
    expect(screen.getByText("127.0.0.1:5173")).toBeTruthy();
    const approve = screen.getByRole("button", {
      name: /Approve/,
    }) as HTMLButtonElement;
    expect(approve.disabled).toBe(true);
    fireEvent.click(screen.getByRole("checkbox", { name: "operator" }));
    expect(approve.disabled).toBe(false);
    fireEvent.click(approve);
    await waitFor(() =>
      expect(assign).toHaveBeenCalledWith(
        "http://127.0.0.1:5173/cb?code=k&iss=x&state=s-1",
      ),
    );
    expect(posted[0]).toMatchObject({
      approved: true,
      roles: ["operator"],
      redirect_uri: "http://127.0.0.1:5173/cb",
      code_challenge: "abc",
      code_challenge_method: "S256",
      state: "s-1",
      resource: "http://localhost/mcp",
    });
  });

  it("names the host serving a metadata document client, and no host for a registered one", async () => {
    stub(REQ, { ...CLIENT, metadata_host: "client.example" });
    const { unmount } = mount();
    expect(await screen.findByText("client.example")).toBeTruthy();
    expect(screen.getByTestId("metadata-host").textContent).toContain(
      "served from client.example",
    );
    unmount();
    vi.restoreAllMocks();
    stub(REQ);
    mount();
    expect(await screen.findByText("Connect Claude Code")).toBeTruthy();
    expect(screen.queryByTestId("metadata-host")).toBeNull();
  });

  it("denies with no roles and sends access_denied back", async () => {
    const { posted, assign } = stub(REQ);
    mount();
    fireEvent.click(await screen.findByRole("button", { name: /Deny/ }));
    await waitFor(() =>
      expect(assign).toHaveBeenCalledWith(
        "http://127.0.0.1:5173/cb?error=access_denied&iss=x&state=s-1",
      ),
    );
    expect(posted[0]).toMatchObject({ approved: false, roles: [] });
  });

  it("says so when the request is incomplete, without asking the server", async () => {
    window.history.pushState({}, "", "/oauth/authorize?client_id=c-1");
    const spy = vi.spyOn(globalThis, "fetch");
    mount();
    expect(
      await screen.findByText(/This sign-in request is incomplete/),
    ).toBeTruthy();
    expect(spy).not.toHaveBeenCalled();
  });

  it("says so when no client is registered under the request", async () => {
    stub(REQ, { title: "Not Found", status: 404 }, 404);
    mount();
    expect(await screen.findByText(/No client is registered/)).toBeTruthy();
  });
});
