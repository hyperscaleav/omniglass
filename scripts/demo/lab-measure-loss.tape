# A lab run reports what omniglass lost on the way in, as one loss ratio per kind per hop
#
# Every telemetry batch a node sends carries a delivery ledger: its boot, its seq, and how many
# items of each kind it holds. The server writes what became of each item onto the batch's
# receipt (stored, coalesced, deduplicated, or refused and why), and omniglass lab measure
# reconciles the ledgers against the receipts once every node has settled. Each kind and hop reads
# as one loss ratio, 0 healthy, with the cause of any loss named beside it (#1641, ADR-1641). The
# node hop comes first: what each node minted against what its spool shipped, with a batch the
# spool dropped named by the batches after it under its own cause, so the ingest row reads only
# what was lost after the node shipped it (#1692, ADR-1692).

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
Type "# The delivery table: each kind across the node hop, then the ingest hop, with the loss ratio and its cause"
Enter
Wait
Type "sed -n '/^delivery /,$p' /tmp/lab-measure.txt"
Enter
Wait
Type "# The same figures in the JSON a second run compares against: each kind that sent items reads 0 on both hops"
Enter
Wait
Type `jq -c '.delivery[] | {kind, hop, in, out, loss, deduplicated, outcomes}' /tmp/lab-measure.json`
Enter
Wait
Type "# The node's out is the ingest's in: a hole the node accounts for is its loss, never ingest missing"
Enter
Wait
Type `jq -c '.delivery | group_by(.kind)[] | {kind: .[0].kind, node_out: (map(select(.hop == "node"))[0].out), ingest_in: (map(select(.hop == "ingest"))[0].in)}' /tmp/lab-measure.json`
Enter
Wait
Type `jq -c '.delivery_end_to_end' /tmp/lab-measure.json`
Enter
Wait
