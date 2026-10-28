## zoned-address-skips-prefix-check (fixed)

- **Cue:** `netip.Prefix.Contains`, or `==` against a fixed address, used for a security decision
  on an address that can carry an IPv6 zone (`fe80::1%eth0`), or that embeds an IPv4 destination
  (NAT64 `64:ff9b::/96`, 6to4, Teredo), without normalizing it first.
- **Failure:** `Prefix.Contains` is false for any zoned address and `==` misses one, so
  `::1%lo` passes a loopback check and `fd00:ec2::254%eth0` a metadata check; a translated address
  is judged public though it reaches a private IPv4 one. Proven in the #1320 review: the egress
  client connected and sent a ClientHello to `[::1%lo]` under an empty allow list.
- **Fix:** refuse a zoned address outright and judge the normalized address (zone dropped,
  IPv4-mapped unmapped, NAT64 reduced to its IPv4), as `egress.CheckAddr` does, with a real-socket
  test dialing the zoned loopback.
