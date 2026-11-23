# A location create cannot probe for a location it cannot read
#
# POST /locations resolves its parent under the caller's location:read scope
# before its location:create scope (#1343). The East field tech, granted deploy
# under the East Campus only, gets the same 404 for a parent that exists outside
# their read (the West Building) as for one that exists nowhere, byte for byte,
# and creates under a location inside their grant.

Hide
Type `demo_hush 'export OMNIGLASS_TOKEN=$(omniglass user impersonate tech-east --mode act_as | jq -r .token)'`
Enter
Wait
Show

Type "# Acting as tech-east: deploy under the East Campus, nothing in the West"
Enter
Wait
Type `omniglass auth me | jq -c '{user: .person.username}'`
Enter
Wait
Type "# A parent that exists, but outside what tech-east can read"
Enter
Wait
Type `omniglass location create --name probe --location-type floor --parent hq.west 2>/dev/null | jq -c '{status, detail}' || true`
Enter
Wait
Type "# A parent that exists nowhere: the same answer, so nothing is disclosed"
Enter
Wait
Type `omniglass location create --name probe --location-type floor --parent hq.nowhere 2>/dev/null | jq -c '{status, detail}' || true`
Enter
Wait
Type "# Inside the grant, the create is served"
Enter
Wait
Type `omniglass location create --name level-9 --location-type floor --parent east.hall | jq -c '{name, parent}'`
Enter
Wait
