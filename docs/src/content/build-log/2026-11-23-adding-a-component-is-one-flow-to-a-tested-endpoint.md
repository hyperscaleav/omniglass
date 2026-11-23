---
title: "Adding a component is one flow, from its product to a tested endpoint (#2460)"
date: 2026-11-23
refs: [2460, 2427, 2289, 878]
---

Adding a device took about twelve clicks over three pages and a drawer, an unfiltered list of every
driver, a secret typed by name, and an address read off the Simulation page, with no test at the
end. **Add a component** (`/components/add`) is now one page of six steps: the product (a filterable
picker naming each product's type and vendor) and its name; the system, the slot and the location (taken
from the system, since the server does not inherit it yet, #878); the endpoint, with the product's
own driver chosen and its type's other drivers listed next; the credentials, picked from the secrets
of the kind each input takes, with the secret form inline to create a missing one; the poll test of
#2427, which runs at once and sends a failure back to the step that fixes it; and the component's
Overview with the values the test read.

Nothing is created until **Create and test**, and Cancel, Back from the test, or leaving the page
take the component and its endpoint back out. In a lab the endpoint step fills itself from a new
read-only route, `GET /outposts/{name}/simulation/components:plan`, which previews the address and
the driver attach a new simulated device of the product would get, shared with the add route's own
planning. Explore's New menu, a system's Components and the Simulation page open the flow; the
plain create form stays behind its "advanced" link. The driver inputs render through one
`DriverInputFields` shared with the new-endpoint form, and `SecretPicker` is the first of the
reference pickers #2289 asks for.
