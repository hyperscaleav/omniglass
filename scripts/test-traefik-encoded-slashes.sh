#!/usr/bin/env bash
# Qualified-name addressing through the chart's default ingress (#1138).
#
# A qualified name rides the URL path with its slash percent-encoded
# (`/api/v1/endpoints/<addr>%2Fhttps`, `/api/v1/secrets/%24platform%2F<name>`,
# ADR-1133). The chart's default ingress class is Traefik, and a Traefik release
# may reject an encoded slash at the entry point (a 400 at the edge while direct
# access works). This runs a pinned Traefik container in front of a backend that
# echoes the raw request path, and asserts for each version that the status
# through Traefik equals the status direct and that the backend saw the path
# byte for byte.
#
# Needs docker and python3 on Linux (the containers share the host network, on
# free ports). Usage: scripts/test-traefik-encoded-slashes.sh
# Override the pinned versions with TRAEFIK_VERSIONS="3.0.4 3.7.19".
set -euo pipefail

versions=${TRAEFIK_VERSIONS:-"3.0.4 3.6.25 3.7.19"}
paths=(
  '/api/v1/endpoints/boi.17c.415a.%24comp.display-1%2Fhttps'
  '/api/v1/secrets/%24platform%2Fdemo-panel-login'
)

free_port() {
  python3 -c 'import socket; s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'
}

work=$(mktemp -d)
backend_port=$(free_port)
edge_port=$(free_port)
container="og-traefik-check-$$"
backend_pid=
cleanup() {
  docker rm -f "$container" >/dev/null 2>&1 || true
  [ -n "$backend_pid" ] && kill "$backend_pid" 2>/dev/null || true
  rm -rf "$work"
}
trap cleanup EXIT

# The backend answers 200 with the raw request target, so the check sees exactly
# what an ingress forwarded.
cat >"$work/echo.py" <<PY
import http.server

class Echo(http.server.BaseHTTPRequestHandler):
    def do_GET(self):
        body = self.path.encode()
        self.send_response(200)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def log_message(self, *args):
        pass

http.server.ThreadingHTTPServer(("127.0.0.1", $backend_port), Echo).serve_forever()
PY
python3 "$work/echo.py" &
backend_pid=$!

# The chart's Ingress is a plain "/" Prefix path with no annotations and no
# entry point options, so the check configures none either.
cat >"$work/traefik.yml" <<YML
entryPoints:
  web:
    address: "127.0.0.1:$edge_port"
providers:
  file:
    directory: /etc/traefik/dyn
YML
mkdir "$work/dyn"
cat >"$work/dyn/route.yml" <<YML
http:
  routers:
    omniglass: {rule: "PathPrefix(\`/\`)", service: omniglass, entryPoints: [web]}
  services:
    omniglass:
      loadBalancer: {servers: [{url: "http://127.0.0.1:$backend_port"}]}
YML

fetch() { curl -s --retry 20 --retry-connrefused --retry-delay 1 -o "$2" -w '%{http_code}' "$1"; }

failures=0
for v in $versions; do
  docker rm -f "$container" >/dev/null 2>&1 || true
  docker run -d --rm --name "$container" --network host \
    -v "$work/traefik.yml:/etc/traefik/traefik.yml:ro" \
    -v "$work/dyn:/etc/traefik/dyn:ro" "traefik:v$v" >/dev/null
  for p in "${paths[@]}"; do
    direct=$(fetch "http://127.0.0.1:$backend_port$p" "$work/direct.out")
    edge=$(fetch "http://127.0.0.1:$edge_port$p" "$work/edge.out")
    if [ "$direct" = "$edge" ] && [ "$(cat "$work/edge.out")" = "$p" ]; then
      echo "PASS traefik v$v $p (direct $direct, ingress $edge, path intact)"
    else
      echo "FAIL traefik v$v $p (direct $direct, ingress $edge, backend saw: $(cat "$work/edge.out"))" >&2
      failures=$((failures + 1))
    fi
  done
done
[ "$failures" -eq 0 ]
