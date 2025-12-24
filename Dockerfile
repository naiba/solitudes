# Build stage with dependency caching
FROM golang:alpine AS builder

# Install build dependencies
RUN apk --no-cache --no-progress add --virtual build-deps build-base git linux-pam-dev

WORKDIR /build

# Copy go mod files first for better caching
COPY go.mod go.sum ./
RUN go mod download

# Copy source code
COPY . .

# Build binary with optimizations
RUN CGO_ENABLED=true go build -o solitudes \
    -ldflags="-s -w -X github.com/naiba/solitudes.BuildVersion=`git rev-parse HEAD 2>/dev/null || echo 'unknown'`" \
    cmd/web/main.go

# Final stage
FROM alpine:latest

# Install runtime dependencies
RUN echo http://dl-2.alpinelinux.org/alpine/edge/community/ >>/etc/apk/repositories && \
    apk --no-cache --no-progress add \
    tzdata \
    libstdc++ \
    ca-certificates && \
    rm -rf /var/cache/apk/*

WORKDIR /solitudes

# Copy binary and required files
COPY --from=builder /build/solitudes .
COPY --from=builder /build/resource ./resource
COPY --from=builder /go/pkg/mod/github.com/yanyiwu /go/pkg/mod/github.com/yanyiwu

# Configure container
VOLUME ["/solitudes/data"]
EXPOSE 8080

CMD ["/solitudes/solitudes"]
