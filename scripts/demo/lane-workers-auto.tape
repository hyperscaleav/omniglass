# A node sizes its lane pool from its CPU and memory
#
# --lane-workers defaults to auto: the node reads the CPU and memory its cgroup allows (each held
# to the host's) and sizes its endpoint lane pool to the tighter of the two, weighing
# how long a run waits on its device against the CPU it costs, clamped to 4 to 256. A number still
# sets the pool. The node reports the count it took as lane-worker-count beside its busy share (#1869).

Type "# The pool's knob: a count, or auto, which is the default"
Enter
Wait
Type `omniglass node run --help | grep -- '--lane-workers '`
Enter
Wait
Type "# Anything else is refused at start, naming where it came from"
Enter
Wait
Type `omniglass node run --name demo --token demo --lane-workers lots 2>&1 || true`
Enter
Wait
Type `OMNIGLASS_LANE_WORKERS=0 omniglass node run --name demo --token demo 2>&1 || true`
Enter
Wait

Hide
Type `demo_until 150 'omniglass component metric list edge-hq | jq -e "[.metrics[] | select(.metric_type_name == \"lane-worker-count\" or .metric_type_name == \"worker-busy-fraction\")] | length == 2"'`
Enter
Wait@180s
Type "clear"
Enter
Wait
Show
Type "# The node edge-hq sized its own pool; it reports the count beside how busy the pool was"
Enter
Wait
Type `omniglass component metric list edge-hq | jq -c '.metrics[] | select(.metric_type_name == "lane-worker-count") | {metric: .metric_type_name, pool: .instance, in_clamp: (.value >= 4 and .value <= 256)}'`
Enter
Wait
Type `omniglass component metric list edge-hq | jq -c '.metrics[] | select(.metric_type_name == "worker-busy-fraction") | {metric: .metric_type_name, pool: .instance, level: (.value >= 0 and .value <= 1)}'`
Enter
Wait
