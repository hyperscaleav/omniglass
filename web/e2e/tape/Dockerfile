# The terminal recorder (#1132): VHS, pinned by digest to its own image, which
# bundles what VHS drives (ttyd for the terminal, chromium to render it,
# ffmpeg to encode the GIF and MP4) and the fonts the house settings name.
# Added here: jq, which demos filter the CLI's JSON through (the static binary
# of jq's own image, pinned by digest at the version Debian trixie ships, so
# the build fetches no package index, #1180); psql, which a tape that declares
# `# demo-needs: database` stages rows with (#1313), copied from the lab's own
# Postgres image (pinned by digest at the lab's major, PostgresImage in
# internal/lab/plan.go) with the shared libraries the VHS image lacks; the
# prelude every tape sources unseen, with the pacing rule it applies (pace.sh,
# #1247); and this tree's omniglass binary, copied out of the lab's image (OG_IMAGE, the omniglass:dev make built) so the CLI a tape types is the
# one the lab serves. Built and tagged per run by web/e2e/tapes.sh, which pulls
# the VHS image while the lab comes up, and untagged at teardown. Both tags publish a
# multi-platform index, so it builds on amd64 and arm64 alike.
ARG OG_IMAGE
FROM ${OG_IMAGE} AS omniglass

FROM ghcr.io/jqlang/jq:1.7.1@sha256:096b83865ad59b5b02841f103f83f45c51318394331bf1995e187ea3be937432 AS jq

FROM postgres:18@sha256:b9f6b8ff03b62f3cfd9d9ba3fe56226a9991c79d3fb46fd46536263bf02be553 AS psql
# psql and every library it links except glibc's (the VHS image is the same
# Debian release, so its own glibc serves), gathered so the final image takes
# them whole, whatever the architecture's library path.
RUN set -eu; mkdir -p /psql; cp /usr/lib/postgresql/18/bin/psql /psql/psql; \
    ldd /psql/psql | awk '/=> \//{print $3}' | grep -Ev '/(libc|libm|libresolv|libdl|libpthread)\.so' | xargs -I{} cp -L {} /psql/

FROM ghcr.io/charmbracelet/vhs:v0.12.1@sha256:ea49a6a1c529be83153e88321892b5585964418f0b9055e8c1e0d732194234a3
COPY --from=jq /jq /usr/local/bin/jq
COPY --from=omniglass /omniglass /usr/local/bin/omniglass
COPY --from=psql /psql /usr/local/lib/psql
COPY psql.sh /usr/local/bin/psql
COPY prelude.sh pace.sh /demo/
