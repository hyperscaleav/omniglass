## fixed-testcontainer-port (live)

- **Cue:** a test binding a literal host port; a testcontainer with a fixed port mapping.
- **Failure:** parallel runs and CI collide; the suite is flaky in exactly the way that
  erodes trust in the gate.
- **Fix:** testcontainers-go's random ephemeral ports, address read from the container.
