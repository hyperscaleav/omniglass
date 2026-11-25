# Lifecycle fields are set once on a product and overridden per component
#
# A product carries the lifecycle defaults every one of its components shares: when it reaches end
# of life, what replacing it costs and how long it is expected to last. A new display of that product
# reads them as its own effective values with nothing set on it. One display was installed on a known
# day, so it declares its own install date; a date that is not a date is refused (#2366).
#
# The product is a custom one made off camera: a shipped (official) product's defaults are
# read-only, so the demo cannot set them on one.

Hide
Type `demo_hush 'omniglass product create --name lobby-display --label "Lobby display" --component-type display --kind device'`
Enter
Wait
Show

Type "# The product sets its lifecycle defaults once"
Enter
Wait
Type `omniglass product property update lobby-display end-of-life --default-value 2031-06-30 | jq -c '{property_type_name, default_value}'`
Enter
Wait
Type `omniglass product metric update lobby-display replacement-cost --default-value 4200 | jq -c '{metric_type_name, default_value}'`
Enter
Wait
Type `omniglass product metric update lobby-display expected-life-years --default-value 7 | jq -c '{metric_type_name, default_value}'`
Enter
Wait
Type "# A new display of that product inherits them, with nothing set on it"
Enter
Wait
Type `c=$(omniglass component create --name lobby-display-1 --product lobby-display --location hq.west | jq -r .name)`
Enter
Wait
Type `omniglass component property list "$c" | jq -c '.properties[] | select(.property_type_name == "end-of-life") | {property_type_name, value, is_set}'`
Enter
Wait
Type `omniglass component metric list "$c" | jq -c '.metrics[] | select(.metric_type_name | test("replacement-cost|expected-life-years")) | {metric_type_name, value}'`
Enter
Wait
Type "# This one was installed on a known day, so it declares its own install date"
Enter
Wait
Type `omniglass component property update "$c" install-date --value 2024-08-15 | jq -c '{property_type_name, value}'`
Enter
Wait
Type "# A date that is not a date is refused"
Enter
Wait
Type `omniglass component property update "$c" warranty-end --value 'next spring' 2>/dev/null | jq -r .detail || true`
Enter
Wait
