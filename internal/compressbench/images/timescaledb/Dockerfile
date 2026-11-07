# A test-only Postgres 18 with TimescaleDB Community Edition (the TSL build,
# which carries the columnstore) for the compression benchmark (#1516). The
# extension is the official, unmodified package from Timescale's signed apt
# repository, pinned to one release built against this Postgres; the licenses
# are the release tag's LICENSE and tsl/LICENSE-TIMESCALE, checked against
# their digests. Never shipped, never a product image: make compress-bench
# builds it locally.
FROM postgres:18

ARG TIMESCALEDB_VERSION=2.31.0
ARG TIMESCALEDB_LICENSE_SHA256=0378e0948feefd85f579319c74d6e2b671194037f550c7176ef26649d94c895b
ARG TIMESCALEDB_TSL_LICENSE_SHA256=8691e074dcdbc2e5be2f0f888d44a65497d73f3813fbb6bf1411ce26ce907e6d

RUN apt-get update \
 && apt-get install -y --no-install-recommends curl gnupg ca-certificates \
 && curl -fsSL https://packagecloud.io/timescale/timescaledb/gpgkey | gpg --dearmor -o /usr/share/keyrings/timescale.gpg \
 && echo "deb [signed-by=/usr/share/keyrings/timescale.gpg] https://packagecloud.io/timescale/timescaledb/debian/ $(. /etc/os-release && echo $VERSION_CODENAME) main" \
      > /etc/apt/sources.list.d/timescale.list \
 && apt-get update \
 && pkg=$(apt-cache madison timescaledb-2-postgresql-18 | awk -v v="${TIMESCALEDB_VERSION}~" 'index($3, v) == 1 { print $3; exit }') \
 && test -n "$pkg" \
 && apt-get install -y --no-install-recommends "timescaledb-2-postgresql-18=$pkg" "timescaledb-2-loader-postgresql-18=$pkg" \
 && apt-get purge -y curl gnupg && apt-get autoremove -y && rm -rf /var/lib/apt/lists/*
ADD --checksum=sha256:${TIMESCALEDB_LICENSE_SHA256} \
    https://raw.githubusercontent.com/timescale/timescaledb/${TIMESCALEDB_VERSION}/LICENSE /tmp/LICENSE
ADD --checksum=sha256:${TIMESCALEDB_TSL_LICENSE_SHA256} \
    https://raw.githubusercontent.com/timescale/timescaledb/${TIMESCALEDB_VERSION}/tsl/LICENSE-TIMESCALE /tmp/LICENSE-TIMESCALE
# install, not ADD --chmod, places the licenses: ADD's mode lands on the
# directory it creates too, which leaves the files unreadable to postgres.
RUN install -D -m 644 -t /usr/share/doc/timescaledb /tmp/LICENSE /tmp/LICENSE-TIMESCALE && rm /tmp/LICENSE /tmp/LICENSE-TIMESCALE

CMD ["postgres", "-c", "shared_preload_libraries=timescaledb", "-c", "timescaledb.telemetry_level=off"]
