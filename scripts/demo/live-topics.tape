# A watch names the entity a write changed
#
# The console's one live stream carries a changed notice for each topic a page follows, so the page
# refetches only what moved and never polls (#2731). Here a watch follows Touch Panel 2 for a few
# seconds while an alarm is raised on it: the alarm recomputes the panel's health, and once that
# commits the stream names the topic. A notice carries no data, only the topic, the entity's id and
# what happened; the reader refetches the route it already reads.

Hide
Type "clear"
Enter
Wait
Show

# One line holds the watch and the write, so no hold the recorder puts between typed lines (the
# prompt returns only after a line's output has had its read time, #1247) can outrun the watch. The
# CLI prints a stream when it ends, so the watch file is read after `wait`.
Type "# Follow Touch Panel 2's topic, then drop its touch surface out: an alarm, which recomputes its health"
Enter
Wait
Type `omniglass view watch --topics component:panel-2 --duration-seconds 6 > watch.txt & sleep 2; omniglass component alarm create panel-2 --severity high --message 'touch unresponsive' --dedup-key touch | jq -c '{severity, message}'; wait`
Enter
Wait
Type "# The stream named the panel once the write committed: refetch it, nothing more"
Enter
Wait
Type `grep -A1 '^event: changed' watch.txt | sed -n 's/^data: //p' | jq -c '{topic, op}'`
Enter
Wait
