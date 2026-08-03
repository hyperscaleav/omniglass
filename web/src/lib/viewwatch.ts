// The client half of the :watch seam: a fetch-based SSE reader over
// GET /views/{name}:watch with notify-then-refetch semantics. No data rides
// the stream; every change event (including the connect baseline) tells the
// caller to re-run the view, which is exactly the query invalidation the
// useView hook performs. fetch (not EventSource) so the stored bearer token
// rides the same way it does on every API call, and so tests can stub the
// stream.
import { getToken } from "../api/client";

export interface WatchHandle {
  close(): void;
}

export interface WatchOptions {
  // The reconnect delay after a dropped stream. The server's fresh baseline
  // event covers whatever was missed while disconnected.
  retryMs?: number;
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
        data.push(line.slice(5).trim());
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
  const retryMs = opts.retryMs ?? 3000;
  const origin = typeof globalThis.location !== "undefined" ? globalThis.location.origin : "";
  const search = new URLSearchParams();
  for (const p of params ?? []) search.append("param", p);
  const qs = search.toString();
  const url = `${origin}/api/v1/views/${encodeURIComponent(name)}:watch${qs ? `?${qs}` : ""}`;

  let closed = false;
  const controller = { current: new AbortController() };

  const connect = async (): Promise<void> => {
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
        if (resp.ok && resp.body) {
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
        }
      } catch {
        // A dropped or aborted stream: fall through to the retry delay.
      }
      if (closed) return;
      await new Promise((r) => setTimeout(r, retryMs));
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
