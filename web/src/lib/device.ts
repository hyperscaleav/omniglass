// deviceLabel renders a raw User-Agent into a short, human-friendly device string
// ("Chrome on macOS"), the industry-standard hint for a session list. It is a coarse
// heuristic, not a full UA parser (no dependency), and never a security signal, since a
// User-Agent is client-supplied and spoofable. An empty UA (a CLI/API token, which has
// no browser) reads as "CLI / API"; an unrecognized one as "Unknown device".
export function deviceLabel(ua?: string): string {
  if (!ua) return "CLI / API";
  // Order matters: an iOS UA contains "Mac OS X" and an Android UA contains "Linux", so
  // the more specific mobile checks come first.
  const os = /iPhone|iPad/.test(ua)
    ? "iOS"
    : /Android/.test(ua)
      ? "Android"
      : /Windows/.test(ua)
        ? "Windows"
        : /Mac OS X|Macintosh/.test(ua)
          ? "macOS"
          : /Linux/.test(ua)
            ? "Linux"
            : "";
  const browser = /Edg\//.test(ua)
    ? "Edge"
    : /OPR\/|Opera/.test(ua)
      ? "Opera"
      : /Chrome\//.test(ua)
        ? "Chrome"
        : /Firefox\//.test(ua)
          ? "Firefox"
          : /Safari\//.test(ua) && !/Chrome/.test(ua)
            ? "Safari"
            : /curl/i.test(ua)
              ? "curl"
              : "";
  if (browser && os) return `${browser} on ${os}`;
  return browser || os || "Unknown device";
}
