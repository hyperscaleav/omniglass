---
title: "go.mod is the only place the Go version is written (#2596)"
date: 2026-11-24
refs: [2596, 2421]
---

The Dockerfile pinned `golang:1.26.0-bookworm` while go.mod carried its own copy, and the golang image
defaults to `GOTOOLCHAIN=local`, so a go.mod bump failed the image build. The Dockerfile now declares
`ARG GO_VERSION` with no default and builds `FROM golang:${GO_VERSION}-bookworm`; the Makefile derives it
with `scripts/go-version.sh` (the `toolchain` directive, else `go`) and passes it to `make` and
`make image`. `scripts/check-go-pin.sh` is the drift check (a hard-coded golang image, or a docker build
without `GO_VERSION`); `make preflight` runs it. Tests: `scripts/check_go_pin_test.go`.

Left for a maintainer, since an agent token cannot edit `.github/workflows`: pass `GO_VERSION` (read from
go.mod, e.g. `scripts/go-version.sh`) in `build-args` of the three `docker/build-push-action` steps in
`image.yml` and `release.yml`, then run the check with `--workflows` in CI. Until then those image builds
fail loudly on the empty `GO_VERSION`.
