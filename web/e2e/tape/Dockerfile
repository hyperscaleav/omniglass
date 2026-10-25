# The terminal recorder (#1132): VHS, pinned by digest to its own image, which
# bundles what VHS drives (ttyd for the terminal, chromium to render it,
# ffmpeg to encode the GIF and MP4) and the fonts the house settings name.
# Added here: jq, which demos filter the CLI's JSON through (the static binary
# of jq's own image, pinned by digest at the version Debian trixie ships, so
# the build fetches no package index, #1180); the prelude every tape sources
# unseen; and this tree's omniglass binary, copied out of the walkthrough
# stack's image (OG_IMAGE) so the CLI a tape types is the one the stack
# serves. Built and tagged per run by web/e2e/tapes.sh, which pulls the VHS
# image while the stack builds, and untagged at teardown. Both tags publish a
# multi-platform index, so it builds on amd64 and arm64 alike.
ARG OG_IMAGE
FROM ${OG_IMAGE} AS omniglass

FROM ghcr.io/jqlang/jq:1.7.1@sha256:096b83865ad59b5b02841f103f83f45c51318394331bf1995e187ea3be937432 AS jq

FROM ghcr.io/charmbracelet/vhs:v0.12.1@sha256:ea49a6a1c529be83153e88321892b5585964418f0b9055e8c1e0d732194234a3
COPY --from=jq /jq /usr/local/bin/jq
COPY --from=omniglass /omniglass /usr/local/bin/omniglass
COPY prelude.sh /demo/prelude.sh
