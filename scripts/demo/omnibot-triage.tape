# omnibot triages an alarm
#
# A run that names the triage-alarm skill carries out the skill the server
# ships: omnibot reads the component, its alarms and its reachability, then
# says where the fault is and what it would do next. The lab's server plays a
# recorded model that answers a triage run with a triage, so the run is the
# same every time (#2163).

Type "# Raise an alarm on the meeting 405 video bar"
Enter
Wait
Type `omniglass component alarm create 'hq.west.level-4.meeting-405.$comp.videobar-1' --severity high --message 'Video feed lost' --dedup-key demo-triage | jq -c '{severity, message}'`
Enter
Wait

Type "# Ask omnibot to triage it, with the skill the server ships"
Enter
Wait
Type `omniglass assistant conversation create --name triage | jq -c '{name}'`
Enter
Wait
Type `omniglass assistant conversation run triage --messages '[]' --forwardedProps '{"skill":{"name":"triage-alarm","arguments":{"component":"hq.west.level-4.meeting-405.$comp.videobar-1","alarm":"Video feed lost"}}}' | jq -Rc 'ltrimstr("data: ") | fromjson? | select(.type == "TOOL_CALL_START" or .type == "TEXT_MESSAGE_CONTENT") | .toolCallName // .delta'`
Enter
Wait
