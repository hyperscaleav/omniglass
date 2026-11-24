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

CI does the same: the arm64 smoke and multi-arch steps in `image.yml` and the release image step in
`release.yml` pass `GO_VERSION` from `scripts/go-version.sh` in `build-args`, and the arm64 smoke job runs
`scripts/check-go-pin.sh --workflows`. The `ARG` sits above the first `FROM`, since a `FROM` line only
sees global build args; the check fails on one declared after it.
