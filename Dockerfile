FROM golang:1.27.3-trixie@sha256:1896654cfc5c2b5ad74c07f29cb36f5d2d1cbed42e3054070fbfe444e521e83c AS builder

RUN apt-get update && apt-get upgrade -y && rm -rf /var/lib/apt/lists/*
ENV GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 CGO_ENABLED=1
RUN go install golang.org/x/vuln/cmd/govulncheck@v1.8.0 \
    && git config --system --add safe.directory /src
WORKDIR /src

FROM builder AS build
COPY . .
ARG REVISION=unknown
RUN ./utils/headless/build.sh

FROM build AS probe
RUN go build -mod=readonly -tags=container,netgo,osusergo,sqlite_omit_load_extension \
    -trimpath -buildvcs=false -ldflags '-s -w' \
    -o headless-dist/runtime-probe ./utils/headless/probe.go

FROM gcr.io/distroless/base-nossl-debian13:nonroot@sha256:8c563c1fb5e120606f0d85733049775faed6192e2bd2223ef283a5393eec22b9 AS debian
FROM debian AS base

LABEL org.opencontainers.image.source="https://github.com/Enucatl/proton-bridge"

COPY --from=debian --chown=1000:1000 --chmod=0700 /home/nonroot /data
COPY --from=build --chmod=0755 /src/headless-dist/proton-bridge-headless /proton-bridge-headless
COPY --from=build /src/LICENSE /usr/share/doc/proton-bridge/LICENSE

ENV HOME=/data TMPDIR=/tmp
USER 1000:1000
WORKDIR /data
VOLUME ["/data"]
EXPOSE 1143/tcp 1025/tcp

HEALTHCHECK --interval=30s --timeout=15s --start-period=30s --retries=3 \
    CMD ["/proton-bridge-headless", "--healthcheck"]

ENTRYPOINT ["/proton-bridge-headless"]
CMD ["--noninteractive"]

FROM base AS smoke
COPY --from=probe /src/headless-dist/runtime-probe /runtime-probe

FROM base AS runtime
