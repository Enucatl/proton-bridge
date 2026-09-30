#!/bin/sh
# Reviewed Linux amd64 build/runtime inputs. Update these pins together.
# shellcheck disable=SC2034
BUILDER_IMAGE=proton-bridge-headless-builder:go1.26.7
GO_IMAGE=golang:1.26.7-alpine@sha256:bf9573d7c1d2b09992e4f893ea1ef30842854846bdb8ae390468f95ea6b09062
DISTROLESS_IMAGE=gcr.io/distroless/static-debian13:nonroot@sha256:e2e927ec666bae08560abb3c55d0659eceabb657f56b6782ab500a9fc7f555e3
TRIVY_IMAGE=aquasec/trivy:0.74.0@sha256:ee940acbf1f58ebadb42d01434ce4609530bf1b52536afbd1eee66cd7123c5c9
UPSTREAM_VERSION=3.27.1
