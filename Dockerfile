# syntax=docker/dockerfile:1.7@sha256:a57df69d0ea827fb7266491f2813635de6f17269be881f696fbfdf2d83dda33e

FROM --platform=$BUILDPLATFORM docker.io/library/golang:1.26.6-bookworm@sha256:116d58cbd88c1297624acc6e967a060012422bacf9930927e23fb719189c6f36 AS build

WORKDIR /src

ARG TARGETOS
ARG TARGETARCH
ARG VERSION=0.0.0-dev
ARG COMMIT=unknown
ARG BUILD_DATE=1970-01-01T00:00:00Z
ARG DIRTY=false
ARG SOURCE_DATE_EPOCH=0
ARG CGO_ENABLED=0
ARG GO_BUILD_TAGS=

COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .
RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    if [ -n "$GO_BUILD_TAGS" ]; then set -- -tags "$GO_BUILD_TAGS"; else set --; fi; \
    CGO_ENABLED=$CGO_ENABLED GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -buildvcs=false "$@" \
      -ldflags="-s -w -X github.com/dc-tec/openbao-kubernetes-kms/internal/version.version=$VERSION -X github.com/dc-tec/openbao-kubernetes-kms/internal/version.commit=$COMMIT -X github.com/dc-tec/openbao-kubernetes-kms/internal/version.buildDate=$BUILD_DATE -X github.com/dc-tec/openbao-kubernetes-kms/internal/version.dirty=$DIRTY" \
      -o /out/bao-kms-provider ./cmd/bao-kms-provider

FROM gcr.io/distroless/static-debian12:nonroot@sha256:a9329520abc449e3b14d5bc3a6ffae065bdde0f02667fa10880c49b35c109fd1

LABEL org.opencontainers.image.title="bao-kms-provider" \
      org.opencontainers.image.description="OpenBao-native Kubernetes KMS v2 provider" \
      org.opencontainers.image.source="https://github.com/dc-tec/openbao-kubernetes-kms"

COPY --from=build /out/bao-kms-provider /bao-kms-provider

USER 65532:65532
ENTRYPOINT ["/bao-kms-provider"]
