# Base images are pinned by digest. Renovate's dockerfile manager updates the
# tag and digest together; the repository's other Dockerfiles don't pin yet.

# Build stage
FROM --platform=$BUILDPLATFORM golang:1.27-alpine@sha256:70bd7ddea8b6cbe5d6943cde279d6b67feb10cc924fb9100d1d8757c1575b280 AS builder

ARG TARGETOS=linux
ARG TARGETARCH=amd64
ARG VERSION=dev
ARG GIT_COMMIT=unknown
ARG GIT_TREE_STATE=unknown
ARG BUILD_DATE=unknown

WORKDIR /workspace

COPY go.mod go.mod
COPY go.sum go.sum
RUN go mod download

COPY cmd/ cmd/
COPY pkg/ pkg/
COPY internal/ internal/

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build -trimpath \
    -ldflags="-s -w -X 'go.miloapis.com/activity/internal/version.Version=${VERSION}' \
              -X 'go.miloapis.com/activity/internal/version.GitCommit=${GIT_COMMIT}' \
              -X 'go.miloapis.com/activity/internal/version.GitTreeState=${GIT_TREE_STATE}' \
              -X 'go.miloapis.com/activity/internal/version.BuildDate=${BUILD_DATE}'" \
    -o activitytool ./cmd/activitytool

# Runtime stage: the :nonroot image already runs as nonroot (65532:65532).
# Consumers mount their repository read-only at /work and run with
# --network none --read-only; the tool needs neither network nor writes.
FROM gcr.io/distroless/static-debian12:nonroot@sha256:afa5c872c891853ca7fcf1f12c3edb23f7eeef36189728842dd51042ff57f7ab

WORKDIR /work
COPY --from=builder /workspace/activitytool /activitytool

ENTRYPOINT ["/activitytool"]
