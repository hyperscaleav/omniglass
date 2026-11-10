# A lab run reads the sim hop: what the simulated endpoints answered against what reached the node
#
# The delivery table now opens with the sim hop (#1694): one row per protocol the nodes poll, read
# from the closed loop's marks. In is every request the simulators settled, out the replies that
# reached the node. It counts requests, not items, since a simulator cannot know which kinds an
# answer would carry. A healthy lab reads 0; a simulator that withholds or drops a reply reads it as
# its own cause, and end to end composes the sim hop into every kind (ADR-1694).

# demo-needs: docker-socket

Hide
Type `demo_hush 'omniglass lab measure --systems 3 --warmup 30s --window 1m --fleet-reads 3 --backlog 0 --json /tmp/lab-measure.json >/tmp/lab-measure.txt 2>/tmp/lab-measure.err'`
Enter
Wait@480s
Show
Type "# Ran unseen first (about four minutes): omniglass lab measure --systems 3 --window 1m --backlog 0"
Enter
Wait
Type "# It brought up a throwaway scale lab, measured it, and removed it"
Enter
Wait
Type "grep -E '^(ready|measuring|torn down)' /tmp/lab-measure.err"
Enter
Wait
Type "# The delivery table: a sim row per protocol first, then each kind across the ingest hop"
Enter
Wait
Type "sed -n '/^delivery /,$p' /tmp/lab-measure.txt"
Enter
Wait
Type "# Each sim row in the JSON: requests the sims settled, replies returned, and each cause of loss"
Enter
Wait
Type `jq -c '.delivery[] | select(.hop == "sim") | {protocol, in, out, loss, outcomes}' /tmp/lab-measure.json`
Enter
Wait
Type "# End to end composes the sim hop into every kind: a healthy lab reads 0"
Enter
Wait
Type `jq -c '.delivery_end_to_end' /tmp/lab-measure.json`
Enter
Wait
