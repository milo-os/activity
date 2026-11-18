# Build stage
FROM --platform=$BUILDPLATFORM golang:1.27-alpine AS builder

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

RUN CGO_ENABLED=0 GOOS=${TARGETOS} GOARCH=${TARGETARCH} go build \
    -ldflags="-s -w -X 'go.miloapis.com/activity/internal/version.Version=${VERSION}' \
              -X 'go.miloapis.com/activity/internal/version.GitCommit=${GIT_COMMIT}' \
              -X 'go.miloapis.com/activity/internal/version.GitTreeState=${GIT_TREE_STATE}' \
              -X 'go.miloapis.com/activity/internal/version.BuildDate=${BUILD_DATE}'" \
    -o activitytool ./cmd/activitytool

# Runtime stage
FROM gcr.io/distroless/static:nonroot

WORKDIR /work
COPY --from=builder /workspace/activitytool /activitytool
USER 65532:65532

ENTRYPOINT ["/activitytool"]
