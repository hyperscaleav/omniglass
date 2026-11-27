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

# The CLI prints a stream only when it ends, so the visible watch cannot be read while it runs. A
# hidden probe follows the same topic into its own file: its first `mark` says the stream is
# connected (the alarm waits for it), and its `changed` says the notice landed (the read waits for
# it). The watch runs longer than the holds the recorder puts between lines (#1247) can sum to.
Type "# Follow Touch Panel 2's topic in the background"
Enter
Wait
Type `omniglass view watch --topics component:panel-2 --duration-seconds 20 > watch.txt &`
Enter
Wait
Hide
Type `demo_hush 'curl -sN --max-time 20 -H "Authorization: Bearer $OMNIGLASS_TOKEN" "$OMNIGLASS_SERVER/api/v1/views:watch?topics=component:panel-2&duration_seconds=20" > probe.txt &'`
Enter
Wait
Type `demo_until 20 'grep -q "^event: mark" probe.txt'`
Enter
Wait
Show
Type "# Its touch surface drops out: an alarm, which recomputes its health"
Enter
Wait
Type `omniglass component alarm create panel-2 --severity high --message 'touch unresponsive' --dedup-key touch | jq -c '{severity, message}'`
Enter
Wait
Hide
Type `demo_until 20 'grep -q "^event: changed" probe.txt'`
Enter
Wait
Type `demo_hush 'kill %2; wait %1'`
Enter
Wait
Show
Type "# The stream named the panel once the write committed: refetch it, nothing more"
Enter
Wait
Type `grep -A1 '^event: changed' watch.txt | sed -n 's/^data: //p' | jq -c '{topic, op}'`
Enter
Wait
