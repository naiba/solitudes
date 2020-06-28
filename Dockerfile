FROM golang:alpine AS binarybuilder
# Install build deps
RUN apk --no-cache --no-progress add --virtual build-deps build-base git linux-pam-dev
WORKDIR /naiba/solitudes/
COPY . .
RUN go mod tidy -v && \
  CGO_ENABLED=true go build -o solitudes -ldflags="-s -w -X github.com/naiba/solitudes.BuildVersion=`git rev-parse HEAD`" cmd/web/main.go

FROM alpine:latest
RUN echo http://dl-2.alpinelinux.org/alpine/edge/community/ >>/etc/apk/repositories && apk --no-cache --no-progress add \
  tzdata \
  libstdc++ \
  ca-certificates
# Copy binary to container
WORKDIR /solitudes
COPY resource ./resource
COPY --from=binarybuilder /naiba/solitudes/solitudes .
COPY --from=binarybuilder /go/pkg/mod/github.com/yanyiwu /go/pkg/mod/github.com/yanyiwu
# Configure Docker Container
VOLUME ["/solitudes/data"]
EXPOSE 8080
CMD ["/solitudes/solitudes"]
