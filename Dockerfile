# neofreebox — multi-arch image (linux/amd64, linux/arm64, linux/arm/v7).
# Pure Go, CGO_ENABLED=0: cross-compiled on the build platform, nothing of
# the target architecture runs during the build.

# Base images pinned by digest; Dependabot bumps tag and digest together.
FROM --platform=$BUILDPLATFORM golang:1.27.4-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS build
ARG TARGETOS TARGETARCH VERSION=dev REVISION=
WORKDIR /src

COPY go.mod go.sum ./
RUN go mod download

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY web/ ./web/
# A :dev build gets the commit in its version, which stamps asset URLs (?v=).
RUN V="$VERSION"; [ "$V" = dev ] && [ -n "$REVISION" ] && V="dev-$(echo "$REVISION" | cut -c1-7)"; \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath \
      -ldflags="-s -w -X main.version=${V}" -o /out/neofreebox ./cmd/neofreebox \
    && mkdir -p /out/data

# distroless/static: CA bundle and time zones, no shell, non-root (UID 65532).
FROM gcr.io/distroless/static-debian13:nonroot@sha256:aafe993665bf6200db38a02c11a9351e782a84b6a45e7f33b6695638c88e3931
COPY --from=build /out/neofreebox /neofreebox
COPY --from=build --chown=nonroot:nonroot /out/data /data
ENV NEOFREEBOX_ADDR=:8080 \
    NEOFREEBOX_DATA_DIR=/data \
    TZ=Europe/Paris
VOLUME /data
EXPOSE 8080
USER nonroot
HEALTHCHECK --interval=30s --timeout=5s --start-period=10s CMD ["/neofreebox", "healthcheck"]
ENTRYPOINT ["/neofreebox"]
CMD ["serve"]
