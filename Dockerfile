FROM --platform=$BUILDPLATFORM golang:1.23-alpine AS build
ARG TARGETOS=linux
ARG TARGETARCH=arm64
WORKDIR /src
COPY . .
# tidy is a no-op once go.sum is committed
RUN go mod tidy && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/poller ./cmd/poller && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/loader ./cmd/loader && \
    CGO_ENABLED=0 GOOS=$TARGETOS GOARCH=$TARGETARCH go build -trimpath -ldflags="-s -w" -o /out/feed_check ./scripts/feed_check

FROM alpine:3.20
RUN apk add --no-cache ca-certificates tzdata postgresql16-client
COPY --from=build /out/ /app/
COPY sql /app/sql
USER 65534
CMD ["/app/poller"]
