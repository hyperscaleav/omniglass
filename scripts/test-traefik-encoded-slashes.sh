#!/usr/bin/env bash
# Test whether Traefik passes percent-encoded slashes through to the backend server.
# This validates issue #1138: qualified names with percent-encoded slashes in the URL path.
#
# Usage: ./test-traefik-encoded-slashes.sh [server-url]
# Defaults to testing against http://localhost:8080 if no server-url is given.

set -e

SERVER_URL="${1:-http://localhost:8080}"
TRAEFIK_URL="${TRAEFIK_URL:-http://localhost:18080}"

# Colors for output
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m' # No Color

test_path() {
	local path="$1"
	local desc="$2"

	echo -n "Testing $desc... "

	# Test direct request to server
	direct_code=$(curl -s -o /dev/null -w "%{http_code}" "$SERVER_URL$path")

	# Test request through Traefik
	traefik_code=$(curl -s -o /dev/null -w "%{http_code}" "$TRAEFIK_URL$path" -H "X-Forwarded-For: 127.0.0.1")

	if [ "$direct_code" = "$traefik_code" ]; then
		echo -e "${GREEN}PASS${NC} (both returned $direct_code)"
		return 0
	else
		echo -e "${RED}FAIL${NC} (direct: $direct_code, traefik: $traefik_code)"
		return 1
	fi
}

echo "Testing Traefik with percent-encoded slashes"
echo "Server URL: $SERVER_URL"
echo "Traefik URL: $TRAEFIK_URL"
echo ""

# Test various encoded paths
failures=0

# Test encoded slash (%2F)
test_path "/api/v1/endpoints/campus.room-a.%24comp.display-1%2Fhttps" "endpoint with encoded slash" || ((failures++))

# Test encoded dollar and slash (%24 and %2F)
test_path "/api/v1/secrets/%24platform%2Fdemo-panel-login" "secret with encoded dollar and slash" || ((failures++))

# Test simple encoded slash
test_path "/api/v1/test%2Fpath" "simple path with encoded slash" || ((failures++))

echo ""
if [ $failures -eq 0 ]; then
	echo -e "${GREEN}All tests passed!${NC}"
	echo "Traefik correctly passes percent-encoded slashes through to the backend."
	exit 0
else
	echo -e "${RED}$failures test(s) failed${NC}"
	echo "Traefik does not pass percent-encoded slashes through to the backend."
	echo "The chart may need the encodedCharacters setting configured."
	exit 1
fi
