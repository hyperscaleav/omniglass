# Retention reports its status as platform metrics
#
# Each retention pass lands what it did as platform metrics the server process writes: per lane the
# rows it removed and whether it stopped behind, the pass's duration and failures, the partitions it
# created and dropped, how far the trend and block marks trail, the oldest metric day's age, and the
# stored tables' sizes. The keys are reserved to Omniglass, so no node, push or operator can write
# them, and a seeded rule alarms on a lane that stays behind (#1629, ADR-1629). The figures land on
# the server's own platform component once it has one (#1671).

Type "# The retention keys join the catalog, written only by the platform"
Enter
Wait
Type `omniglass metric-type list | jq -c '.metric_types[] | select(.name | test("^(retention-|metric-partition-|metric-oldest-|metric-(trend|block)-mark-|table-bytes|database-bytes)")) | {name, unit, writer_class}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# Behind reads 1 for a lane that stopped short of its window, or whose prune failed"
Enter
Wait
Type `omniglass metric-type get retention-behind | jq -c '{name, writer_class, description}'`
Enter
Wait
Type "# A seeded rule opens a warning on a lane held behind for 5 minutes, and clears at 0"
Enter
Wait
Type `omniglass event-rule get retention-behind | jq -c '{name, official, sample_key, fire_criteria, fire_sustain, clear_criteria, severity}'`
Enter
Wait
