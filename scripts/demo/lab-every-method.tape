# Every collection method runs healthy in the lab
#
# The lab runs every way omniglass collects, each healthy on a component that is no planted fault,
# on both tiers: its built-in data (it reads up, or a pushed endpoint has been heard) and the values
# its driver reads from the device. The HQ boardroom holds the methods the rest of the fleet does not
# use; the simulated meeting service is subscribed to its webhook endpoint from the start; and a
# simulated booking service pushes the boardroom system's bookings through the API, as a service
# that may push to that one system and do nothing else (#1370).

Type "# The HQ boardroom's extra endpoints: the methods the rest of the fleet never attaches"
Enter
Wait
Type `omniglass endpoint list | jq -c '.endpoints[] | select(.component_id != null and (.label // "" | test("^(Bar control socket|Display web admin|Panel web|Panel shell|Panel Telnet|Panel ping)$"))) | {component, transport, driver, label}'`
Enter
Wait
Type `panel='hq.west.level-2.boardroom-a.$comp.panel-1'`
Enter
Wait
Type "# Each reads up: built in, whatever its driver"
Enter
Wait
Type `omniglass component reachability list "$panel" | jq -c '.endpoints[] | select(.label != null) | {label, transport, verdict: .verdict.value}'`
Enter
Wait
Type "# And each driver's own values land, under the endpoint they came through"
Enter
Wait
Type `bar='hq.west.level-2.boardroom-a.$comp.videobar-1'`
Enter
Wait
Type `omniglass component metric list "$bar" | jq -c '.metrics[] | select(.metric_type_name == "volume") | {metric: .metric_type_name, endpoints: [.instances[].instance] | sort}'`
Enter
Wait
Type `omniglass component property sample list "$panel" model-number | jq -c '[.samples[] | .instance] | unique'`
Enter
Wait

Type "# The meeting service was subscribed to its webhook endpoint when the lab came up, and has posted"
Enter
Wait
Type `ucc='hq.$comp.service-1'`
Enter
Wait
Hide
Type `demo_until 150 'omniglass component reachability list "$ucc" | jq -e ".endpoints[] | select(.transport == \"webhook\") | .last_seen != null"'`
Enter
Wait@160s
Type "clear"
Enter
Wait
Show
Type `omniglass component reachability list "$ucc" | jq -c '.endpoints[] | select(.transport == "webhook") | {label, transport, heard: (.last_seen != null)}'`
Enter
Wait

Type "# A booking service pushes the boardroom system's bookings through POST /telemetry:push"
Enter
Wait
Type `omniglass principal list | jq -c '.principals[] | select(.service.name == "sim-booking-service") | {service: .service.name, grants: [.grants[] | {role, scope_kind, scope_op}]}'`
Enter
Wait
Type `sys='hq.west.level-2.boardroom-a.$sys.boardroom'`
Enter
Wait
Hide
Type `demo_until 90 'omniglass system metric list "$sys" | jq -e "[.metrics[] | select(.metric_type_name == \"booking-count\" and .is_sampled)] | length == 1"'`
Enter
Wait@100s
Type "clear"
Enter
Wait
Show
Type `omniglass system metric list "$sys" | jq -c '.metrics[] | select(.metric_type_name == "booking-count") | {metric: .metric_type_name, sampled: .is_sampled}'`
Enter
Wait
Type `omniglass system property list "$sys" | jq -c '.properties[] | select(.property_type_name == "booking-state") | {property: .property_type_name, value: .reconciliation.is}'`
Enter
Wait
