# syntax=docker/dockerfile:1.8
ARG GO_VERSION=1.24
ARG BUF_VERSION=1.66.0
ARG CODEX_VERSION=0.115.0

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS buf
ARG BUF_VERSION
RUN apk add --no-cache curl
RUN curl -sSL \
      "https://github.com/bufbuild/buf/releases/download/v${BUF_VERSION}/buf-$(uname -s)-$(uname -m)" \
      -o /usr/local/bin/buf && \
    chmod +x /usr/local/bin/buf

FROM --platform=$BUILDPLATFORM alpine:3.21 AS codex
ARG CODEX_VERSION
ARG TARGETARCH
RUN apk add --no-cache ca-certificates curl
RUN case "$TARGETARCH" in \
      amd64) codex_target="x86_64-unknown-linux-musl" ;; \
      arm64) codex_target="aarch64-unknown-linux-musl" ;; \
      *) echo "Unsupported TARGETARCH: $TARGETARCH" >&2; exit 1 ;; \
    esac && \
    curl -sSL \
      "https://github.com/openai/codex/releases/download/rust-v${CODEX_VERSION}/codex-${codex_target}.tar.gz" \
      -o /tmp/codex.tar.gz && \
    tar -xzf /tmp/codex.tar.gz -C /tmp && \
    mv "/tmp/codex-${codex_target}" /usr/local/bin/codex && \
    chmod +x /usr/local/bin/codex

FROM --platform=$BUILDPLATFORM golang:${GO_VERSION}-alpine AS build

WORKDIR /src

COPY --from=buf /usr/local/bin/buf /usr/local/bin/buf

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go mod download

COPY buf.gen.yaml buf.yaml ./
RUN buf generate buf.build/agynio/api \
    --path agynio/api/threads/v1 \
    --path agynio/api/notifications/v1 \
    --path agynio/api/teams/v1

COPY . .

ARG TARGETOS TARGETARCH
ENV CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    go build -trimpath -ldflags "-s -w" -o /out/agynd ./cmd/agynd

FROM alpine:3.21 AS runtime

WORKDIR /app

COPY --from=codex /usr/local/bin/codex /usr/local/bin/codex
COPY --from=build /out/agynd /app/agynd

RUN addgroup -g 10001 -S app && adduser -u 10001 -S app -G app

USER 10001

ENTRYPOINT ["/app/agynd"]
