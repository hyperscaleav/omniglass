# The lab measures what one driver's polls cost a node
#
# omniglass lab perf drivers brings up a throwaway lab with no fleet and, for each shipped driver,
# a node of its own with the simulator beside it, so the node's cgroup holds only that driver's
# work. It measures the node idle, then at one, two and three times a step of poll tasks, and
# writes the slope of CPU and memory per task to the table the docs render. Here one driver is
# swept once at a small step and short windows; the committed table ran at 1,600 tasks a step
# (#1871).

# demo-needs: docker-socket

Hide
Type `demo_hush 'omniglass lab perf drivers --only ucc-rooms --step-tasks 100 --max-step-tasks 0 --warmup 30s --window 30s --out /tmp/drivers.json >/tmp/drivers.txt 2>/tmp/drivers.err'`
Enter
Wait@600s
Show
Type "# Ran unseen first (about six minutes): omniglass lab perf drivers --only ucc-rooms --step-tasks 100"
Enter
Wait
Type "# Each step: how many tasks the node ran, then its working set and CPU over the window"
Enter
Wait
Type `grep -E '^\[|step:|working set' /tmp/drivers.err`
Enter
Wait
Type "# The slope over the steps is the driver's cost per task"
Enter
Wait
Type `grep -E 'per task|not measured|^wrote' /tmp/drivers.err`
Enter
Wait
Type "# The table the docs render: the host it ran on, and one row per driver"
Enter
Wait
Type `jq -c '{step_tasks, host: (.host | {cores})}' /tmp/drivers.json`
Enter
Wait
Type `jq -c '.rows[] | {driver, tasks, millicores_per_task, cpu_per_run_ms, memory_per_task_bytes}' /tmp/drivers.json`
Enter
Wait
