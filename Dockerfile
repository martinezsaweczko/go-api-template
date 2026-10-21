# syntax=docker/dockerfile:1

# Build:  make docker-build   (or docker build --build-arg VERSION=v1.0.0 -t go-api-template .)
#
# Run (keys are never baked into the image; mount them read-only):
#   docker run --rm -p 8080:8080 \
#     -v "$PWD/keys:/app/keys:ro" \
#     go-api-template:<version>
#
# The JWT key paths default to keys/private.pem and keys/public.pem relative
# to the working directory /app. Point them elsewhere (for example at a
# mounted secret) with APP_JWT_PRIVATE_KEY_PATH and APP_JWT_PUBLIC_KEY_PATH.
#
# There is no HEALTHCHECK instruction: the distroless image has no shell or
# HTTP client. Orchestrators should probe GET /livez (liveness) and
# GET /readyz (readiness) instead.

# Keep in line with the go directive in go.mod and .go-version. Pinned to the
# exact patch: the golang image sets GOTOOLCHAIN=local, so a cached floating
# 1.26 tag older than the go directive would fail the build.
ARG GO_VERSION=1.26.10

FROM golang:${GO_VERSION}-alpine AS builder
WORKDIR /src

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

# Same build metadata as the Makefile LDFLAGS.
ARG VERSION=dev
ARG BUILD_TIME=unknown
ARG APP_NAME=go-api-template
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 go build -trimpath \
      -ldflags "-s -w -X main.version=${VERSION} -X main.buildTime=${BUILD_TIME} -X main.appName=${APP_NAME}" \
      -o /out/api ./cmd/api

FROM gcr.io/distroless/static-debian12:nonroot

WORKDIR /app
COPY --from=builder /out/api /app/api

USER nonroot:nonroot
# Listen on all interfaces inside the container (APP_PORT defaults to 8080).
ENV APP_ADDRESS=0.0.0.0
EXPOSE 8080

ENTRYPOINT ["/app/api"]
