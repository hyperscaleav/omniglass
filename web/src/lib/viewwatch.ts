// The client half of the :watch seam: a fetch-based SSE reader over
// GET /views/{name}:watch with notify-then-refetch semantics. No data rides
// the stream; every change event (including the connect baseline) tells the
// caller to re-run the view, which is exactly the query invalidation the
// useView hook performs. fetch (not EventSource) so the stored bearer token
// rides the same way it does on every API call, and so tests can stub the
// stream.
import { getToken, notifyUnauthorized } from "../api/client";

export interface WatchHandle {
  close(): void;
}

export interface WatchOptions {
  // The base reconnect delay after a dropped stream. Successive failures back
  // off from it (doubling, capped, jittered) so N consoles reconnecting after
  // a server restart do not arrive as one herd. The server's fresh baseline
  // event covers whatever was missed while disconnected.
  retryMs?: number;
  // The ceiling for the backed-off delay.
  maxRetryMs?: number;
}

// A status the stream will never recover from by retrying: the caller is not
// entitled to this view (403), or it does not exist (404). Retrying either one
// is a permanent hot loop against a decision that will not change without a
// grant change, which arrives as a fresh page load.
function isPermanent(status: number): boolean {
  return status === 403 || status === 404;
}

// createSSEParser returns a chunk consumer that reassembles SSE frames across
// arbitrary chunk boundaries and dispatches one (type, data) per event.
// Comment lines (the server's heartbeats) and field lines other than
// event/data (id, retry) are consumed silently; an event with no explicit
// type dispatches as "message", per the SSE spec.
export function createSSEParser(onEvent: (type: string, data: string) => void): (chunk: string) => void {
  let buffer = "";
  let eventType = "";
  let data: string[] = [];
  const dispatch = () => {
    if (data.length > 0) {
      onEvent(eventType || "message", data.join("\n"));
    }
    eventType = "";
    data = [];
  };
  return (chunk: string) => {
    buffer += chunk;
    for (;;) {
      const nl = buffer.indexOf("\n");
      if (nl < 0) return;
      const line = buffer.slice(0, nl).replace(/\r$/, "");
      buffer = buffer.slice(nl + 1);
      if (line === "") {
        dispatch();
      } else if (line.startsWith(":")) {
        // A comment: the keep-alive heartbeat. Nothing to do.
      } else if (line.startsWith("event:")) {
        eventType = line.slice(6).trim();
      } else if (line.startsWith("data:")) {
        // The spec strips ONE leading space, not all whitespace: a payload's
        // own trailing space is significant and must survive.
        data.push(line.slice(5).replace(/^ /, ""));
      }
      // Other fields (id, retry) are valid SSE; the reconnect delay is ours.
    }
  };
}

// watchView opens the watch stream for a view and invokes onChange on every
// change notification. The connection reconnects with a fixed delay whenever
// the stream drops (the server's baseline event on the fresh stream covers
// the gap) until close() is called.
export function watchView(
  name: string,
  params: string[] | undefined,
  onChange: () => void,
  opts: WatchOptions = {},
): WatchHandle {
  const baseRetryMs = opts.retryMs ?? 3000;
  const maxRetryMs = opts.maxRetryMs ?? 30000;
  const origin = typeof globalThis.location !== "undefined" ? globalThis.location.origin : "";
  const search = new URLSearchParams();
  for (const p of params ?? []) search.append("param", p);
  const qs = search.toString();
  const url = `${origin}/api/v1/views/${encodeURIComponent(name)}:watch${qs ? `?${qs}` : ""}`;

  let closed = false;
  const controller = { current: new AbortController() };

  const connect = async (): Promise<void> => {
    let failures = 0;
    while (!closed) {
      controller.current = new AbortController();
      try {
        const headers: Record<string, string> = { Accept: "text/event-stream" };
        const token = getToken();
        if (token) headers.Authorization = `Bearer ${token}`;
        const resp = await globalThis.fetch(url, {
          headers,
          credentials: "include",
          signal: controller.current.signal,
        });
        if (resp.status === 401) {
          // The session ended. Raise the same signal every other API call
          // raises, so the guard redirects, and stop: reconnecting would poll
          // a dead session behind a login screen.
          closed = true;
          notifyUnauthorized();
          return;
        }
        if (isPermanent(resp.status)) {
          closed = true;
          return;
        }
        if (resp.ok && resp.body) {
          // A stream that opened is a healthy connection: the next drop
          // reconnects promptly rather than inheriting an earlier backoff.
          failures = 0;
          const parse = createSSEParser((type) => {
            if (type === "change") onChange();
          });
          const reader = resp.body.getReader();
          const dec = new TextDecoder();
          for (;;) {
            const { done, value } = await reader.read();
            if (done) break;
            parse(dec.decode(value, { stream: true }));
          }
        } else {
          failures++;
        }
      } catch {
        // A dropped or aborted stream: fall through to the retry delay.
        failures++;
      }
      if (closed) return;
      // Exponential backoff with jitter: a server restart drops every console
      // at once, and a fixed delay would bring them all back in lockstep.
      const backoff = Math.min(baseRetryMs * 2 ** Math.max(0, failures - 1), maxRetryMs);
      const delay = backoff * (0.8 + Math.random() * 0.4);
      await new Promise((r) => setTimeout(r, delay));
    }
  };
  void connect();

  return {
    close() {
      closed = true;
      controller.current.abort();
    },
  };
}
