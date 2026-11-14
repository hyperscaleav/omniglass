# The server reports how far behind its fleet it lands telemetry
#
# Once a report window the server measures its own pipeline from inside and lands the figures on its
# platform component: ingest lag (its clock at the batch's commit minus the sample's ts), the
# ingest lanes' backlog and the busiest lane's busy share, the calc, outbox and sweep ticks, the node
# request paths and the Go runtime. The seeded ingest-lag-high rule raises a warning on the server's
# component when the oldest landing holds over 30 s (#1628, ADR-1628).

Hide
Type `demo_until 150 'omniglass component metric list server | jq -e "[.metrics[] | select(.metric_type_name == \"ingest-lag\" or .metric_type_name == \"bus-consumer-pending\" or .metric_type_name == \"process-goroutine-count\")] | length == 3"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show
Type "# One report window in: the server's pipeline figures on its own component"
Enter
Wait
Type `omniglass component metric list server | jq -c '[.metrics[].metric_type_name | select(test("^(ingest|bus|calc|outbox|sweep|process)-"))] | unique'`
Enter
Wait
Type "# Ingest lag: seconds from a sample's ts to its batch's commit, with the window's throughput"
Enter
Wait
Type `omniglass component metric list server | jq -c '[.metrics[] | select(.metric_type_name | test("^ingest-(lag|lag-max|batch-count|sample-count)$")) | {(.metric_type_name): (.value * 1000 | round / 1000)}] | add'`
Enter
Wait
Type "# The ingest lanes at the window's close: their backlog, and the busiest lane's busy share"
Enter
Wait
Type `omniglass component metric list server | jq -c '[.metrics[] | select(.metric_type_name | test("^bus-consumer-|^ingest-lane-busy$")) | {(.metric_type_name): (.value * 1000 | round / 1000)}] | add'`
Enter
Wait
Type "# The Go runtime, read from runtime/metrics like the node's"
Enter
Wait
Type `omniglass component metric list server | jq -c '[.metrics[] | select(.metric_type_name | test("^process-goroutine-count$|^process-heap-size$")) | {(.metric_type_name): .value}] | add'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# The seeded rule that pages when the server falls behind its fleet"
Enter
Wait
Type `omniglass event-rule get ingest-lag-high | jq -c '{name, sample_key, fire_criteria, fire_sustain, clear_criteria, severity}'`
Enter
Wait
