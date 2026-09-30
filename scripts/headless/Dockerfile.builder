# This is a build/test environment, not the deployment image.
ARG GO_IMAGE
FROM ${GO_IMAGE}
COPY scripts/headless/apk.lock /apk.lock
RUN apk add --no-cache $(cat /apk.lock)
ENV GOTOOLCHAIN=local GOOS=linux GOARCH=amd64 CGO_ENABLED=1
RUN go install golang.org/x/vuln/cmd/govulncheck@v1.1.4 \
    && go install github.com/CycloneDX/cyclonedx-gomod/cmd/cyclonedx-gomod@v1.9.0 \
    && git config --system --add safe.directory /src
WORKDIR /src
USER nobody
