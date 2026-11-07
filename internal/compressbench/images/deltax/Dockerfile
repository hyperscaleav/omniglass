# A test-only Postgres 18 with pg_deltax (Apache 2.0) for the compression
# benchmark (#1516). The extension is the official, unmodified release .deb
# from github.com/xataio/deltax, checked against its digest; the license is the
# release tag's own LICENSE, likewise checked. Never shipped, never a product
# image: make compress-bench builds it locally.
FROM postgres:18

ARG DELTAX_VERSION=0.2.1
ARG DELTAX_DEB_SHA256=a910074441ead9efafe865d65a5044150c40074ae4803a9225076bf884449705
ARG DELTAX_LICENSE_SHA256=cfc7749b96f63bd31c3c42b5c471bf756814053e847c10f3eb003417bc523d30

ADD --checksum=sha256:${DELTAX_DEB_SHA256} \
    https://github.com/xataio/deltax/releases/download/v${DELTAX_VERSION}/pg-deltax-pg18_${DELTAX_VERSION}_amd64.deb /tmp/pg-deltax.deb
ADD --checksum=sha256:${DELTAX_LICENSE_SHA256} \
    https://raw.githubusercontent.com/xataio/deltax/v${DELTAX_VERSION}/LICENSE /tmp/LICENSE
# install, not ADD --chmod, places the license: ADD's mode lands on the
# directory it creates too, which leaves the file unreadable to postgres.
RUN apt-get install -y --no-install-recommends /tmp/pg-deltax.deb && rm /tmp/pg-deltax.deb \
 && install -D -m 644 /tmp/LICENSE /usr/share/doc/pg-deltax-pg18/LICENSE && rm /tmp/LICENSE

# pg_deltax registers its maintenance worker from _PG_init, so it must be
# preloaded. The worker services only the postgres database, where the
# benchmark keeps no deltatable, so nothing compresses behind its back.
CMD ["postgres", "-c", "shared_preload_libraries=pg_deltax"]
