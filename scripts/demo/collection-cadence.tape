# How often an install collects is a setting
#
# Two collection settings set the platform's cadences: how often each endpoint's reachability is
# probed (probe_seconds), and how often a driver poll that declares no schedule runs (poll_seconds).
# A driver's declared schedule and a node's own --probe-every still win. Both default to the
# 30-second heartbeat a node runs at, and neither can go below it (#1373).

Type "# The platform's cadences, and where each comes from: the 30-second heartbeat by default"
Enter
Wait
Type `omniglass setting list | jq -c '{collection: .values.collection, from: .sources["collection.probe_seconds"]}'`
Enter
Wait
Type "# An admin raises them on Admin > Settings; a value below the heartbeat is refused there"
Enter
Wait
Type "# A node's own --probe-every still wins over the platform, and is held to its own heartbeat"
Enter
Wait
Type `omniglass node run --server "$OMNIGLASS_SERVER" --name edge-hq --token unused --heartbeat 10s --probe-every 5s 2>&1 | head -1 || true`
Enter
Wait
