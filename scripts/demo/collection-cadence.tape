# How often an install collects is a setting
#
# Two collection settings set the platform's cadences: how often each endpoint's reachability is
# probed (probe_seconds), and how often a driver poll that declares no schedule runs (poll_seconds).
# A driver's declared schedule and a node's own --probe-every still win. Both default to the
# 30-second heartbeat a node runs at, and neither can go below it (#1373).

Type "# The platform's cadences: the 30-second heartbeat by default"
Enter
Wait
Type `omniglass setting list | jq -c '.values.collection'`
Enter
Wait
Type "# Below the heartbeat is refused: no node runs faster than it checks in"
Enter
Wait
Type `curl -s -X PATCH -H "Authorization: Bearer $OMNIGLASS_TOKEN" -H 'Content-Type: application/merge-patch+json' "$OMNIGLASS_SERVER/api/v1/settings/collection" -d '{"probe_seconds": 10}' | jq -c '{status, errors: [.errors[].message]}'`
Enter
Wait
Type "# Probe every minute instead: half the probe rows a day"
Enter
Wait
Type `curl -s -X PATCH -H "Authorization: Bearer $OMNIGLASS_TOKEN" -H 'Content-Type: application/merge-patch+json' "$OMNIGLASS_SERVER/api/v1/settings/collection" -d '{"probe_seconds": 60}' >/dev/null`
Enter
Wait
Type `omniglass setting list | jq -c '{collection: .values.collection, from: .sources["collection.probe_seconds"]}'`
Enter
Wait
Type "# A node's own --probe-every still wins, and is held to its own heartbeat too"
Enter
Wait
Type `omniglass node run --server "$OMNIGLASS_SERVER" --name edge-hq --token unused --heartbeat 10s --probe-every 5s 2>&1 | head -1`
Enter
Wait
