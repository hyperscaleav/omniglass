# sim: the Omniglass device simulator

The simulator serves a fleet of fictional AV and IT devices, each on its own loopback address,
speaking the protocols real gear speaks (HTTP and HTTPS, SNMP, SSH, Telnet, raw TCP line
protocols, UDP, WebSocket, server-sent events, signed webhooks) from one live state per device.
An outpost collects from a simulated device exactly as it would from hardware. It is the test
bed for the end-to-end tests, the screenshot harness and the lab, and a teaching surface: an
operator can break a device on purpose and watch the monitoring loop respond.

It is part of the single Omniglass Go module and binary, not a separate program.

## Layout

| Path | What it is |
| --- | --- |
| `sim/` | The engine: devices, faces per protocol, behavior, faults, state, observation. |
| `sim/profiles/` | Device profiles as data (`devices/`, `surfaces/`, `templates/`), embedded into the binary. |
| `sim/surface/` | Protocol surfaces a Go device family declares in Go (`nvp`, `snmp`, `httpapi`), byte for byte what the YAML surfaces and repeated profile pieces answer. |
| `sim/profile/` | The profile model: parse, validate, resolve includes. |
| `sim/fleet/` | Deterministic naming and enrichment of fleet members. |
| `sim/impair/` | Network impairment (firewall table and qdisc) for a simulated device. |
| `sim/simfleet/` | Fleet and system plans built from profiles. |
| `sim/simhub/` | Registry of the simulators connected to a server. |
| `sim/simoutpost/` | The simulator running beside or inside an outpost. |
| `sim/mailsim/` | The simulated mail server. |
| `sim/simtest/` | Test helpers for loopback addressing. |

## Running it

- `omniglass sim run` runs beside an outpost with that outpost's credential (the same
  `OMNIGLASS_SERVER`, `OMNIGLASS_OUTPOST_NAME` and `OMNIGLASS_OUTPOST_TOKEN` as `outpost run`),
  pulls the devices placed on that outpost and serves them. `omniglass sim mail` runs the
  simulated mail server.
- `make lab` brings up a server, an outpost and a simulated fleet together;
  `make test-lab LAB_RUN=<test>` runs one lab scenario test.
- `make bench-sim` serves a 1,000-device fleet and reports startup time.

## Writing a device profile

A profile is one YAML file in `sim/profiles/devices/`, named for the product it simulates. It
declares the product's identity (`name`, `manufacturer`, `model`, `device_type`), its interfaces
and protocol surfaces, its state, and the behavior that moves that state. Surface pieces two
profiles share live in `sim/profiles/surfaces/` and are taken in with `include:`. The editor
schema is `sim/profiles/devices/schema.json`; the first line of a profile points at it. Start
from the closest existing profile and copy its shape. Coverage of seeded products by profiles is
accounted for in `sim/profiles/coverage.yaml` and checked by the tests.

## Where the docs live

The architecture page is [Simulation](../docs/src/content/docs/architecture/simulation.mdx), the
operator walk-through is the simulation guide under `docs/src/content/docs/operator/`, and the
lab is described under `docs/src/content/docs/developer/`.
