# Sample a running scale lab into a time series
#
# omniglass lab measure --every samples the scale lab already running on an interval instead of
# bringing one up: each sample is one JSON line appended to a file, carrying the server's uptime,
# the window's ingest lag, the fleet view and the system page timings under concurrent clients, and
# the absolutes that grow with time (the database's bytes, the metric partitions, dead tuples). A
# soak reads as a curve, and a server that restarted shows as uptime going back to zero (#1632).

# demo-needs: docker-socket

Hide
Type `demo_hush 'omniglass lab run --scenario corporate --systems 20 --port 0 >/tmp/scale.log 2>&1 & scale=$!'`
Enter
Wait
Type `demo_until 170 'grep -q "lab is ready" /tmp/scale.log'`
Enter
Wait@180s
Show

Type "# A corporate scale lab of 20 systems is up; sample it three times, each over one 30 second collection cycle"
Enter
Wait
Hide
Type `demo_hush 'omniglass lab measure --every 30s --window 30s --warmup 10s --samples 3 --page-clients 2 --page-loads 5 --append series.jsonl >/tmp/series.txt 2>/tmp/series.err'`
Enter
Wait@480s
Show
Type "# Sampled unseen: omniglass lab measure --every 30s --window 30s --samples 3 --append series.jsonl"
Enter
Wait
Type "# One JSON line a sample: the server's uptime climbs, and the lag and page timings sit beside it"
Enter
Wait
Type `jq -c '{seq, uptime_s: (.uptime_seconds | floor), error, lag_p95_ms: (.ingest_lag_ms.p95 | round), system_page_p95_ms: (.system_page_ms.p95 | round), page_error_ratio: .system_page_error_ratio}' series.jsonl`
Enter
Wait
Type "# And the absolutes a soak watches grow"
Enter
Wait
Type `jq -c '{seq, database_bytes: .absolutes.database_bytes, metric_partitions: .absolutes.metric_partitions, dead_tuples: .absolutes.dead_tuples}' series.jsonl`
Enter
Wait

Hide
Type `demo_hush 'kill -INT "$scale"; wait "$scale"'`
Enter
Wait@90s
Show
