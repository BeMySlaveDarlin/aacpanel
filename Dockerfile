FROM golang:1.26-alpine AS build
WORKDIR /src
COPY go.mod go.sum ./
# Modules as their own layer: editing sources does not pull them again.
RUN go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY deploy ./deploy
COPY web ./web
# The frontend is built by the same toolchain: esbuild is a Go library here,
# so neither node nor npm appear in the image.
RUN go run ./cmd/webbuild

# Dependencies are allowed only on a clean govulncheck, and it runs here so
# the condition cannot go stale. Only vulnerabilities the code actually calls
# fail the build. The version is pinned: @latest would make the build
# irreproducible.
ARG GOVULNCHECK_VERSION=v1.7.0
RUN go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}
# Emergency exit for a release that cannot wait for a fix. The skip shouts in
# the build log: a silent one is how the check gets lost.
ARG SKIP_VULNCHECK=0
RUN if [ "$SKIP_VULNCHECK" = "1" ]; then         echo "!!!! govulncheck SKIPPED via SKIP_VULNCHECK=1 - the image is built with no dependency check";     else         govulncheck ./...;     fi

RUN CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/aacpanel ./cmd/aacpanel

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/aacpanel /aacpanel
EXPOSE 8776
USER nonroot:nonroot
ENTRYPOINT ["/aacpanel"]
