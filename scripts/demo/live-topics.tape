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

Type "# Follow Touch Panel 2's topic for eight seconds, in the background"
Enter
Wait
Type `omniglass view watch --topics component:panel-2 --duration-seconds 8 > watch.txt &`
Enter
Wait
Hide
Type `demo_hush 'sleep 3'`
Enter
Wait
Show
Type "# Its touch surface drops out: an alarm, which recomputes its health"
Enter
Wait
Type `omniglass component alarm create panel-2 --severity high --message 'touch unresponsive' --dedup-key touch | jq -c '{severity, message}'`
Enter
Wait
Type "# The stream named the panel once the write committed: refetch it, nothing more"
Enter
Wait
Type `wait; grep -A1 '^event: changed' watch.txt | sed -n 's/^data: //p' | jq -c '{topic, op}'`
Enter
Wait
