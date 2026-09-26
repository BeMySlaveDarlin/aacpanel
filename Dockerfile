FROM golang:1.26-alpine AS build
WORKDIR /src

# The Go caches live in cache mounts every build shares, not in the layers: a
# layer would carry its own copy of the build cache, some 250 MB per build,
# and the builder would keep every one of them.
ENV GOCACHE=/root/.cache/go-build GOMODCACHE=/go/pkg/mod

# Dependencies are allowed only on a clean govulncheck. The tool does not
# depend on the sources, so it is installed before them and is not rebuilt
# with every edit. The version is pinned: @latest would make the build
# irreproducible.
ARG GOVULNCHECK_VERSION=v1.7.0
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    go install golang.org/x/vuln/cmd/govulncheck@${GOVULNCHECK_VERSION}

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod go mod download
COPY cmd ./cmd
COPY internal ./internal
COPY deploy ./deploy
COPY web ./web
# The frontend is built by the same toolchain: esbuild is a Go library here,
# so neither node nor npm appear in the image.
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    go run ./cmd/webbuild

# The check runs here so the condition cannot go stale. Only vulnerabilities
# the code actually calls fail the build.
# Emergency exit for a release that cannot wait for a fix. The skip shouts in
# the build log: a silent one is how the check gets lost.
ARG SKIP_VULNCHECK=0
RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    if [ "$SKIP_VULNCHECK" = "1" ]; then         echo "!!!! govulncheck SKIPPED via SKIP_VULNCHECK=1 - the image is built with no dependency check";     else         govulncheck ./...;     fi

RUN --mount=type=cache,target=/root/.cache/go-build --mount=type=cache,target=/go/pkg/mod \
    CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o /out/aacpanel ./cmd/aacpanel

FROM gcr.io/distroless/static-debian12:nonroot
COPY --from=build /out/aacpanel /aacpanel
EXPOSE 8776
USER nonroot:nonroot
ENTRYPOINT ["/aacpanel"]
