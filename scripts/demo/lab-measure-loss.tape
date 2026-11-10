# A lab run reports what omniglass lost on the way in, as one loss ratio per kind per hop
#
# Every telemetry batch a node sends carries a delivery ledger: its boot, its seq, and how many
# items of each kind it holds. The server writes what became of each item onto the batch's
# receipt (stored, coalesced, deduplicated, or refused and why), and omniglass lab measure
# reconciles the ledgers against the receipts once every node has settled. Each kind and hop reads
# as one loss ratio, 0 healthy, with the cause of any loss named beside it (#1641, ADR-1641). A
# batch the server Terms whole reads under its own cause, not missing (#1691, ADR-1691).

# demo-needs: docker-socket

Hide
Type `demo_hush 'omniglass lab measure --systems 3 --warmup 30s --window 1m --fleet-reads 3 --backlog 0 --json /tmp/lab-measure.json >/tmp/lab-measure.txt 2>/tmp/lab-measure.err'`
Enter
Wait@480s
Show
Type "# Ran unseen first (about four minutes): omniglass lab measure --systems 3 --window 1m --backlog 0"
Enter
Wait
Type "# It brought up a throwaway scale lab, measured it, settled every node boot, and removed it"
Enter
Wait
Type "grep -E '^(ready|measuring|delivery|torn down)' /tmp/lab-measure.err"
Enter
Wait
Type "# The delivery table: each kind across the ingest hop, in and out, the loss ratio and its cause"
Enter
Wait
Type "sed -n '/^delivery /,$p' /tmp/lab-measure.txt"
Enter
Wait
Type "# The same figures in the JSON a second run compares against: each kind that sent items reads 0"
Enter
Wait
Type `jq -c '.delivery[] | {kind, hop, in, out, loss, deduplicated, outcomes}' /tmp/lab-measure.json`
Enter
Wait
Type `jq -c '.delivery_end_to_end' /tmp/lab-measure.json`
Enter
Wait
Type "# A batch the server Terms whole reads under its cause; each is a count on every row, 0 here"
Enter
Wait
Type `jq -c '.delivery[] | {kind, termed: (.outcomes | {"refused-owner", "refused-task", "refused-batch", "refused-metadata", abandoned})}' /tmp/lab-measure.json`
Enter
Wait
