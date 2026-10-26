# A registry renames, and a product's references clear
#
# A custom vendor and driver, and a product that names both. The product's
# driver clears with an empty string and reads back gone, where it used to
# revert (#260). Then the vendor is renamed: the product holds its uuid, so it
# reads the new name, the old one stops resolving, and an official vendor keeps
# the name it ships with (#555). No uuid is typed.

Type "# A custom vendor and driver, and a product that names both"
Enter
Wait
Type `omniglass vendor create --name acme --label Acme | jq -c '{name, label, kind}'`
Enter
Wait
Type `omniglass driver create --name acme-ctl --label 'Acme Controller' | jq -c '{name, label}'`
Enter
Wait
Type `omniglass product create --name acme-bar --label 'Acme Bar' --kind device --component-type video-bar --vendor-id acme --driver-id acme-ctl | jq -c '{name, vendor, driver}'`
Enter
Wait

Type "# Clear the driver: an empty string clears, an omitted field is left alone"
Enter
Wait
Type `omniglass product update acme-bar --driver-id '' | jq -c '{name, vendor, driver}'`
Enter
Wait
Type `omniglass product get acme-bar | jq -c '{name, vendor, driver}'`
Enter
Wait

Hide
Type "clear"
Enter
Wait
Show
Type "# Rename the vendor: its own act, gated by vendor:rename"
Enter
Wait
Type `omniglass vendor rename acme --name acme-av | jq -c '{name, label}'`
Enter
Wait
Type "# The product holds the vendor's uuid, so it reads the new name"
Enter
Wait
Type `omniglass product get acme-bar | jq -c '{name, vendor}'`
Enter
Wait
Type "# A script that typed the old name stops resolving"
Enter
Wait
Type `omniglass vendor get acme 2>/dev/null | jq -r .detail || true`
Enter
Wait
Type "# An official vendor keeps the name it ships with"
Enter
Wait
Type `omniglass vendor rename kestrel --name kestrel-av 2>/dev/null | jq -r .detail || true`
Enter
Wait
